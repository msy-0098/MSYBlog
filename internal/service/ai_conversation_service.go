package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/model"
)

const (
	defaultConversationTitle = "新对话"
	defaultAIModel           = "deepseek-chat"
	maxContextMessages       = 20
	maxAutoTitleRunes        = 60
)

var (
	ErrConversationContentRequired = errors.New("conversation message content is required")
	ErrConversationTitleRequired   = errors.New("conversation title is required")
	ErrStreamCallbackRequired      = errors.New("stream callback is required")
	ErrAIClientUnavailable         = errors.New("AI client is not configured")
	ErrStreamResultDiscarded       = errors.New("AI stream result was discarded")
)

type AIConversationService struct {
	db       *gorm.DB
	aiClient ai.ChatClient
	model    string

	streamMu      sync.Mutex
	activeStreams map[activeStreamKey]map[uint64]context.CancelFunc
	nextStreamID  uint64
}

type activeStreamKey struct {
	adminID        uint
	conversationID uint
}

func NewAIConversationService(db *gorm.DB, aiClient ai.ChatClient, modelName string) *AIConversationService {
	if strings.TrimSpace(modelName) == "" {
		modelName = defaultAIModel
	}
	return &AIConversationService{db: db, aiClient: aiClient, model: modelName, activeStreams: make(map[activeStreamKey]map[uint64]context.CancelFunc)}
}

func (s *AIConversationService) List(ctx context.Context, adminID uint) ([]model.AIConversation, error) {
	var conversations []model.AIConversation
	err := s.db.WithContext(ctx).
		Where("created_by = ?", adminID).
		Order("last_message_at DESC NULLS LAST, created_at DESC").
		Find(&conversations).Error
	return conversations, err
}

func (s *AIConversationService) Create(ctx context.Context, adminID uint) (model.AIConversation, error) {
	conversation := model.AIConversation{
		Title:     defaultConversationTitle,
		TitleMode: model.AIConversationTitleModeAuto,
		CreatedBy: adminID,
		Model:     s.model,
	}
	return conversation, s.db.WithContext(ctx).Create(&conversation).Error
}

func (s *AIConversationService) Get(ctx context.Context, adminID, conversationID uint) (model.AIConversation, []model.AIMessage, error) {
	conversation, err := s.conversationForAdmin(ctx, s.db, adminID, conversationID)
	if err != nil {
		return model.AIConversation{}, nil, err
	}
	var messages []model.AIMessage
	if err := s.db.WithContext(ctx).Where("conversation_id = ?", conversation.ID).Order("sequence asc").Find(&messages).Error; err != nil {
		return model.AIConversation{}, nil, err
	}
	return conversation, messages, nil
}

func (s *AIConversationService) Rename(ctx context.Context, adminID, conversationID uint, title string) (model.AIConversation, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return model.AIConversation{}, ErrConversationTitleRequired
	}
	conversation, err := s.conversationForAdmin(ctx, s.db, adminID, conversationID)
	if err != nil {
		return model.AIConversation{}, err
	}
	if err := s.db.WithContext(ctx).Model(&conversation).Updates(map[string]any{
		"title":      title,
		"title_mode": model.AIConversationTitleModeManual,
	}).Error; err != nil {
		return model.AIConversation{}, err
	}
	conversation.Title = title
	conversation.TitleMode = model.AIConversationTitleModeManual
	return conversation, nil
}

func (s *AIConversationService) Delete(ctx context.Context, adminID, conversationID uint) error {
	conversation, err := s.conversationForAdmin(ctx, s.db, adminID, conversationID)
	if err != nil {
		return err
	}
	s.cancelConversationStreams(adminID, conversationID)
	return s.db.WithContext(ctx).Delete(&conversation).Error
}

func (s *AIConversationService) Clear(ctx context.Context, adminID uint) error {
	s.cancelAdminStreams(adminID)
	return s.db.WithContext(ctx).Where("created_by = ?", adminID).Delete(&model.AIConversation{}).Error
}

func (s *AIConversationService) StreamMessage(ctx context.Context, adminID, conversationID uint, content string, onDelta func(string) error) (model.AIMessage, error) {
	return s.StreamMessageWithStart(ctx, adminID, conversationID, content, nil, onDelta)
}

func (s *AIConversationService) StreamMessageWithStart(ctx context.Context, adminID, conversationID uint, content string, onStart func(model.AIMessage) error, onDelta func(string) error) (model.AIMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return model.AIMessage{}, ErrConversationContentRequired
	}
	if onDelta == nil {
		return model.AIMessage{}, ErrStreamCallbackRequired
	}
	streamCtx, stop := s.registerActiveStream(ctx, adminID, conversationID)
	defer stop()

	var conversation model.AIConversation
	var assistant model.AIMessage
	if err := s.db.WithContext(streamCtx).Transaction(func(tx *gorm.DB) error {
		var err error
		conversation, err = s.conversationForAdmin(streamCtx, tx.Clauses(clause.Locking{Strength: "UPDATE"}), adminID, conversationID)
		if err != nil {
			return err
		}
		var lastSequence int
		if err := tx.Model(&model.AIMessage{}).Where("conversation_id = ?", conversation.ID).Select("COALESCE(MAX(sequence), 0)").Scan(&lastSequence).Error; err != nil {
			return err
		}
		userMessage := model.AIMessage{
			ConversationID: conversation.ID,
			Role:           model.AIMessageRoleUser,
			Content:        content,
			Status:         model.AIMessageStatusCompleted,
			Sequence:       lastSequence + 1,
			Model:          s.model,
		}
		if err := tx.Create(&userMessage).Error; err != nil {
			return err
		}
		assistant = model.AIMessage{
			ConversationID: conversation.ID,
			Role:           model.AIMessageRoleAssistant,
			Content:        "",
			Status:         model.AIMessageStatusStreaming,
			Sequence:       lastSequence + 2,
			Model:          s.model,
		}
		if err := tx.Create(&assistant).Error; err != nil {
			return err
		}

		updates := map[string]any{"message_count": conversation.MessageCount + 2}
		if conversation.TitleMode == model.AIConversationTitleModeAuto {
			updates["title"] = autoConversationTitle(content)
			conversation.Title = updates["title"].(string)
		}
		if err := tx.Model(&conversation).Updates(updates).Error; err != nil {
			return err
		}
		conversation.MessageCount += 2
		return nil
	}); err != nil {
		return model.AIMessage{}, err
	}
	if onStart != nil {
		if err := onStart(assistant); err != nil {
			return s.finishStream(streamCtx, conversation, assistant, "", model.AIMessageStatusAborted, err)
		}
	}

	messages, err := s.contextMessages(streamCtx, conversation.ID)
	if err != nil {
		return s.finishStream(streamCtx, conversation, assistant, "", model.AIMessageStatusFailed, err)
	}
	prompt := append([]ai.Message{{Role: "system", Content: "你是马森雨个人技术博客的管理助手。请使用简洁、准确的中文回答；不确定时明确说明。"}}, messages...)

	if s.aiClient == nil || !s.aiClient.Configured() {
		return s.finishStream(streamCtx, conversation, assistant, "", model.AIMessageStatusFailed, ErrAIClientUnavailable)
	}

	var output strings.Builder
	var callbackErr error
	streamErr := s.aiClient.StreamChat(streamCtx, prompt, func(delta string) error {
		output.WriteString(delta)
		if err := onDelta(delta); err != nil {
			callbackErr = err
			return err
		}
		return nil
	})
	if streamErr == nil && streamCtx.Err() == nil {
		return s.finishStream(streamCtx, conversation, assistant, output.String(), model.AIMessageStatusCompleted, nil)
	}
	if streamErr == nil {
		streamErr = streamCtx.Err()
	}
	if callbackErr != nil || errors.Is(streamErr, context.Canceled) || errors.Is(streamCtx.Err(), context.Canceled) {
		return s.finishStream(streamCtx, conversation, assistant, output.String(), model.AIMessageStatusAborted, streamErr)
	}
	return s.finishStream(streamCtx, conversation, assistant, output.String(), model.AIMessageStatusFailed, streamErr)
}

func (s *AIConversationService) registerActiveStream(ctx context.Context, adminID, conversationID uint) (context.Context, func()) {
	streamCtx, cancel := context.WithCancel(ctx)
	key := activeStreamKey{adminID: adminID, conversationID: conversationID}
	streamID := atomic.AddUint64(&s.nextStreamID, 1)

	s.streamMu.Lock()
	if s.activeStreams[key] == nil {
		s.activeStreams[key] = make(map[uint64]context.CancelFunc)
	}
	s.activeStreams[key][streamID] = cancel
	s.streamMu.Unlock()

	return streamCtx, func() {
		cancel()
		s.streamMu.Lock()
		defer s.streamMu.Unlock()
		streams := s.activeStreams[key]
		if streams == nil {
			return
		}
		delete(streams, streamID)
		if len(streams) == 0 {
			delete(s.activeStreams, key)
		}
	}
}

func (s *AIConversationService) cancelConversationStreams(adminID, conversationID uint) {
	s.cancelStreams(func(key activeStreamKey) bool {
		return key.adminID == adminID && key.conversationID == conversationID
	})
}

func (s *AIConversationService) cancelAdminStreams(adminID uint) {
	s.cancelStreams(func(key activeStreamKey) bool {
		return key.adminID == adminID
	})
}

func (s *AIConversationService) cancelStreams(matches func(activeStreamKey) bool) {
	var cancellations []context.CancelFunc
	s.streamMu.Lock()
	for key, streams := range s.activeStreams {
		if !matches(key) {
			continue
		}
		for _, cancel := range streams {
			cancellations = append(cancellations, cancel)
		}
	}
	s.streamMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}
func (s *AIConversationService) conversationForAdmin(ctx context.Context, db *gorm.DB, adminID, conversationID uint) (model.AIConversation, error) {
	var conversation model.AIConversation
	err := db.WithContext(ctx).Where("id = ? AND created_by = ?", conversationID, adminID).First(&conversation).Error
	return conversation, err
}

func (s *AIConversationService) contextMessages(ctx context.Context, conversationID uint) ([]ai.Message, error) {
	var stored []model.AIMessage
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ? AND status IN ?", conversationID, []string{model.AIMessageStatusCompleted, model.AIMessageStatusAborted}).
		Order("sequence desc").
		Limit(maxContextMessages).
		Find(&stored).Error; err != nil {
		return nil, err
	}
	messages := make([]ai.Message, 0, len(stored))
	for index := len(stored) - 1; index >= 0; index-- {
		messages = append(messages, ai.Message{Role: stored[index].Role, Content: stored[index].Content})
	}
	return messages, nil
}

func (s *AIConversationService) finishStream(ctx context.Context, conversation model.AIConversation, assistant model.AIMessage, content, status string, streamErr error) (model.AIMessage, error) {
	now := time.Now()
	errorMessage := ""
	if streamErr != nil {
		errorMessage = streamErr.Error()
	}
	updates := map[string]any{
		"content":       content,
		"status":        status,
		"error_message": errorMessage,
	}
	// Persist first; only then report terminal status to callers.
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Transaction(func(tx *gorm.DB) error {
		messageResult := tx.Model(&model.AIMessage{}).Where("id = ?", assistant.ID).Updates(updates)
		if messageResult.Error != nil {
			return messageResult.Error
		}
		if messageResult.RowsAffected != 1 {
			return ErrStreamResultDiscarded
		}
		conversationResult := tx.Model(&model.AIConversation{}).Where("id = ?", conversation.ID).Updates(map[string]any{
			"last_message_at": now,
			"message_count":   conversation.MessageCount,
		})
		if conversationResult.Error != nil {
			return conversationResult.Error
		}
		if conversationResult.RowsAffected != 1 {
			return ErrStreamResultDiscarded
		}
		return nil
	}); err != nil {
		if errors.Is(err, ErrStreamResultDiscarded) {
			assistant.Content = content
			// Never report completed when the row was discarded mid-stream.
			if status == model.AIMessageStatusCompleted {
				assistant.Status = model.AIMessageStatusAborted
			} else if status != "" {
				assistant.Status = status
			}
			assistant.ErrorMessage = ErrStreamResultDiscarded.Error()
			return assistant, err
		}
		return assistant, fmt.Errorf("persist AI stream result: %w", err)
	}
	assistant.Content = content
	assistant.Status = status
	assistant.ErrorMessage = errorMessage
	if streamErr != nil {
		return assistant, streamErr
	}
	return assistant, nil
}

func autoConversationTitle(content string) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if utf8.RuneCountInString(content) <= maxAutoTitleRunes {
		return content
	}
	runes := []rune(content)
	return string(runes[:maxAutoTitleRunes]) + "…"
}
