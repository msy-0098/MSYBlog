package model

import "time"

type Project struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:120;not null"`
	Description string `gorm:"type:text;not null"`
	URL         string `gorm:"size:500"`
	Cover       string `gorm:"size:500"`
	TechStack   string `gorm:"size:1000;not null;default:'[]'"`
	Sort        int    `gorm:"not null;default:0;index"`
	Visible     bool   `gorm:"not null;default:true;index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
