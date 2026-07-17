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

// Shared with database/service packages: session advisory lock for public schema tests.
const postgresTestLockKey int64 = 81220260709

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

// In-process reentrant gate: nested lockPostgresTestDatabase calls inside the same
// *testing.T must not open a second session-level advisory lock (that deadlocks).
// Cross-package isolation still relies on pg_advisory_lock below.
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
	lockPostgresTestDatabase(t, dsn)
	return cfg
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
	// Session-level advisory locks must stay on one connection.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	// Fail fast in CI instead of hanging until the package test timeout.
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