package model

import "time"

const (
	CommentStatusApproved = "approved"
	CommentStatusHidden   = "hidden"
)

type Comment struct {
	ID        uint   `gorm:"primaryKey"`
	PostID    uint   `gorm:"index;not null"`
	Post      Post   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	UserID    uint   `gorm:"index;not null"`
	User      User   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	Content   string `gorm:"type:text;not null"`
	Status    string `gorm:"size:20;index;not null;default:'approved'"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
