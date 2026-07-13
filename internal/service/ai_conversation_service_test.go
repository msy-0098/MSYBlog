package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

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

func newAIConversationService(t *testing.T, client *fakeChatClient) (*AIConversationService, *gorm.DB, model.User) {
	t.Helper()
	db := testServiceDatabase(t)
	admin := model.User{Username: fmt.Sprintf("service-admin-%s", t.Name()), Email: fmt.Sprintf("service-admin-%s@example.test", t.Name()), Role: model.UserRoleAdmin, PasswordHash: "not-used"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	return NewAIConversationService(db, client, "deepseek-chat"), db, admin
}

func testServiceDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("BLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("BLOG_TEST_DATABASE_DSN is required for PostgreSQL service integration tests")
	}
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
