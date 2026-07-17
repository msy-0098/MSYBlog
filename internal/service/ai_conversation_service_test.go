package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/model"
)

var errClientDisconnected = errors.New("client disconnected")
var errUpstreamUnavailable = errors.New("upstream unavailable")

type fakeChatClient struct {
	deltas   []string
	err      error
	messages []ai.Message
}

var _ ai.ChatClient = (*fakeChatClient)(nil)

func (f *fakeChatClient) Configured() bool { return true }
func (f *fakeChatClient) Chat(context.Context, []ai.Message) (string, error) {
	return "", errors.New("not implemented")
}
func (f *fakeChatClient) StreamChat(_ context.Context, messages []ai.Message, callback func(string) error) error {
	f.messages = append([]ai.Message(nil), messages...)
	if f.err != nil {
		return f.err
	}
	for _, delta := range f.deltas {
		if err := callback(delta); err != nil {
			return err
		}
	}
	return nil
}

type blockingChatClient struct {
	started chan struct{}
	release chan struct{}
}

var _ ai.ChatClient = (*blockingChatClient)(nil)

func (f *blockingChatClient) Configured() bool { return true }
func (f *blockingChatClient) Chat(context.Context, []ai.Message) (string, error) {
	return "", errors.New("not implemented")
}
func (f *blockingChatClient) StreamChat(ctx context.Context, _ []ai.Message, _ func(string) error) error {
	f.started <- struct{}{}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.release:
		return nil
	}
}

func newAIConversationService(t *testing.T, client ai.ChatClient) (*AIConversationService, *gorm.DB, model.User) {
	t.Helper()
	db := testServiceDatabase(t)
	admin := model.User{Username: fmt.Sprintf("service-admin-%s", t.Name()), Email: fmt.Sprintf("service-admin-%s@example.test", t.Name()), Role: model.UserRoleAdmin, PasswordHash: "not-used"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return NewAIConversationService(db, client, "deepseek-chat"), db, admin
}

// Shared with database/router packages.
const postgresTestLockKey int64 = 81220260709

// In-process reentrant gate: nested helpers in the same *testing.T must not open
// a second session-level advisory lock.
var postgresTestGate = struct {
	mu     sync.Mutex
	cond   *sync.Cond
	owner  *testing.T
	depth  int
	lockDB *gorm.DB
	sqlDB  *sql.DB
}{}

func init() {
	postgresTestGate.cond = sync.NewCond(&postgresTestGate.mu)
}

func lockPostgresTestDatabase(t *testing.T, dsn string) {
	t.Helper()

	postgresTestGate.mu.Lock()
	for postgresTestGate.owner != nil && postgresTestGate.owner != t {
		postgresTestGate.cond.Wait()
	}
	if postgresTestGate.owner == t {
		postgresTestGate.depth++
		postgresTestGate.mu.Unlock()
		t.Cleanup(func() { releasePostgresTestDatabase(t) })
		return
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		postgresTestGate.mu.Unlock()
		t.Fatalf("open lock database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		postgresTestGate.mu.Unlock()
		t.Fatalf("get lock sql database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := db.Exec("SET lock_timeout TO '15s'").Error; err != nil {
		_ = sqlDB.Close()
		postgresTestGate.mu.Unlock()
		t.Fatalf("set lock timeout: %v", err)
	}
	if err := db.Exec("SELECT pg_advisory_lock(?)", postgresTestLockKey).Error; err != nil {
		_ = sqlDB.Close()
		postgresTestGate.mu.Unlock()
		t.Fatalf("lock postgres test database: %v", err)
	}

	postgresTestGate.owner = t
	postgresTestGate.depth = 1
	postgresTestGate.lockDB = db
	postgresTestGate.sqlDB = sqlDB
	postgresTestGate.mu.Unlock()
	t.Cleanup(func() { releasePostgresTestDatabase(t) })
}

func releasePostgresTestDatabase(t *testing.T) {
	postgresTestGate.mu.Lock()
	defer postgresTestGate.mu.Unlock()
	if postgresTestGate.owner != t {
		return
	}
	postgresTestGate.depth--
	if postgresTestGate.depth > 0 {
		return
	}
	if postgresTestGate.lockDB != nil {
		_ = postgresTestGate.lockDB.Exec("SELECT pg_advisory_unlock(?)", postgresTestLockKey).Error
	}
	if postgresTestGate.sqlDB != nil {
		_ = postgresTestGate.sqlDB.Close()
	}
	postgresTestGate.owner = nil
	postgresTestGate.lockDB = nil
	postgresTestGate.sqlDB = nil
	postgresTestGate.cond.Signal()
}

func resetServiceSchema(t *testing.T, dsn string) {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open reset database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get reset sql database: %v", err)
	}
	defer sqlDB.Close()
	if err := db.Exec(`
		DROP TABLE IF EXISTS
			ai_messages,
			ai_conversations,
			post_tags,
			post_likes,
			comments,
			posts,
			categories,
			tags,
			projects,
			friend_links,
			uploads,
			email_verification_codes,
			access_logs,
			ip_bans,
			users,
			site_settings
		CASCADE
	`).Error; err != nil {
		t.Fatalf("drop test tables: %v", err)
	}
}

func testServiceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("BLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("BLOG_TEST_DATABASE_DSN is required for PostgreSQL service integration tests")
	}

	// Serialize against database/router package tests that share public schema.
	// Reentrant within the same *testing.T so nested helpers do not deadlock.
	lockPostgresTestDatabase(t, dsn)
	// Fresh schema every test: residual rows/counters make MessageCount assertions flaky.
	resetServiceSchema(t, dsn)

	cfg, err := config.Load("__missing_service_test_config__.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Database.Driver, cfg.Database.DSN = "postgres", dsn
	db, err := database.Open(database.Options{Config: cfg})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db.Session(&gorm.Session{NewDB: true})
}

func TestConversationServicePersistsUserAndStreamedAssistant(t *testing.T) {
	client := &fakeChatClient{deltas: []string{"第一段", "第二段"}}
	service, db, admin := newAIConversationService(t, client)
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	message, err := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "分析流量", func(string) error { return nil })
	if err != nil {
		t.Fatalf("stream message: %v", err)
	}
	if message.Status != model.AIMessageStatusCompleted || message.Content != "第一段第二段" {
		t.Fatalf("unexpected assistant message: %#v", message)
	}
	var rows []model.AIMessage
	if err := db.Where("conversation_id = ?", conversation.ID).Order("sequence").Find(&rows).Error; err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(rows) != 2 || rows[0].Role != model.AIMessageRoleUser || rows[1].Role != model.AIMessageRoleAssistant || rows[0].Sequence != 1 || rows[1].Sequence != 2 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	var stored model.AIConversation
	if err := db.First(&stored, conversation.ID).Error; err != nil {
		t.Fatalf("load conversation: %v", err)
	}
	if stored.Title != "分析流量" || stored.TitleMode != model.AIConversationTitleModeAuto || stored.MessageCount != 2 || stored.LastMessageAt == nil {
		t.Fatalf("unexpected conversation: %#v", stored)
	}
}

func TestConversationServiceRejectsOtherAdministratorAndPreservesManualTitle(t *testing.T) {
	service, db, admin := newAIConversationService(t, &fakeChatClient{deltas: []string{"answer"}})
	other := model.User{Username: fmt.Sprintf("service-other-%s", t.Name()), Email: fmt.Sprintf("service-other-%s@example.test", t.Name()), Role: model.UserRoleAdmin, PasswordHash: "not-used"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create other admin: %v", err)
	}
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, _, err := service.Get(context.Background(), other.ID, conversation.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected owner isolation error, got %v", err)
	}
	renamed, err := service.Rename(context.Background(), admin.ID, conversation.ID, "手动标题")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.TitleMode != model.AIConversationTitleModeManual {
		t.Fatalf("rename did not mark title manual: %#v", renamed)
	}
	if _, err := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "不会覆盖标题", func(string) error { return nil }); err != nil {
		t.Fatalf("stream message: %v", err)
	}
	stored, _, err := service.Get(context.Background(), admin.ID, conversation.ID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if stored.Title != "手动标题" || stored.TitleMode != model.AIConversationTitleModeManual {
		t.Fatalf("manual title was overwritten: %#v", stored)
	}
}

func TestConversationServiceDeleteCascadesAndClearIsScopedToOwner(t *testing.T) {
	service, db, admin := newAIConversationService(t, &fakeChatClient{deltas: []string{"answer"}})
	other := model.User{Username: fmt.Sprintf("service-clear-other-%s", t.Name()), Email: fmt.Sprintf("service-clear-other-%s@example.test", t.Name()), Role: model.UserRoleAdmin, PasswordHash: "not-used"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create other admin: %v", err)
	}
	doomed, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create doomed conversation: %v", err)
	}
	if _, err := service.StreamMessage(context.Background(), admin.ID, doomed.ID, "delete me", func(string) error { return nil }); err != nil {
		t.Fatalf("stream doomed: %v", err)
	}
	if err := service.Delete(context.Background(), admin.ID, doomed.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var count int64
	if err := db.Model(&model.AIMessage{}).Where("conversation_id = ?", doomed.ID).Count(&count).Error; err != nil {
		t.Fatalf("count deleted messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected cascading message deletion, got %d rows", count)
	}
	own, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create own: %v", err)
	}
	theirs, err := service.Create(context.Background(), other.ID)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if err := service.Clear(context.Background(), admin.ID); err != nil {
		t.Fatalf("clear own: %v", err)
	}
	if _, _, err := service.Get(context.Background(), admin.ID, own.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected own removed, got %v", err)
	}
	if _, _, err := service.Get(context.Background(), other.ID, theirs.ID); err != nil {
		t.Fatalf("other conversation should remain: %v", err)
	}
}

func TestConversationServiceMarksCallbackAbortAndUpstreamFailure(t *testing.T) {
	service, db, admin := newAIConversationService(t, &fakeChatClient{deltas: []string{"partial"}})
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if _, err := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "abort", func(string) error { return errClientDisconnected }); !errors.Is(err, errClientDisconnected) {
		t.Fatalf("expected callback error, got %v", err)
	}
	aborted := latestAssistantMessage(t, db, conversation.ID)
	if aborted.Status != model.AIMessageStatusAborted || aborted.Content != "partial" {
		t.Fatalf("expected aborted assistant message, got %#v", aborted)
	}
	failing := NewAIConversationService(db, &fakeChatClient{err: errUpstreamUnavailable}, "deepseek-chat")
	if _, err := failing.StreamMessage(context.Background(), admin.ID, conversation.ID, "fail", func(string) error { return nil }); !errors.Is(err, errUpstreamUnavailable) {
		t.Fatalf("expected upstream error, got %v", err)
	}
	failed := latestAssistantMessage(t, db, conversation.ID)
	if failed.Status != model.AIMessageStatusFailed || failed.ErrorMessage == "" {
		t.Fatalf("expected failed assistant message, got %#v", failed)
	}
}

func TestConversationServiceLimitsContextToTwentyPersistedMessages(t *testing.T) {
	client := &fakeChatClient{deltas: []string{"answer"}}
	service, db, admin := newAIConversationService(t, client)
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	for sequence := 1; sequence <= 25; sequence++ {
		if err := db.Create(&model.AIMessage{ConversationID: conversation.ID, Role: model.AIMessageRoleUser, Content: fmt.Sprintf("old-%d", sequence), Status: model.AIMessageStatusCompleted, Sequence: sequence}).Error; err != nil {
			t.Fatalf("seed message %d: %v", sequence, err)
		}
	}
	if _, err := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "newest", func(string) error { return nil }); err != nil {
		t.Fatalf("stream message: %v", err)
	}
	if len(client.messages) != 21 {
		t.Fatalf("expected system prompt plus 20 persisted messages, got %d: %#v", len(client.messages), client.messages)
	}
	if client.messages[0].Role != "system" || client.messages[1].Content != "old-7" || client.messages[len(client.messages)-1].Content != "newest" {
		t.Fatalf("unexpected context window: %#v", client.messages)
	}
}

func latestAssistantMessage(t *testing.T, db *gorm.DB, conversationID uint) model.AIMessage {
	t.Helper()
	var message model.AIMessage
	if err := db.Where("conversation_id = ? AND role = ?", conversationID, model.AIMessageRoleAssistant).Order("sequence desc").First(&message).Error; err != nil {
		t.Fatalf("load latest assistant message: %v", err)
	}
	return message
}

func TestConversationServiceDeleteCancelsActiveStream(t *testing.T) {
	client := &blockingChatClient{started: make(chan struct{}, 1), release: make(chan struct{})}
	service, _, admin := newAIConversationService(t, client)
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	result := make(chan struct {
		message model.AIMessage
		err     error
	}, 1)
	go func() {
		message, streamErr := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "delete while streaming", func(string) error { return nil })
		result <- struct {
			message model.AIMessage
			err     error
		}{message, streamErr}
	}()
	awaitStreamStart(t, client.started)
	if err := service.Delete(context.Background(), admin.ID, conversation.ID); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	close(client.release)

	outcome := awaitStreamResult(t, result)
	if outcome.err == nil || outcome.message.Status == model.AIMessageStatusCompleted {
		t.Fatalf("deleted stream must not finish successfully, got message=%#v err=%v", outcome.message, outcome.err)
	}
}

func TestConversationServiceClearCancelsActiveStreamsForOwner(t *testing.T) {
	client := &blockingChatClient{started: make(chan struct{}, 1), release: make(chan struct{})}
	service, _, admin := newAIConversationService(t, client)
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	result := make(chan struct {
		message model.AIMessage
		err     error
	}, 1)
	go func() {
		message, streamErr := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "clear while streaming", func(string) error { return nil })
		result <- struct {
			message model.AIMessage
			err     error
		}{message, streamErr}
	}()
	awaitStreamStart(t, client.started)
	if err := service.Clear(context.Background(), admin.ID); err != nil {
		t.Fatalf("clear conversations: %v", err)
	}
	close(client.release)

	outcome := awaitStreamResult(t, result)
	if outcome.err == nil || outcome.message.Status == model.AIMessageStatusCompleted {
		t.Fatalf("cleared stream must not finish successfully, got message=%#v err=%v", outcome.message, outcome.err)
	}
}

func TestConversationServiceDoesNotReportCompletionAfterConcurrentMessageDeletion(t *testing.T) {
	client := &blockingChatClient{started: make(chan struct{}, 1), release: make(chan struct{})}
	service, db, admin := newAIConversationService(t, client)
	conversation, err := service.Create(context.Background(), admin.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	result := make(chan struct {
		message model.AIMessage
		err     error
	}, 1)
	go func() {
		message, streamErr := service.StreamMessage(context.Background(), admin.ID, conversation.ID, "delete message row", func(string) error { return nil })
		result <- struct {
			message model.AIMessage
			err     error
		}{message, streamErr}
	}()
	awaitStreamStart(t, client.started)
	if err := db.Where("conversation_id = ?", conversation.ID).Delete(&model.AIMessage{}).Error; err != nil {
		t.Fatalf("delete streaming messages directly: %v", err)
	}
	close(client.release)

	outcome := awaitStreamResult(t, result)
	if outcome.err == nil || outcome.message.Status == model.AIMessageStatusCompleted {
		t.Fatalf("missing assistant row must not be reported as completed, got message=%#v err=%v", outcome.message, outcome.err)
	}
}

func awaitStreamStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for AI stream to start")
	}
}

func awaitStreamResult[T any](t *testing.T, result <-chan T) T {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for AI stream result")
		var zero T
		return zero
	}
}

type activeStreamRegistration struct {
	adminID        uint
	conversationID uint
	ctx            context.Context
	stop           func()
}

func TestActiveStreamRegistryCancelsOnlyMatchingOwnerAndConversation(t *testing.T) {
	service := NewAIConversationService(nil, &fakeChatClient{}, "deepseek-chat")
	registrations := make(chan activeStreamRegistration, 32)
	var workers sync.WaitGroup
	register := func(adminID, conversationID uint, count int) {
		for range count {
			workers.Add(1)
			go func() {
				defer workers.Done()
				ctx, stop := service.registerActiveStream(context.Background(), adminID, conversationID)
				registrations <- activeStreamRegistration{adminID: adminID, conversationID: conversationID, ctx: ctx, stop: stop}
			}()
		}
	}
	register(1, 11, 16)
	register(1, 12, 8)
	register(2, 11, 8)
	workers.Wait()
	close(registrations)

	var ownerConversation, ownerOtherConversation, otherOwner []activeStreamRegistration
	for registration := range registrations {
		t.Cleanup(registration.stop)
		switch {
		case registration.adminID == 1 && registration.conversationID == 11:
			ownerConversation = append(ownerConversation, registration)
		case registration.adminID == 1 && registration.conversationID == 12:
			ownerOtherConversation = append(ownerOtherConversation, registration)
		default:
			otherOwner = append(otherOwner, registration)
		}
	}

	service.cancelConversationStreams(1, 11)
	assertContextsCanceled(t, ownerConversation, true)
	assertContextsCanceled(t, ownerOtherConversation, false)
	assertContextsCanceled(t, otherOwner, false)

	service.cancelAdminStreams(1)
	assertContextsCanceled(t, ownerOtherConversation, true)
	assertContextsCanceled(t, otherOwner, false)
}

func assertContextsCanceled(t *testing.T, registrations []activeStreamRegistration, wantCanceled bool) {
	t.Helper()
	for _, registration := range registrations {
		select {
		case <-registration.ctx.Done():
			if !wantCanceled {
				t.Fatal("unexpected active stream cancellation")
			}
		default:
			if wantCanceled {
				t.Fatal("expected active stream cancellation")
			}
		}
	}
}
