package model

import "time"

type AccessLog struct {
	ID        uint      `gorm:"primaryKey"`
	IP        string    `gorm:"size:64;index;not null"`
	Method    string    `gorm:"size:12;not null"`
	Path      string    `gorm:"size:500;index;not null"`
	Status    int       `gorm:"index;not null"`
	UserAgent string    `gorm:"size:500"`
	PostID    *uint     `gorm:"index"`
	CreatedAt time.Time `gorm:"index"`
}

type IPBan struct {
	ID        uint       `gorm:"primaryKey"`
	IP        string     `gorm:"size:64;uniqueIndex;not null"`
	Reason    string     `gorm:"size:255;not null"`
	Active    bool       `gorm:"index;not null;default:true"`
	ExpiresAt *time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
