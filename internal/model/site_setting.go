package model

import "time"

type SiteSetting struct {
	Key       string `gorm:"primaryKey;size:100"`
	Value     string `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
