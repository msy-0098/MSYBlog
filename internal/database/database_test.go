package database

import (
	"database/sql"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
)

// Shared with router/service packages.
const postgresTestLockKey int64 = 81220260709

// In-process reentrant gate so nested helpers in the same *testing.T do not open
// a second session-level advisory lock (that deadlocks on the same key).
var postgresTestGate = struct {
	mu     sync.Mutex
	cond   *sync.Cond
	owner  *testing.T
	depth  int
	lockDB *gorm.DB
	sqlDB  *sql.DB
}{}

func init() {
	postgresTestGate.cond = sync.NewCond(&postgresTestGate.mu)
}

func TestAutoMigrateCreatesOnlySchemaIncludingAIModels(t *testing.T) {
	db := testPostgresDatabase(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if !db.Migrator().HasTable(&model.AIConversation{}) {
		t.Fatal("AutoMigrate must create ai_conversations")
	}
	if !db.Migrator().HasTable(&model.AIMessage{}) {
		t.Fatal("AutoMigrate must create ai_messages")
	}

	for _, table := range []any{&model.User{}, &model.SiteSetting{}, &model.Post{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatalf("count %T rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("AutoMigrate must not seed %T rows, got %d", table, count)
		}
	}
}

func TestSeedDefaultsWritesDataAfterSchema(t *testing.T) {
	db := testPostgresDatabase(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := SeedDefaults(db, config.Default()); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	for _, table := range []any{&model.User{}, &model.SiteSetting{}, &model.Post{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatalf("count %T rows: %v", table, err)
		}
		if count == 0 {
			t.Fatalf("SeedDefaults must write %T rows after schema migration", table)
		}
	}
}

func TestOpenDoesNotSeedExistingBusinessData(t *testing.T) {
	db := testPostgresDatabase(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	const username = "existing-admin"
	const originalHash = "keep-existing-password-hash"
	if err := db.Create(&model.User{
		Username:     username,
		Email:        "existing-admin@example.test",
		Nickname:     "Existing admin",
		Role:         model.UserRoleAdmin,
		PasswordHash: originalHash,
	}).Error; err != nil {
		t.Fatalf("create existing admin: %v", err)
	}

	cfg := config.Default()
	cfg.Database.DSN = os.Getenv("BLOG_TEST_DATABASE_DSN")
	cfg.Admin.InitialUsername = username
	cfg.Admin.InitialPassword = "must-not-replace-existing-password"
	opened, err := Open(Options{Config: cfg})
	if err != nil {
		t.Fatalf("open existing database: %v", err)
	}
	openedSQL, err := opened.DB()
	if err != nil {
		t.Fatalf("get opened database handle: %v", err)
	}
	t.Cleanup(func() { _ = openedSQL.Close() })

	var admin model.User
	if err := db.Where("username = ?", username).First(&admin).Error; err != nil {
		t.Fatalf("load existing admin: %v", err)
	}
	if admin.PasswordHash != originalHash {
		t.Fatal("Open must not overwrite an existing administrator password hash")
	}
	for _, table := range []any{&model.SiteSetting{}, &model.Post{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatalf("count %T rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("Open must not seed %T into an existing business database, got %d rows", table, count)
		}
	}
}

func TestOpenDoesNotSeedAnExistingEmptySchema(t *testing.T) {
	db := testPostgresDatabase(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	cfg := config.Default()
	cfg.Database.DSN = os.Getenv("BLOG_TEST_DATABASE_DSN")
	opened, err := Open(Options{Config: cfg})
	if err != nil {
		t.Fatalf("open existing empty schema: %v", err)
	}
	openedSQL, err := opened.DB()
	if err != nil {
		t.Fatalf("get opened database handle: %v", err)
	}
	t.Cleanup(func() { _ = openedSQL.Close() })

	for _, table := range []any{&model.User{}, &model.SiteSetting{}, &model.Post{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatalf("count %T rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("Open must seed only an uninitialized database, got %d %T rows", count, table)
		}
	}
}
func TestAutoMigrateEnforcesAIMessageConversationSequenceUniqueness(t *testing.T) {
	db := testPostgresDatabase(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	creator := createTestAIUser(t, db)

	conversation := model.AIConversation{
		Title:     "new conversation",
		TitleMode: model.AIConversationTitleModeAuto,
		CreatedBy: creator.ID,
		Model:     "deepseek-chat",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	if err := db.Create(&model.AIMessage{
		ConversationID: conversation.ID,
		Role:           model.AIMessageRoleUser,
		Content:        "hi",
		Status:         model.AIMessageStatusCompleted,
		Sequence:       1,
	}).Error; err != nil {
		t.Fatalf("create first message: %v", err)
	}
	if err := db.Create(&model.AIMessage{
		ConversationID: conversation.ID,
		Role:           model.AIMessageRoleAssistant,
		Content:        "duplicate",
		Status:         model.AIMessageStatusCompleted,
		Sequence:       1,
	}).Error; err == nil {
		t.Fatal("expected duplicate conversation sequence to fail")
	}
}

func TestSeedDefaultSiteSettingsIsIdempotentAfterAdminUpdates(t *testing.T) {
	db := testPostgresDatabase(t)
	if err := db.AutoMigrate(&model.SiteSetting{}); err != nil {
		t.Fatalf("migrate settings: %v", err)
	}

	if err := SeedDefaultSiteSettings(db); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	if err := db.Model(&model.SiteSetting{}).Where("key = ?", "siteTitle").Update("value", "后台改过的标题").Error; err != nil {
		t.Fatalf("update site title: %v", err)
	}
	if err := SeedDefaultSiteSettings(db); err != nil {
		t.Fatalf("seed defaults after update: %v", err)
	}

	var settings []model.SiteSetting
	if err := db.Where("key = ?", "siteTitle").Find(&settings).Error; err != nil {
		t.Fatalf("query site title: %v", err)
	}
	if len(settings) != 1 {
		t.Fatalf("expected one site title row, got %d", len(settings))
	}
	if settings[0].Value != "后台改过的标题" {
		t.Fatalf("expected admin-updated title to remain, got %q", settings[0].Value)
	}
}

func TestSiteSettingLookupUsesPostgresQuotedKeyColumn(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=127.0.0.1 user=blog_user password=test dbname=blog_test port=5432 sslmode=disable TimeZone=Asia/Shanghai",
		PreferSimpleProtocol: true,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	statement := db.Where(siteSettingLookup("siteTitle")).First(&model.SiteSetting{}).Statement
	sql := statement.SQL.String()
	if strings.Contains(sql, "WHERE key =") {
		t.Fatalf("site setting lookup must not use a bare reserved key column: %s", sql)
	}
	if !strings.Contains(sql, `"site_settings"."key"`) {
		t.Fatalf("site setting lookup should quote the key column, got: %s", sql)
	}
}

func TestOpenRejectsLegacyDatabaseDrivers(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql"} {
		t.Run(driver, func(t *testing.T) {
			cfg := config.Default()
			cfg.Database.Driver = driver
			cfg.Database.DSN = "unused"

			_, err := Open(Options{Config: cfg})
			if err == nil {
				t.Fatal("expected unsupported database driver error")
			}
			if !strings.Contains(err.Error(), "unsupported database driver") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func lockPostgresTestDatabase(t *testing.T, dsn string) {
	t.Helper()

	postgresTestGate.mu.Lock()
	for postgresTestGate.owner != nil && postgresTestGate.owner != t {
		postgresTestGate.cond.Wait()
	}
	if postgresTestGate.owner == t {
		postgresTestGate.depth++
		postgresTestGate.mu.Unlock()
		t.Cleanup(func() { releasePostgresTestDatabase(t) })
		return
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		postgresTestGate.mu.Unlock()
		t.Fatalf("open lock database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		postgresTestGate.mu.Unlock()
		t.Fatalf("get lock sql database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	if err := db.Exec("SET lock_timeout TO '15s'").Error; err != nil {
		_ = sqlDB.Close()
		postgresTestGate.mu.Unlock()
		t.Fatalf("set lock timeout: %v", err)
	}
	if err := db.Exec("SELECT pg_advisory_lock(?)", postgresTestLockKey).Error; err != nil {
		_ = sqlDB.Close()
		postgresTestGate.mu.Unlock()
		t.Fatalf("lock postgres test database: %v", err)
	}

	postgresTestGate.owner = t
	postgresTestGate.depth = 1
	postgresTestGate.lockDB = db
	postgresTestGate.sqlDB = sqlDB
	postgresTestGate.mu.Unlock()
	t.Cleanup(func() { releasePostgresTestDatabase(t) })
}

func releasePostgresTestDatabase(t *testing.T) {
	postgresTestGate.mu.Lock()
	defer postgresTestGate.mu.Unlock()
	if postgresTestGate.owner != t {
		return
	}
	postgresTestGate.depth--
	if postgresTestGate.depth > 0 {
		return
	}
	if postgresTestGate.lockDB != nil {
		_ = postgresTestGate.lockDB.Exec("SELECT pg_advisory_unlock(?)", postgresTestLockKey).Error
	}
	if postgresTestGate.sqlDB != nil {
		_ = postgresTestGate.sqlDB.Close()
	}
	postgresTestGate.owner = nil
	postgresTestGate.lockDB = nil
	postgresTestGate.sqlDB = nil
	postgresTestGate.cond.Signal()
}

func testPostgresDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("BLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("BLOG_TEST_DATABASE_DSN is required for PostgreSQL database integration tests")
	}

	// Cross-package isolation; reentrant within the same *testing.T.
	lockPostgresTestDatabase(t, dsn)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	// Keep schema work on one session connection.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	if err := db.Exec(`
		DROP TABLE IF EXISTS
			ai_messages,
			ai_conversations,
			post_tags,
			post_likes,
			comments,
			posts,
			categories,
			tags,
			projects,
			friend_links,
			uploads,
			email_verification_codes,
			access_logs,
			ip_bans,
			users,
			site_settings
		CASCADE
	`).Error; err != nil {
		t.Fatalf("drop test tables: %v", err)
	}

	return db
}

func TestModelsRegistryMigratesEveryExpectedTable(t *testing.T) {
	db := testPostgresDatabase(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	expected := []struct {
		name  string
		model any
	}{
		{name: "site settings", model: &model.SiteSetting{}},
		{name: "users", model: &model.User{}},
		{name: "email verification codes", model: &model.EmailVerificationCode{}},
		{name: "categories", model: &model.Category{}},
		{name: "tags", model: &model.Tag{}},
		{name: "posts", model: &model.Post{}},
		{name: "post likes", model: &model.PostLike{}},
		{name: "comments", model: &model.Comment{}},
		{name: "projects", model: &model.Project{}},
		{name: "friend links", model: &model.FriendLink{}},
		{name: "uploads", model: &model.Upload{}},
		{name: "access logs", model: &model.AccessLog{}},
		{name: "IP bans", model: &model.IPBan{}},
		{name: "AI conversations", model: &model.AIConversation{}},
		{name: "AI messages", model: &model.AIMessage{}},
	}
	registered := Models()

	for _, tt := range expected {
		t.Run(tt.name, func(t *testing.T) {
			registeredModel := false
			for _, candidate := range registered {
				if reflect.TypeOf(candidate) == reflect.TypeOf(tt.model) {
					registeredModel = true
					break
				}
			}
			if !registeredModel {
				t.Fatalf("Models() is missing %T", tt.model)
			}
			if !db.Migrator().HasTable(tt.model) {
				t.Fatalf("AutoMigrate did not create the table for %T", tt.model)
			}
		})
	}
}

func TestAutoMigrateEnforcesAIConversationForeignKeysAndCascades(t *testing.T) {
	db := testPostgresDatabase(t)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := db.Create(&model.AIConversation{
		Title:     "orphaned creator",
		TitleMode: model.AIConversationTitleModeAuto,
		CreatedBy: 999999,
		Model:     "deepseek-chat",
	}).Error; err == nil {
		t.Fatal("expected conversation with a missing creator to fail")
	}
	if err := db.Create(&model.AIMessage{
		ConversationID: 999999,
		Role:           model.AIMessageRoleUser,
		Content:        "orphaned conversation",
		Status:         model.AIMessageStatusCompleted,
		Sequence:       1,
	}).Error; err == nil {
		t.Fatal("expected message with a missing conversation to fail")
	}

	creator := createTestAIUser(t, db)
	conversation := model.AIConversation{
		Title:     "cascade conversation",
		TitleMode: model.AIConversationTitleModeAuto,
		CreatedBy: creator.ID,
		Model:     "deepseek-chat",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	message := model.AIMessage{
		ConversationID: conversation.ID,
		Role:           model.AIMessageRoleUser,
		Content:        "cascade message",
		Status:         model.AIMessageStatusCompleted,
		Sequence:       1,
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := db.Delete(&conversation).Error; err != nil {
		t.Fatalf("delete conversation: %v", err)
	}
	var messageCount int64
	if err := db.Model(&model.AIMessage{}).Where("conversation_id = ?", conversation.ID).Count(&messageCount).Error; err != nil {
		t.Fatalf("count deleted conversation messages: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("deleting a conversation must delete its messages, got %d", messageCount)
	}

	conversation = model.AIConversation{
		Title:     "creator cascade conversation",
		TitleMode: model.AIConversationTitleModeAuto,
		CreatedBy: creator.ID,
		Model:     "deepseek-chat",
	}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatalf("create conversation for creator cascade: %v", err)
	}
	message = model.AIMessage{
		ConversationID: conversation.ID,
		Role:           model.AIMessageRoleAssistant,
		Content:        "creator cascade message",
		Status:         model.AIMessageStatusCompleted,
		Sequence:       1,
	}
	if err := db.Create(&message).Error; err != nil {
		t.Fatalf("create message for creator cascade: %v", err)
	}
	if err := db.Delete(&creator).Error; err != nil {
		t.Fatalf("delete creator: %v", err)
	}

	var conversationCount int64
	if err := db.Model(&model.AIConversation{}).Where("created_by = ?", creator.ID).Count(&conversationCount).Error; err != nil {
		t.Fatalf("count deleted creator conversations: %v", err)
	}
	if conversationCount != 0 {
		t.Fatalf("deleting a creator must delete conversations, got %d", conversationCount)
	}
	if err := db.Model(&model.AIMessage{}).Where("conversation_id = ?", conversation.ID).Count(&messageCount).Error; err != nil {
		t.Fatalf("count deleted creator messages: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("deleting a creator must delete messages through conversations, got %d", messageCount)
	}
}

func createTestAIUser(t *testing.T, db *gorm.DB) model.User {
	t.Helper()

	user := model.User{
		Username:     "ai-foreign-key-test-user",
		Email:        "ai-foreign-key-test@example.test",
		Nickname:     "AI Test User",
		Role:         model.UserRoleAdmin,
		PasswordHash: "test-password-hash",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create AI test user: %v", err)
	}
	return user
}
