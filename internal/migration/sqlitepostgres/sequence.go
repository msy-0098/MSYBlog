package sqlitepostgres

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// ResetSequences advances every PostgreSQL primary-key sequence after explicit
// ID insertion, so subsequent application writes cannot collide with migrated
// rows. It is intended to run inside the copy transaction.
func ResetSequences(tx *gorm.DB, tables []TableSpec) error {
	for _, table := range tables {
		if !table.HasSequence {
			continue
		}
		if !isSafeIdentifier(table.Name) {
			return fmt.Errorf("unsafe table name %q", table.Name)
		}
		if !tx.Migrator().HasTable(table.Name) {
			continue
		}

		var sequenceName *string
		if err := tx.Raw(`SELECT pg_get_serial_sequence(?, 'id')`, table.Name).Scan(&sequenceName).Error; err != nil {
			return fmt.Errorf("lookup sequence for %s: %w", table.Name, err)
		}
		// Empty newly-added tables or identity-only columns may not expose a serial sequence.
		if sequenceName == nil || strings.TrimSpace(*sequenceName) == "" {
			continue
		}

		quotedTable := `"` + table.Name + `"`
		statement := `SELECT setval(?, COALESCE((SELECT MAX(id) FROM ` + quotedTable + `), 1), EXISTS(SELECT 1 FROM ` + quotedTable + `))`
		if err := tx.Exec(statement, *sequenceName).Error; err != nil {
			return fmt.Errorf("reset sequence for %s: %w", table.Name, err)
		}
	}
	return nil
}

func isSafeIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return !strings.Contains(value, "..")
}