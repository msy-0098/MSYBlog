package sqlitepostgres

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/database"
)

// Options configures a single safe SQLite-to-PostgreSQL migration.
type Options struct {
	SQLitePath  string
	PostgresDSN string
	DryRun      bool
}

// Run scans a read-only SQLite source and either reports its contents or copies
// it transactionally to an empty PostgreSQL target. It never seeds defaults.
func Run(ctx context.Context, options Options) (Report, error) {
	if strings.TrimSpace(options.SQLitePath) == "" {
		return Report{}, fmt.Errorf("SQLite source path is required")
	}
	if strings.TrimSpace(options.PostgresDSN) == "" {
		return Report{}, fmt.Errorf("PostgreSQL DSN is required")
	}

	source, err := OpenSQLiteReadOnly(options.SQLitePath)
	if err != nil {
		return Report{}, err
	}
	defer closeGormDB(source)

	target, err := OpenPostgres(options.PostgresDSN)
	if err != nil {
		return Report{}, err
	}
	defer closeGormDB(target)

	tables := ExistingBusinessTables()
	if err := EnsureTargetEmpty(target, tables); err != nil {
		return Report{}, err
	}
	if options.DryRun {
		return Scan(ctx, source, tables)
	}

	// Schema creation is intentionally after the dry-run branch: dry runs make
	// no writes at all, while real migrations create only schema and never seed.
	if err := database.AutoMigrate(target); err != nil {
		return Report{}, fmt.Errorf("create PostgreSQL schema: %w", err)
	}
	if err := EnsureTargetEmpty(target, tables); err != nil {
		return Report{}, err
	}
	return CopyAndValidate(ctx, source, target)
}

// Scan produces the same source table report as a migration without modifying
// either database.
func Scan(ctx context.Context, source *gorm.DB, tables []TableSpec) (Report, error) {
	tableReports, err := scanTables(ctx, source, tables)
	if err != nil {
		return Report{}, err
	}
	return Report{Tables: tableReports}, nil
}

// CopyAndValidate makes all target changes in one transaction. Any copy,
// sequence reset, count, relationship, or digest failure rolls everything back.
func CopyAndValidate(ctx context.Context, source, target *gorm.DB) (Report, error) {
	var report Report
	err := target.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := CopyAll(ctx, tx, source); err != nil {
			return err
		}
		if err := ResetSequences(tx, ExistingBusinessTables()); err != nil {
			return err
		}
		validated, err := ValidateMigration(ctx, source, tx)
		if err != nil {
			return err
		}
		report = validated
		return nil
	})
	if err != nil {
		return Report{}, fmt.Errorf("SQLite to PostgreSQL migration failed: %w", err)
	}
	return report, nil
}

func closeGormDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}
