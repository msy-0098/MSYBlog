package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"masenyu.top/blog/backend/internal/migration/sqlitepostgres"
)

var passwordPattern = regexp.MustCompile(`(?i)(password\s*=\s*)\S+`)

func main() {
	sqlitePath := flag.String("sqlite-path", "", "read-only SQLite source path")
	dryRun := flag.Bool("dry-run", false, "validate and report without writing PostgreSQL")
	flag.Parse()

	if strings.TrimSpace(*sqlitePath) == "" {
		fatal("--sqlite-path is required")
	}
	postgresDSN := os.Getenv("BLOG_DATABASE_DSN")
	if strings.TrimSpace(postgresDSN) == "" {
		fatal("BLOG_DATABASE_DSN is required")
	}

	report, err := sqlitepostgres.Run(context.Background(), sqlitepostgres.Options{
		SQLitePath:  *sqlitePath,
		PostgresDSN: postgresDSN,
		DryRun:      *dryRun,
	})
	if err != nil {
		fatal("migration failed: " + redactSensitive(err.Error(), postgresDSN))
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fatal("write migration report: " + err.Error())
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "migration:", message)
	os.Exit(1)
}

func redactSensitive(message, dsn string) string {
	message = strings.ReplaceAll(message, dsn, "[REDACTED]")
	return passwordPattern.ReplaceAllString(message, "${1}[REDACTED]")
}
