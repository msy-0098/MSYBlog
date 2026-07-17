package sqlitepostgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// TableReport is a count and content summary for one migrated business table.
type TableReport struct {
	Name       string `json:"name"`
	SourceRows int64  `json:"sourceRows"`
	TargetRows int64  `json:"targetRows"`
	Digest     string `json:"digest"`
}

// Report is emitted for both dry runs and completed migrations.
type Report struct {
	Tables []TableReport `json:"tables"`
}

func (r Report) TableNames() []string {
	names := make([]string, 0, len(r.Tables))
	for _, table := range r.Tables {
		names = append(names, table.Name)
	}
	return names
}

func scanTables(ctx context.Context, source *gorm.DB, tables []TableSpec) ([]TableReport, error) {
	reports := make([]TableReport, 0, len(tables))
	for _, spec := range tables {
		if !source.Migrator().HasTable(spec.Name) {
			if spec.SourceOptional {
				reports = append(reports, TableReport{Name: spec.Name, Digest: emptyDigest()})
				continue
			}
			return nil, fmt.Errorf("SQLite source table %s is missing", spec.Name)
		}
		count, err := tableCount(ctx, source, spec.Name)
		if err != nil {
			return nil, fmt.Errorf("count SQLite table %s: %w", spec.Name, err)
		}
		digest, err := tableDigest(ctx, source, spec.Name)
		if err != nil {
			return nil, fmt.Errorf("digest SQLite table %s: %w", spec.Name, err)
		}
		reports = append(reports, TableReport{Name: spec.Name, SourceRows: count, Digest: digest})
	}
	return reports, nil
}

// ValidateMigration compares source and target row counts and deterministic
// content digests, then checks all foreign-key-like business relations.
func ValidateMigration(ctx context.Context, source, target *gorm.DB) (Report, error) {
	sourceReports, err := scanTables(ctx, source, ExistingBusinessTables())
	if err != nil {
		return Report{}, err
	}
	for index := range sourceReports {
		report := &sourceReports[index]
		targetCount, err := tableCount(ctx, target, report.Name)
		if err != nil {
			return Report{}, fmt.Errorf("count PostgreSQL table %s: %w", report.Name, err)
		}
		if report.SourceRows != targetCount {
			return Report{}, fmt.Errorf("count mismatch for %s: SQLite=%d PostgreSQL=%d", report.Name, report.SourceRows, targetCount)
		}
		targetDigest, err := tableDigest(ctx, target, report.Name)
		if err != nil {
			return Report{}, fmt.Errorf("digest PostgreSQL table %s: %w", report.Name, err)
		}
		if report.Digest != targetDigest {
			return Report{}, fmt.Errorf("digest mismatch for %s", report.Name)
		}
		report.TargetRows = targetCount
	}
	if err := validateRelations(ctx, target); err != nil {
		return Report{}, err
	}
	return Report{Tables: sourceReports}, nil
}

func validateRelations(ctx context.Context, target *gorm.DB) error {
	checks := []struct {
		name  string
		query string
	}{
		{"posts.category_id", "SELECT COUNT(*) FROM posts p LEFT JOIN categories c ON c.id = p.category_id WHERE c.id IS NULL"},
		{"post_tags.post_id", "SELECT COUNT(*) FROM post_tags pt LEFT JOIN posts p ON p.id = pt.post_id WHERE p.id IS NULL"},
		{"post_tags.tag_id", "SELECT COUNT(*) FROM post_tags pt LEFT JOIN tags t ON t.id = pt.tag_id WHERE t.id IS NULL"},
		{"comments.post_id", "SELECT COUNT(*) FROM comments c LEFT JOIN posts p ON p.id = c.post_id WHERE p.id IS NULL"},
		{"comments.user_id", "SELECT COUNT(*) FROM comments c LEFT JOIN users u ON u.id = c.user_id WHERE u.id IS NULL"},
		{"post_likes.post_id", "SELECT COUNT(*) FROM post_likes pl LEFT JOIN posts p ON p.id = pl.post_id WHERE p.id IS NULL"},
		{"access_logs.post_id", "SELECT COUNT(*) FROM access_logs a LEFT JOIN posts p ON p.id = a.post_id WHERE a.post_id IS NOT NULL AND p.id IS NULL"},
		{"ai_conversations.created_by", "SELECT COUNT(*) FROM ai_conversations c LEFT JOIN users u ON u.id = c.created_by WHERE u.id IS NULL"},
		{"ai_messages.conversation_id", "SELECT COUNT(*) FROM ai_messages m LEFT JOIN ai_conversations c ON c.id = m.conversation_id WHERE c.id IS NULL"},
	}
	for _, check := range checks {
		var orphaned int64
		if err := target.WithContext(ctx).Raw(check.query).Scan(&orphaned).Error; err != nil {
			return fmt.Errorf("validate relation %s: %w", check.name, err)
		}
		if orphaned != 0 {
			return fmt.Errorf("relation validation failed for %s: %d orphaned rows", check.name, orphaned)
		}
	}
	return nil
}

func tableCount(ctx context.Context, db *gorm.DB, table string) (int64, error) {
	var count int64
	if err := db.WithContext(ctx).Table(table).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func tableDigest(ctx context.Context, db *gorm.DB, table string) (string, error) {
	columns, ok := digestColumns[table]
	if !ok {
		return "", fmt.Errorf("no digest columns configured")
	}
	rows, err := db.WithContext(ctx).Table(table).Select(strings.Join(columns, ", ")).Order(digestOrders[table]).Rows()
	if err != nil {
		return "", err
	}
	defer rows.Close()

	hash := sha256.New()
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return "", err
		}
		for _, value := range values {
			_, _ = hash.Write([]byte(canonicalDigestValue(value)))
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write([]byte{'\n'})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func emptyDigest() string {
	return hex.EncodeToString(sha256.New().Sum(nil))
}

func canonicalDigestValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "<nil>"
	case []byte:
		return string(typed)
	case time.Time:
		return typed.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	case *time.Time:
		if typed == nil {
			return "<nil>"
		}
		return typed.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
	case bool:
		if typed {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(typed)
	}
}

var digestColumns = map[string][]string{
	"site_settings":            {"key", "value", "created_at", "updated_at"},
	"users":                    {"id", "username", "email", "nickname", "role", "password_hash", "created_at", "updated_at"},
	"email_verification_codes": {"id", "email", "code_hash", "used_at", "expires_at", "created_at", "updated_at"},
	"categories":               {"id", "name", "slug", "created_at", "updated_at"},
	"tags":                     {"id", "name", "slug", "created_at", "updated_at"},
	"posts":                    {"id", "title", "slug", "summary", "content", "cover", "status", "view_count", "category_id", "created_at", "updated_at", "published_at"},
	"post_tags":                {"post_id", "tag_id"},
	"post_likes":               {"id", "post_id", "ip", "created_at", "updated_at"},
	"comments":                 {"id", "post_id", "user_id", "content", "status", "created_at", "updated_at"},
	"projects":                 {"id", "name", "description", "url", "cover", "tech_stack", "sort", "visible", "created_at", "updated_at"},
	"friend_links":             {"id", "name", "url", "description", "logo", "sort", "visible", "created_at", "updated_at"},
	"uploads":                  {"id", "filename", "path", "mime_type", "size", "created_at"},
	"access_logs":              {"id", "ip", "method", "path", "status", "user_agent", "post_id", "created_at"},
	"ip_bans":                  {"id", "ip", "reason", "active", "expires_at", "created_at", "updated_at"},
	"ai_conversations":         {"id", "title", "title_mode", "created_by", "model", "message_count", "last_message_at", "created_at", "updated_at"},
	"ai_messages":              {"id", "conversation_id", "role", "content", "status", "sequence", "model", "error_message", "created_at", "updated_at"},
}
var digestOrders = map[string]string{
	"site_settings":            "key",
	"post_tags":                "post_id, tag_id",
	"users":                    "id",
	"email_verification_codes": "id",
	"categories":               "id",
	"tags":                     "id",
	"posts":                    "id",
	"post_likes":               "id",
	"comments":                 "id",
	"projects":                 "id",
	"friend_links":             "id",
	"uploads":                  "id",
	"access_logs":              "id",
	"ip_bans":                  "id",
	"ai_conversations":         "id",
	"ai_messages":              "id",
}
