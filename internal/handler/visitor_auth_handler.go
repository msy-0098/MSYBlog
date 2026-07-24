package handler

import (
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/config"
	blogmail "masenyu.top/blog/backend/internal/mail"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

type VisitorAuthHandler struct {
	db          *gorm.DB
	cfg         config.Config
	jwtSecret   string
	now         func() time.Time
	codeLimiter *service.VerificationCodeLimiter
	mailSender  blogmail.Sender
}

type EmailCodeRequest struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
}

type VisitorRegisterRequest struct {
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type VisitorLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type VisitorResetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"newPassword"`
}

type VisitorUserDTO struct {
	ID       uint   `json:"id"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
}

type VisitorAuthResponse struct {
	Token string         `json:"token"`
	User  VisitorUserDTO `json:"user"`
}

func NewVisitorAuthHandler(db *gorm.DB, cfg config.Config, codeLimiter *service.VerificationCodeLimiter, mailSender blogmail.Sender) VisitorAuthHandler {
	if codeLimiter == nil {
		codeLimiter = service.NewVerificationCodeLimiter(cfg.VerificationCode.Cooldown, time.Now)
	}
	return VisitorAuthHandler{
		db:          db,
		cfg:         cfg,
		jwtSecret:   cfg.Auth.JWTSecret,
		now:         time.Now,
		codeLimiter: codeLimiter,
		mailSender:  mailSender,
	}
}

func (h VisitorAuthHandler) SendEmailCode(c *gin.Context) {
	var req EmailCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		badRequest(c)
		return
	}

	purpose, err := service.ParseCodePurpose(req.Purpose)
	if err != nil {
		badRequest(c)
		return
	}

	reservation, retryAfter := h.codeLimiter.Reserve(email, purpose)
	if retryAfter > 0 {
		seconds := int(math.Ceil(retryAfter.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		response.ErrorWithData(
			c,
			http.StatusTooManyRequests,
			"请求过于频繁，请稍后再试",
			gin.H{"retryAfter": seconds},
		)
		return
	}
	defer reservation.Rollback()

	if purpose == "reset" {
		var user model.User
		err := h.db.Where("email = ? AND role = ?", email, model.UserRoleVisitor).First(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			reservation.Commit()
			h.respondEmailCodeSent(c)
			return
		}
		if err != nil {
			internalError(c)
			return
		}
	}

	code, err := h.issueCode()
	if err != nil {
		internalError(c)
		return
	}

	codeHash, err := auth.HashPassword(code)
	if err != nil {
		internalError(c)
		return
	}

	if err := h.db.Create(&model.EmailVerificationCode{
		Email:     email,
		Purpose:   purpose,
		CodeHash:  codeHash,
		ExpiresAt: h.now().Add(h.cfg.VerificationCode.ExpiresIn),
	}).Error; err != nil {
		internalError(c)
		return
	}

	if h.mailSender != nil {
		from := h.cfg.Mail.From
		if from == "" {
			from = h.cfg.Mail.Username
		}
		message, err := blogmail.BuildVerificationEmail(blogmail.VerificationEmail{
			From:      from,
			To:        email,
			Code:      code,
			Purpose:   purpose,
			ExpiresIn: h.cfg.VerificationCode.ExpiresIn,
		})
		if err == nil {
			err = h.mailSender.Send(c.Request.Context(), email, message)
		}
		if err != nil {
			if purpose == "reset" {
				reservation.Commit()
				h.respondEmailCodeSent(c)
				return
			}
			response.Error(c, http.StatusInternalServerError, 500, "验证码暂时无法发送，请稍后再试")
			return
		}
	}

	reservation.Commit()
	h.respondEmailCodeSent(c)
}

func (h VisitorAuthHandler) respondEmailCodeSent(c *gin.Context) {
	response.Success(c, gin.H{
		"sent":            true,
		"cooldownSeconds": durationSeconds(h.cfg.VerificationCode.Cooldown),
		"expiresIn":       durationSeconds(h.cfg.VerificationCode.ExpiresIn),
	})
}

func durationSeconds(duration time.Duration) int {
	seconds := int(math.Ceil(duration.Seconds()))
	if seconds < 0 {
		return 0
	}
	return seconds
}

func (h VisitorAuthHandler) Register(c *gin.Context) {
	var req VisitorRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	email := normalizeEmail(req.Email)
	nickname := strings.TrimSpace(req.Nickname)
	password := strings.TrimSpace(req.Password)
	code := strings.TrimSpace(req.Code)
	if !validEmail(email) || nickname == "" || len(password) < 8 || code == "" {
		badRequest(c)
		return
	}

	if !h.verifyCode(email, code, "register") {
		response.Error(c, http.StatusBadRequest, 400, "验证码错误或已过期")
		return
	}

	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		internalError(c)
		return
	}

	user := model.User{
		Username:     email,
		Email:        email,
		Nickname:     nickname,
		Role:         model.UserRoleVisitor,
		PasswordHash: passwordHash,
	}
	if err := h.db.Create(&user).Error; err != nil {
		conflictOrInternal(c, err)
		return
	}

	token, err := auth.GenerateTokenWithRole(h.jwtSecret, user.ID, user.Email, user.Role, h.now())
	if err != nil {
		internalError(c)
		return
	}

	middleware.SetAuthCookie(c, middleware.VisitorTokenCookie, token)
	response.Success(c, VisitorAuthResponse{Token: token, User: visitorUserDTO(user)})
}

func (h VisitorAuthHandler) Login(c *gin.Context) {
	var req VisitorLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	email := normalizeEmail(req.Email)
	var user model.User
	if err := h.db.Where("email = ? AND role = ?", email, model.UserRoleVisitor).First(&user).Error; err != nil {
		response.Error(c, http.StatusUnauthorized, 401, "邮箱或密码错误")
		return
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		response.Error(c, http.StatusUnauthorized, 401, "邮箱或密码错误")
		return
	}

	token, err := auth.GenerateTokenWithRole(h.jwtSecret, user.ID, user.Email, user.Role, h.now())
	if err != nil {
		internalError(c)
		return
	}

	middleware.SetAuthCookie(c, middleware.VisitorTokenCookie, token)
	response.Success(c, VisitorAuthResponse{Token: token, User: visitorUserDTO(user)})
}

func (h VisitorAuthHandler) Logout(c *gin.Context) {
	middleware.ClearAuthCookie(c, middleware.VisitorTokenCookie)
	response.Success(c, gin.H{"loggedOut": true})
}

func (h VisitorAuthHandler) ResetPassword(c *gin.Context) {
	var req VisitorResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	email := normalizeEmail(req.Email)
	code := strings.TrimSpace(req.Code)
	newPassword := strings.TrimSpace(req.NewPassword)
	if !validEmail(email) || code == "" || len(newPassword) < 8 {
		badRequest(c)
		return
	}

	if !h.verifyCode(email, code, "reset") {
		response.Error(c, http.StatusBadRequest, 400, "验证码错误或已过期")
		return
	}

	var user model.User
	if err := h.db.Where("email = ? AND role = ?", email, model.UserRoleVisitor).First(&user).Error; err != nil {
		response.Error(c, http.StatusBadRequest, 400, "验证码错误或已过期")
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		internalError(c)
		return
	}
	if err := h.db.Model(&user).Update("password_hash", hash).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"updated": true})
}

func (h VisitorAuthHandler) issueCode() (string, error) {
	// Local / test mode without SMTP uses a fixed code so registration still works.
	if h.mailSender == nil {
		return "000000", nil
	}

	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (h VisitorAuthHandler) verifyCode(email string, code string, purpose string) bool {
	purpose = normalizeCodePurpose(purpose)
	var verification model.EmailVerificationCode
	err := h.db.Where(
		"email = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?",
		email, purpose, h.now(),
	).Order("created_at desc").First(&verification).Error
	if err != nil {
		// Backward-compatible fallback for codes created before purpose column existed.
		if purpose == "register" {
			err = h.db.Where(
				"email = ? AND purpose = ? AND used_at IS NULL AND expires_at > ?",
				email, "", h.now(),
			).Order("created_at desc").First(&verification).Error
		}
		if err != nil {
			return false
		}
	}

	if !auth.CheckPassword(verification.CodeHash, code) {
		return false
	}

	usedAt := h.now()
	verification.UsedAt = &usedAt
	return h.db.Save(&verification).Error == nil
}

func normalizeCodePurpose(raw string) string {
	purpose, _ := service.ParseCodePurpose(raw)
	return purpose
}

func visitorUserDTO(user model.User) VisitorUserDTO {
	nickname := user.Nickname
	if nickname == "" {
		nickname = user.Username
	}

	return VisitorUserDTO{
		ID:       user.ID,
		Email:    user.Email,
		Nickname: nickname,
		Role:     user.Role,
	}
}

func normalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func validEmail(email string) bool {
	return strings.Contains(email, "@") && strings.Contains(email, ".") && len(email) <= 180
}
