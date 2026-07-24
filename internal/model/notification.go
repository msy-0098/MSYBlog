package model

import "time"

const (
	NotificationKindComment  = "comment"
	NotificationKindSecurity = "security"
	NotificationKindUser     = "user"
	NotificationKindSystem   = "system"
)

// Notification is an in-app message for an administrator.
type Notification struct {
	ID        uint       `gorm:"primaryKey"`
	UserID    uint       `gorm:"index;not null"`
	User      User       `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Kind      string     `gorm:"size:20;index;not null"`
	Title     string     `gorm:"size:200;not null"`
	Body      string     `gorm:"type:text;not null"`
	RefType   string     `gorm:"size:40"`
	RefID     uint       `gorm:"index"`
	ReadAt    *time.Time `gorm:"index"`
	CreatedAt time.Time  `gorm:"index"`
	UpdatedAt time.Time
}