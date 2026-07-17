package router_test

import (
	"database/sql"
	"os"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
)

type testSQLDatabase struct {
	db   *sql.DB
	once sync.Once
	err  error
}

func (db *testSQLDatabase) Close() error {
	db.once.Do(func() {
		db.err = db.db.Close()
	})
	return db.err
}

func testDatabaseConfig(t *testing.T) config.Config {
	t.Helper()

	dsn := os.Getenv("BLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("BLOG_TEST_DATABASE_DSN is required for PostgreSQL router integration tests")
	}

	cfg, err := config.Load("__missing_test_config__.yaml")
	if err != nil {
		t.Fatalf("load test config: %v", err)
	}
	cfg.Database.Driver = "postgres"
	cfg.Database.DSN = dsn
	lockPostgresTestDatabase(t, cfg)
	return cfg
}

func lockPostgresTestDatabase(t *testing.T, cfg config.Config) {
	t.Helper()

	db, err := gorm.Open(postgres.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open lock database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get lock sql database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("SELECT pg_advisory_unlock(?)", int64(81220260709)).Error
		_ = sqlDB.Close()
	})

	if err := db.Exec("SELECT pg_advisory_lock(?)", int64(81220260709)).Error; err != nil {
		t.Fatalf("lock postgres test database: %v", err)
	}
}

func resetPostgresSchema(t *testing.T, cfg config.Config) {
	t.Helper()

	db, err := gorm.Open(postgres.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("open reset database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get reset sql database: %v", err)
	}
	defer sqlDB.Close()

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
}

func trackSQLDatabase(t *testing.T, db *gorm.DB) *testSQLDatabase {
	t.Helper()

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}

	tracked := &testSQLDatabase{db: sqlDB}
	t.Cleanup(func() {
		_ = tracked.Close()
	})
	return tracked
}
