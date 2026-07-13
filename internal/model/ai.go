package model

import "time"

const (
	AIConversationTitleModeAuto   = "auto"
	AIConversationTitleModeManual = "manual"

	AIMessageRoleUser      = "user"
	AIMessageRoleAssistant = "assistant"

	AIMessageStatusStreaming = "streaming"
	AIMessageStatusCompleted = "completed"
	AIMessageStatusAborted   = "aborted"
	AIMessageStatusFailed    = "failed"
)

type AIConversation struct {
	ID            uint       `gorm:"primaryKey"`
	Title         string     `gorm:"size:255;not null"`
	TitleMode     string     `gorm:"size:20;not null;default:'auto'"`
	CreatedBy     uint       `gorm:"index;not null"`
	Model         string     `gorm:"size:120;not null"`
	MessageCount  int        `gorm:"not null;default:0"`
	LastMessageAt *time.Time `gorm:"index"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type AIMessage struct {
	ID             uint   `gorm:"primaryKey"`
	ConversationID uint   `gorm:"not null;index;uniqueIndex:idx_ai_message_sequence"`
	Role           string `gorm:"size:20;not null"`
	Content        string `gorm:"type:text;not null"`
	Status         string `gorm:"size:20;not null"`
	Sequence       int    `gorm:"not null;uniqueIndex:idx_ai_message_sequence"`
	Model          string `gorm:"size:120"`
	ErrorMessage   string `gorm:"type:text"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
