package sqlitepostgres

import (
	"fmt"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenPostgres opens the migration target without logging its DSN.
func OpenPostgres(dsn string) (*gorm.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("PostgreSQL DSN is required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL target")
	}
	return db, nil
}

// EnsureTargetEmpty refuses to merge historic SQLite data into a populated
// business table. Tables absent from an uninitialized target are considered
// empty, allowing the real migration to create schema explicitly.
func EnsureTargetEmpty(target *gorm.DB, tables []TableSpec) error {
	for _, table := range tables {
		if !target.Migrator().HasTable(table.Name) {
			continue
		}
		var count int64
		if err := target.Table(table.Name).Count(&count).Error; err != nil {
			return fmt.Errorf("count target table %s: %w", table.Name, err)
		}
		if count != 0 {
			return fmt.Errorf("target database is not empty: %s contains %d rows", table.Name, count)
		}
	}
	return nil
}
