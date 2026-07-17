package sqlitepostgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/model"
)

func TestDigestColumnsCoverSecurityCriticalFields(t *testing.T) {
	for table, requiredColumns := range map[string][]string{
		"projects":         {"visible"},
		"ip_bans":          {"active", "expires_at"},
		"ai_conversations": {"last_message_at", "created_at", "updated_at"},
		"ai_messages":      {"status", "created_at", "updated_at"},
	} {
		for _, requiredColumn := range requiredColumns {
			if !contains(digestColumns[table], requiredColumn) {
				t.Fatalf("digest for %s must include %s", table, requiredColumn)
			}
		}
	}
}

func TestCanonicalDigestValueNormalizesBooleanForms(t *testing.T) {
	if got, want := canonicalDigestValue(true), "1"; got != want {
		t.Fatalf("bool true = %q, want %q", got, want)
	}
	if got, want := canonicalDigestValue(false), "0"; got != want {
		t.Fatalf("bool false = %q, want %q", got, want)
	}
	if got, want := canonicalDigestValue(int64(1)), "1"; got != want {
		t.Fatalf("int64 true = %q, want %q", got, want)
	}
	if got, want := canonicalDigestValue(int64(0)), "0"; got != want {
		t.Fatalf("int64 false = %q, want %q", got, want)
	}
}

func TestCanonicalDigestValueNormalizesTimestampPrecision(t *testing.T) {
	value := time.Date(2026, 7, 14, 12, 0, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))
	if got, want := canonicalDigestValue(value), "2026-07-14T04:00:00.123456Z"; got != want {
		t.Fatalf("canonical timestamp = %q, want %q", got, want)
	}
}
func TestRunRequiresPostgresDSN(t *testing.T) {
	source := createSQLiteFixture(t, false)
	_, err := Run(context.Background(), Options{SQLitePath: source})
	if err == nil || !strings.Contains(err.Error(), "PostgreSQL DSN") {
		t.Fatalf("expected missing PostgreSQL DSN error, got %v", err)
	}
}

func TestRunDryRunDoesNotChangePostgres(t *testing.T) {
	target, targetDSN := newEmptyPostgresSchema(t)
	before := tableCounts(t, target)

	report, err := Run(context.Background(), Options{
		SQLitePath:  createSQLiteFixture(t, false),
		PostgresDSN: targetDSN,
		DryRun:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) == 0 {
		t.Fatal("expected dry-run table report")
	}
	if got := tableCounts(t, target); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Fatalf("dry-run changed target: before=%v after=%v", before, got)
	}
}

func TestRunRejectsNonEmptyPostgres(t *testing.T) {
	target, targetDSN := newEmptyPostgresSchema(t)
	if err := target.Create(&model.SiteSetting{Key: "already-there", Value: "1"}).Error; err != nil {
		t.Fatal(err)
	}

	_, err := Run(context.Background(), Options{SQLitePath: createSQLiteFixture(t, false), PostgresDSN: targetDSN})
	if err == nil || !strings.Contains(err.Error(), "target database is not empty") {
		t.Fatalf("expected non-empty target rejection, got %v", err)
	}
}

func TestRunMigratesBusinessTablesAndResetsSequences(t *testing.T) {
	target, targetDSN := newEmptyPostgresSchema(t)
	report, err := Run(context.Background(), Options{SQLitePath: createSQLiteFixture(t, false), PostgresDSN: targetDSN})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(report.TableNames(), "post_tags") {
		t.Fatalf("expected post_tags report, got %v", report.TableNames())
	}
	if got := countRows(t, target, "posts"); got != 2 {
		t.Fatalf("expected 2 posts, got %d", got)
	}
	if got := countRows(t, target, "post_tags"); got != 2 {
		t.Fatalf("expected 2 post tags, got %d", got)
	}

	post := model.Post{Title: "after migration", Slug: "after-migration", Summary: "summary", Content: "content", Status: model.PostStatusDraft, CategoryID: 2}
	if err := target.Create(&post).Error; err != nil {
		t.Fatal(err)
	}
	if post.ID <= 6 {
		t.Fatalf("expected reset sequence to allocate an ID above 6, got %d", post.ID)
	}
}

func TestRunRollsBackBrokenRelations(t *testing.T) {
	target, targetDSN := newEmptyPostgresSchema(t)
	_, err := Run(context.Background(), Options{SQLitePath: createSQLiteFixture(t, true), PostgresDSN: targetDSN})
	if err == nil {
		t.Fatal("expected broken relation to fail migration")
	}
	if got := countRows(t, target, "posts"); got != 0 {
		t.Fatalf("expected transaction rollback, found %d posts", got)
	}
}

func TestRunRollsBackBrokenAccessLogRelationAndSchema(t *testing.T) {
	target, targetDSN := newUninitializedPostgresSchema(t)
	_, err := Run(context.Background(), Options{SQLitePath: createSQLiteFixtureWithBrokenAccessLog(t), PostgresDSN: targetDSN})
	if err == nil || !strings.Contains(err.Error(), "access_logs.post_id") {
		t.Fatalf("expected broken access log relation to fail migration, got %v", err)
	}
	if target.Migrator().HasTable("posts") {
		t.Fatal("a failed migration must roll back the PostgreSQL schema as well as copied data")
	}
}
func TestValidateMigrationDetectsCriticalDigestMismatch(t *testing.T) {
	target, _ := newEmptyPostgresSchema(t)
	sourcePath := createSQLiteFixture(t, false)
	source, err := OpenSQLiteReadOnly(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(target); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyAndValidate(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	if err := target.Model(&model.Post{}).Where("id = ?", 5).Update("content", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMigration(context.Background(), source, target); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestValidateMigrationDetectsSecurityCriticalDigestMismatch(t *testing.T) {
	target, _ := newEmptyPostgresSchema(t)
	sourcePath := createSQLiteFixture(t, false)
	source, err := OpenSQLiteReadOnly(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeGormDB(source) })
	if _, err := CopyAndValidate(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}

	expiresAt := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	lastMessageAt := time.Date(2027, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := target.Model(&model.Project{}).Where("id = ?", 3).Update("visible", false).Error; err != nil {
		t.Fatalf("change project visibility: %v", err)
	}
	if err := target.Model(&model.IPBan{}).Where("id = ?", 2).Updates(map[string]any{"active": false, "expires_at": expiresAt}).Error; err != nil {
		t.Fatalf("change IP ban state: %v", err)
	}
	if err := target.Model(&model.AIConversation{}).Where("id = ?", 2).Update("last_message_at", lastMessageAt).Error; err != nil {
		t.Fatalf("change AI conversation state: %v", err)
	}
	if _, err := ValidateMigration(context.Background(), source, target); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("expected security-critical field digest mismatch, got %v", err)
	}
}

func newEmptyPostgresSchema(t *testing.T) (*gorm.DB, string) {
	return newPostgresSchema(t, true)
}

func newUninitializedPostgresSchema(t *testing.T) (*gorm.DB, string) {
	return newPostgresSchema(t, false)
}

func newPostgresSchema(t *testing.T, migrate bool) (*gorm.DB, string) {
	t.Helper()
	baseDSN := strings.TrimSpace(os.Getenv("BLOG_TEST_DATABASE_DSN"))
	if baseDSN == "" {
		t.Skip("BLOG_TEST_DATABASE_DSN is not set; skipping PostgreSQL integration test")
	}

	admin, err := gorm.Open(postgres.Open(baseDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open PostgreSQL test database: %v", err)
	}
	schema := fmt.Sprintf("sqlite_migration_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA \"" + schema + "\"").Error; err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA IF EXISTS \"" + schema + "\" CASCADE").Error
	})

	targetDSN := baseDSN + " search_path=" + schema
	target, err := gorm.Open(postgres.Open(targetDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open PostgreSQL test schema: %v", err)
	}
	t.Cleanup(func() { closeGormDB(target) })
	if migrate {
		if err := database.AutoMigrate(target); err != nil {
			t.Fatalf("migrate test schema: %v", err)
		}
	}
	return target, targetDSN
}

func createSQLiteFixture(t *testing.T, brokenRelation bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.db")
	source, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(source); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 13, 8, 0, 0, 0, time.UTC)
	mustCreate(t, source, &model.SiteSetting{Key: "siteTitle", Value: "migration fixture"})
	mustCreate(t, source, &model.User{ID: 1, Username: "admin", Email: "admin@example.test", Nickname: "Admin", Role: model.UserRoleAdmin, PasswordHash: "hash"})
	mustCreate(t, source, &model.EmailVerificationCode{ID: 1, Email: "admin@example.test", CodeHash: "code", ExpiresAt: now.Add(time.Hour)})
	mustCreate(t, source, &model.Category{ID: 2, Name: "Go", Slug: "go"})
	mustCreate(t, source, &model.Tag{ID: 3, Name: "Database", Slug: "database"})
	categoryID := uint(2)
	if brokenRelation {
		categoryID = 999
	}
	mustCreate(t, source, &model.Post{ID: 5, Title: "First", Slug: "first", Summary: "first summary", Content: "first content", Status: model.PostStatusPublished, ViewCount: 7, CategoryID: categoryID, PublishedAt: now})
	mustCreate(t, source, &model.Post{ID: 6, Title: "Second", Slug: "second", Summary: "second summary", Content: "second content", Status: model.PostStatusDraft, CategoryID: categoryID, PublishedAt: now})
	mustCreate(t, source, &postTag{PostID: 5, TagID: 3})
	mustCreate(t, source, &postTag{PostID: 6, TagID: 3})
	mustCreate(t, source, &model.Comment{ID: 4, PostID: 5, UserID: 1, Content: "hello", Status: model.CommentStatusApproved})
	mustCreate(t, source, &model.Project{ID: 3, Name: "Blog", Description: "project", TechStack: "[\"Go\"]", Visible: true})
	mustCreate(t, source, &model.Upload{ID: 2, Filename: "cover.png", Path: "/uploads/cover.png", MimeType: "image/png", Size: 42})
	postID := uint(5)
	mustCreate(t, source, &model.AccessLog{ID: 3, IP: "127.0.0.1", Method: "GET", Path: "/api/site", Status: 200, PostID: &postID})
	mustCreate(t, source, &model.IPBan{ID: 2, IP: "192.0.2.1", Reason: "fixture", Active: true})
	mustCreate(t, source, &model.AIConversation{ID: 2, Title: "chat", TitleMode: model.AIConversationTitleModeAuto, CreatedBy: 1, Model: "test", MessageCount: 1})
	mustCreate(t, source, &model.AIMessage{ID: 3, ConversationID: 2, Role: model.AIMessageRoleUser, Content: "hello AI", Status: model.AIMessageStatusCompleted, Sequence: 1, Model: "test"})

	sqlDB, err := source.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func createSQLiteFixtureWithBrokenAccessLog(t *testing.T) string {
	t.Helper()
	path := createSQLiteFixture(t, false)
	source, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	invalidPostID := uint(999)
	if err := source.Model(&model.AccessLog{}).Where("id = ?", 3).Update("post_id", invalidPostID).Error; err != nil {
		t.Fatalf("create broken access log fixture: %v", err)
	}
	closeGormDB(source)
	return path
}
func mustCreate(t *testing.T, db *gorm.DB, value any) {
	t.Helper()
	if err := db.Create(value).Error; err != nil {
		t.Fatal(err)
	}
}

func tableCounts(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64, len(ExistingBusinessTables()))
	for _, table := range ExistingBusinessTables() {
		counts[table.Name] = countRows(t, db, table.Name)
	}
	return counts
}

func countRows(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
