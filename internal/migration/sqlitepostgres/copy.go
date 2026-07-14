package sqlitepostgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
)

// CopyAll copies every business table in dependency order while retaining the
// source primary keys. It must be called with a PostgreSQL transaction.
func CopyAll(ctx context.Context, tx, source *gorm.DB) ([]TableReport, error) {
	tables := ExistingBusinessTables()
	for _, spec := range tables {
		if err := copyTable(ctx, tx, source, spec); err != nil {
			return nil, err
		}
	}
	return scanTables(ctx, source, tables)
}

func copyTable(ctx context.Context, tx, source *gorm.DB, spec TableSpec) error {
	if !source.Migrator().HasTable(spec.Name) {
		if spec.SourceOptional {
			return nil
		}
		return fmt.Errorf("SQLite source table %s is missing", spec.Name)
	}
	switch spec.Name {
	case "site_settings":
		return copyTyped[model.SiteSetting](ctx, tx, source, spec)
	case "users":
		return copyTyped[model.User](ctx, tx, source, spec)
	case "email_verification_codes":
		return copyTyped[model.EmailVerificationCode](ctx, tx, source, spec)
	case "categories":
		return copyTyped[model.Category](ctx, tx, source, spec)
	case "tags":
		return copyTyped[model.Tag](ctx, tx, source, spec)
	case "posts":
		return copyTyped[model.Post](ctx, tx, source, spec)
	case "post_tags":
		return copyTyped[postTag](ctx, tx, source, spec)
	case "comments":
		return copyTyped[model.Comment](ctx, tx, source, spec)
	case "projects":
		return copyTyped[model.Project](ctx, tx, source, spec)
	case "uploads":
		return copyTyped[model.Upload](ctx, tx, source, spec)
	case "access_logs":
		return copyTyped[model.AccessLog](ctx, tx, source, spec)
	case "ip_bans":
		return copyTyped[model.IPBan](ctx, tx, source, spec)
	case "ai_conversations":
		return copyTyped[model.AIConversation](ctx, tx, source, spec)
	case "ai_messages":
		return copyTyped[model.AIMessage](ctx, tx, source, spec)
	default:
		return fmt.Errorf("unsupported migration table %q", spec.Name)
	}
}

func copyTyped[T any](ctx context.Context, tx, source *gorm.DB, spec TableSpec) error {
	var rows []T
	if err := source.WithContext(ctx).Table(spec.Name).Order(spec.OrderColumn).Find(&rows).Error; err != nil {
		return fmt.Errorf("read SQLite table %s: %w", spec.Name, err)
	}
	if len(rows) == 0 {
		return nil
	}
	if err := tx.WithContext(ctx).Table(spec.Name).CreateInBatches(&rows, 100).Error; err != nil {
		return fmt.Errorf("copy table %s: %w", spec.Name, err)
	}
	return nil
}
