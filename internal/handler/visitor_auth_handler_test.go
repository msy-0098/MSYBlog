package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/config"
	blogmail "masenyu.top/blog/backend/internal/mail"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/service"
)

type fakeVerificationEmailSender struct {
	calls *atomic.Int32
	send  func(to string, message []byte) error
}

func (sender fakeVerificationEmailSender) Send(ctx context.Context, to string, message []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sender.calls.Add(1)
	return sender.send(to, message)
}

type emailCodeTestEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Sent            bool `json:"sent"`
		CooldownSeconds int  `json:"cooldownSeconds"`
		ExpiresIn       int  `json:"expiresIn"`
		RetryAfter      int  `json:"retryAfter"`
	} `json:"data"`
}

func TestSendEmailCodeRejectsUnknownPurpose(t *testing.T) {
	handler, engine, _, sendCalls, _ := newEmailCodeTestHandler(t, func(string, string, string) error {
		return nil
	})
	_ = handler

	recorder := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "reader@example.com",
		"purpose": "Register",
	})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if sendCalls.Load() != 0 {
		t.Fatalf("send calls = %d, want 0", sendCalls.Load())
	}
}

func TestSendEmailCodeReturnsConfiguredSuccessDataAndNormalizesEmail(t *testing.T) {
	var sentEmail string
	var sentPurpose string
	_, engine, db, sendCalls, _ := newEmailCodeTestHandler(t, func(email string, _ string, purpose string) error {
		sentEmail = email
		sentPurpose = purpose
		return nil
	})

	recorder := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "  Reader@Example.COM ",
		"purpose": " register ",
	})
	body := decodeEmailCodeEnvelope(t, recorder)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if body.Code != 0 || body.Message != "success" || !body.Data.Sent {
		t.Fatalf("unexpected success envelope: %#v", body)
	}
	if body.Data.CooldownSeconds != 60 || body.Data.ExpiresIn != 600 {
		t.Fatalf("unexpected timing data: %#v", body.Data)
	}
	if sendCalls.Load() != 1 || sentEmail != "reader@example.com" || sentPurpose != "register" {
		t.Fatalf("send calls=%d email=%q purpose=%q", sendCalls.Load(), sentEmail, sentPurpose)
	}

	var verification model.EmailVerificationCode
	if err := db.First(&verification).Error; err != nil {
		t.Fatalf("load verification code: %v", err)
	}
	if verification.Email != "reader@example.com" || verification.Purpose != "register" {
		t.Fatalf("stored verification = %#v", verification)
	}
}

func TestSendEmailCodeCooldownReturnsRetryAfter(t *testing.T) {
	_, engine, _, sendCalls, now := newEmailCodeTestHandler(t, func(string, string, string) error { return nil })
	request := map[string]string{"email": "reader@example.com", "purpose": "register"}

	first := performEmailCodeRequest(t, engine, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d; body=%s", first.Code, first.Body.String())
	}
	*now = now.Add(59*time.Second + 500*time.Millisecond)
	second := performEmailCodeRequest(t, engine, request)
	body := decodeEmailCodeEnvelope(t, second)

	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d; body=%s", second.Code, http.StatusTooManyRequests, second.Body.String())
	}
	if body.Message != "请求过于频繁，请稍后再试" || body.Data.RetryAfter != 1 {
		t.Fatalf("unexpected rounded cooldown response: %#v", body)
	}
	if sendCalls.Load() != 1 {
		t.Fatalf("send calls = %d, want 1", sendCalls.Load())
	}
}

func TestSendEmailCodeUnknownResetDoesNotRevealAccountOrSend(t *testing.T) {
	_, engine, db, sendCalls, _ := newEmailCodeTestHandler(t, func(string, string, string) error { return nil })

	unknown := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "missing@example.com",
		"purpose": "reset",
	})
	unknownBody := decodeEmailCodeEnvelope(t, unknown)

	if unknown.Code != http.StatusOK || unknownBody.Message != "success" || !unknownBody.Data.Sent {
		t.Fatalf("unexpected unknown reset response: status=%d body=%#v", unknown.Code, unknownBody)
	}
	if unknownBody.Data.CooldownSeconds != 60 || unknownBody.Data.ExpiresIn != 600 {
		t.Fatalf("unexpected unknown reset timing data: %#v", unknownBody.Data)
	}
	if sendCalls.Load() != 0 {
		t.Fatalf("unknown reset send calls = %d, want 0", sendCalls.Load())
	}
	var count int64
	if err := db.Model(&model.EmailVerificationCode{}).Count(&count).Error; err != nil {
		t.Fatalf("count verification codes: %v", err)
	}
	if count != 0 {
		t.Fatalf("unknown reset stored %d verification codes, want 0", count)
	}

	if err := db.Create(&model.User{
		Username:     "known@example.com",
		Email:        "known@example.com",
		Nickname:     "Known",
		Role:         model.UserRoleVisitor,
		PasswordHash: "hash",
	}).Error; err != nil {
		t.Fatalf("create known visitor: %v", err)
	}
	known := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "known@example.com",
		"purpose": "reset",
	})
	if known.Code != http.StatusOK || known.Body.String() != unknown.Body.String() {
		t.Fatalf("unknown reset response differs from known success: unknown=%s known=%s", unknown.Body.String(), known.Body.String())
	}
	if sendCalls.Load() != 1 {
		t.Fatalf("known reset send calls after unknown reset = %d, want 1", sendCalls.Load())
	}

	second := performEmailCodeRequest(t, engine, map[string]string{
		"email":   " MISSING@EXAMPLE.COM ",
		"purpose": "reset",
	})
	secondBody := decodeEmailCodeEnvelope(t, second)
	if second.Code != http.StatusTooManyRequests || secondBody.Data.RetryAfter != 60 {
		t.Fatalf("unknown reset cooldown missing: status=%d body=%#v", second.Code, secondBody)
	}
}

func TestSendEmailCodeSMTPFailureDoesNotStartCooldown(t *testing.T) {
	smtpErr := errors.New("smtp password=top-secret code=123456")
	_, engine, _, sendCalls, _ := newEmailCodeTestHandler(t, func(string, string, string) error { return smtpErr })
	request := map[string]string{"email": "reader@example.com", "purpose": "register"}

	first := performEmailCodeRequest(t, engine, request)
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first status = %d, want %d; body=%s", first.Code, http.StatusInternalServerError, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), "验证码暂时无法发送，请稍后再试") {
		t.Fatalf("friendly error missing: %s", first.Body.String())
	}
	if strings.Contains(first.Body.String(), "top-secret") || strings.Contains(first.Body.String(), "123456") {
		t.Fatalf("response leaked SMTP details: %s", first.Body.String())
	}

	second := performEmailCodeRequest(t, engine, request)
	if second.Code != http.StatusInternalServerError {
		t.Fatalf("second status = %d, want %d; body=%s", second.Code, http.StatusInternalServerError, second.Body.String())
	}
	if sendCalls.Load() != 2 {
		t.Fatalf("send calls = %d, want 2 so failed attempts are not cooled down", sendCalls.Load())
	}
}

func TestSendEmailCodeKnownResetSenderFailureMatchesUnknownReset(t *testing.T) {
	_, engine, db, sendCalls, _ := newEmailCodeTestHandler(t, func(string, string, string) error {
		return errors.New("smtp password=top-secret code=123456")
	})

	unknown := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "missing@example.com",
		"purpose": "reset",
	})
	if err := db.Create(&model.User{
		Username:     "known@example.com",
		Email:        "known@example.com",
		Nickname:     "Known",
		Role:         model.UserRoleVisitor,
		PasswordHash: "hash",
	}).Error; err != nil {
		t.Fatalf("create known visitor: %v", err)
	}
	known := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "known@example.com",
		"purpose": "reset",
	})

	if known.Code != unknown.Code || known.Body.String() != unknown.Body.String() {
		t.Fatalf("known reset failure reveals account: unknown=%d %s known=%d %s", unknown.Code, unknown.Body.String(), known.Code, known.Body.String())
	}
	if sendCalls.Load() != 1 {
		t.Fatalf("known reset send calls = %d, want 1", sendCalls.Load())
	}
	knownRetry := performEmailCodeRequest(t, engine, map[string]string{
		"email":   "known@example.com",
		"purpose": "reset",
	})
	knownRetryBody := decodeEmailCodeEnvelope(t, knownRetry)
	if knownRetry.Code != http.StatusTooManyRequests || knownRetryBody.Data.RetryAfter != 60 {
		t.Fatalf("known reset failure cooldown missing: status=%d body=%#v", knownRetry.Code, knownRetryBody)
	}
	if sendCalls.Load() != 1 {
		t.Fatalf("known reset sender retried during cooldown: calls=%d", sendCalls.Load())
	}
}

func TestSendEmailCodeConcurrentRequestsUseSingleSender(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	_, engine, _, sendCalls, _ := newEmailCodeTestHandler(t, func(string, string, string) error {
		startedOnce.Do(func() { close(started) })
		<-release
		return nil
	})
	request := map[string]string{"email": "reader@example.com", "purpose": "register"}

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- performEmailCodeRequest(t, engine, request) }()
	<-started
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { secondDone <- performEmailCodeRequest(t, engine, request) }()

	var second *httptest.ResponseRecorder
	secondTimedOut := false
	select {
	case second = <-secondDone:
	case <-time.After(time.Second):
		secondTimedOut = true
	}
	close(release)
	first := <-firstDone
	if secondTimedOut {
		second = <-secondDone
		t.Fatalf("second request blocked instead of receiving cooldown response; status after release = %d", second.Code)
	}

	if first.Code != http.StatusOK || second.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent statuses = first %d, second %d", first.Code, second.Code)
	}
	if sendCalls.Load() != 1 {
		t.Fatalf("concurrent sender calls = %d, want 1", sendCalls.Load())
	}
}

func TestVerifyCodeDoesNotUseResetCodeForRegisterFallback(t *testing.T) {
	handler, _, db, _, now := newEmailCodeTestHandler(t, func(string, string, string) error { return nil })
	codeHash, err := auth.HashPassword("123456")
	if err != nil {
		t.Fatalf("hash code: %v", err)
	}
	if err := db.Create(&model.EmailVerificationCode{
		Email:     "reader@example.com",
		Purpose:   "reset",
		CodeHash:  codeHash,
		ExpiresAt: now.Add(time.Minute),
	}).Error; err != nil {
		t.Fatalf("create reset code: %v", err)
	}

	if handler.verifyCode("reader@example.com", "123456", "register") {
		t.Fatal("register accepted a reset verification code")
	}
}

func TestVerifyCodeAllowsLegacyEmptyPurposeForRegister(t *testing.T) {
	handler, _, db, _, now := newEmailCodeTestHandler(t, func(string, string, string) error { return nil })
	codeHash, err := auth.HashPassword("123456")
	if err != nil {
		t.Fatalf("hash code: %v", err)
	}
	if err := db.Exec(
		"INSERT INTO email_verification_codes (email, purpose, code_hash, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"reader@example.com", "", codeHash, now.Add(time.Minute), *now, *now,
	).Error; err != nil {
		t.Fatalf("create legacy code: %v", err)
	}

	if !handler.verifyCode("reader@example.com", "123456", "register") {
		t.Fatal("register rejected a legacy verification code with empty purpose")
	}
}

func newEmailCodeTestHandler(t *testing.T, send func(string, string, string) error) (VisitorAuthHandler, *gin.Engine, *gorm.DB, *atomic.Int32, *time.Time) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "visitor-auth.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test database handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.EmailVerificationCode{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	cfg := config.Default()
	cfg.VerificationCode.Cooldown = time.Minute
	cfg.VerificationCode.ExpiresIn = 10 * time.Minute
	cfg.Mail = config.MailConfig{
		SMTPHost: "smtp.example.test",
		SMTPPort: "587",
		Username: "mailer@example.test",
		Password: "smtp-password",
		From:     "mailer@example.test",
	}
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	limiter := service.NewVerificationCodeLimiter(cfg.VerificationCode.Cooldown, func() time.Time { return now })
	sendCalls := &atomic.Int32{}
	var sender blogmail.Sender = fakeVerificationEmailSender{
		calls: sendCalls,
		send: func(email string, message []byte) error {
			content := string(message)
			purpose := "register"
			if strings.Contains(content, "重置密码") {
				purpose = "reset"
			}
			const marker = "验证码："
			start := strings.Index(content, marker)
			if start < 0 || len(content) < start+len(marker)+6 {
				return errors.New("verification code missing from generated message")
			}
			code := content[start+len(marker) : start+len(marker)+6]
			return send(email, code, purpose)
		},
	}
	handler := NewVisitorAuthHandler(db, cfg, limiter, sender)
	handler.now = func() time.Time { return now }

	engine := gin.New()
	engine.POST("/", handler.SendEmailCode)
	return handler, engine, db, sendCalls, &now
}

func performEmailCodeRequest(t *testing.T, engine http.Handler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeEmailCodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) emailCodeTestEnvelope {
	t.Helper()
	var body emailCodeTestEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return body
}
