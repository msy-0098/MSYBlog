package model

import "time"

const (
	PostStatusDraft     = "draft"
	PostStatusPublished = "published"
	PostStatusHidden    = "hidden"
)

type Category struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:80;not null"`
	Slug      string `gorm:"size:120;uniqueIndex;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Tag struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:80;not null"`
	Slug      string `gorm:"size:120;uniqueIndex;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Post struct {
	ID          uint     `gorm:"primaryKey"`
	Title       string   `gorm:"size:180;not null"`
	Slug        string   `gorm:"size:220;uniqueIndex;not null"`
	Summary     string   `gorm:"type:text;not null"`
	Content     string   `gorm:"type:text;not null"`
	Cover       string   `gorm:"size:500"`
	Status      string   `gorm:"size:20;index;not null"`
	ViewCount   int      `gorm:"not null;default:0"`
	LikeCount   int      `gorm:"not null;default:0"`
	CategoryID  uint     `gorm:"index;not null"`
	Category    Category `gorm:"constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	Tags        []Tag    `gorm:"many2many:post_tags;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PublishedAt time.Time `gorm:"index"`
}

// PostLike records one like from a client IP for a post (unique per post+ip).
type PostLike struct {
	ID        uint   `gorm:"primaryKey"`
	PostID    uint   `gorm:"uniqueIndex:idx_post_like_ip;not null"`
	Post      Post   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
	IP        string `gorm:"size:64;uniqueIndex:idx_post_like_ip;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FriendLink is a public friendship / partner link shown on the blog.
type FriendLink struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"size:120;not null"`
	URL         string `gorm:"size:500;not null"`
	Description string `gorm:"size:500"`
	Logo        string `gorm:"size:500"`
	Sort        int    `gorm:"not null;default:0;index"`
	Visible     bool   `gorm:"not null;default:true;index"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
