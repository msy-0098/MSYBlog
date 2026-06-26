package model

import "time"

type Upload struct {
	ID        uint   `gorm:"primaryKey"`
	Filename  string `gorm:"size:255;not null"`
	Path      string `gorm:"size:500;not null"`
	MimeType  string `gorm:"size:120;not null"`
	Size      int64  `gorm:"not null"`
	CreatedAt time.Time
}
