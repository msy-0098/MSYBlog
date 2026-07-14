package sqlitepostgres

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenSQLiteReadOnly opens an existing SQLite database without allowing writes.
// It is intentionally confined to the migration package so the application
// runtime continues to support PostgreSQL only.
func OpenSQLiteReadOnly(path string) (*gorm.DB, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite source: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("sqlite source: %q is a directory", path)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("sqlite source: resolve path: %w", err)
	}

	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(absolutePath)+"?mode=ro"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite source read-only: %w", err)
	}
	return db, nil
}
