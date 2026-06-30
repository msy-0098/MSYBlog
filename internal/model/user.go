package model

import "time"

const (
	UserRoleAdmin   = "admin"
	UserRoleVisitor = "visitor"
)

type User struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"size:180;uniqueIndex;not null"`
	Email        string `gorm:"size:180;uniqueIndex"`
	Nickname     string `gorm:"size:80"`
	Role         string `gorm:"size:20;index;not null;default:'admin'"`
	PasswordHash string `gorm:"size:255;not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type EmailVerificationCode struct {
	ID        uint   `gorm:"primaryKey"`
	Email     string `gorm:"size:180;index;not null"`
	CodeHash  string `gorm:"size:255;not null"`
	UsedAt    *time.Time
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
