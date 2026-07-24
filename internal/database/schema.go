package database

import (
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
)

func Models() []any {
	return []any{
		&model.SiteSetting{},
		&model.User{},
		&model.EmailVerificationCode{},
		&model.Category{},
		&model.Tag{},
		&model.Post{},
		&model.PostLike{},
		&model.Comment{},
		&model.Project{},
		&model.FriendLink{},
		&model.Upload{},
		&model.AccessLog{},
		&model.IPBan{},
		&model.AIConversation{},
		&model.AIMessage{},
		&model.Notification{},
	}
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(Models()...)
}

func SeedDefaults(db *gorm.DB, cfg config.Config) error {
	if err := SeedInitialAdmin(db, cfg); err != nil {
		return err
	}
	if err := SeedDefaultSiteSettings(db); err != nil {
		return err
	}
	if err := SeedDefaultBlogContent(db); err != nil {
		return err
	}
	return SeedCareerTimelinePosts(db)
}
