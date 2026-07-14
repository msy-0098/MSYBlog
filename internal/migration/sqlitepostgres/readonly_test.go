package sqlitepostgres

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenSQLiteReadOnlyRejectsMissingFile(t *testing.T) {
	_, err := OpenSQLiteReadOnly("does-not-exist.db")
	if err == nil {
		t.Fatal("expected a missing SQLite source to be rejected")
	}
	if !strings.Contains(err.Error(), "sqlite source") {
		t.Fatalf("expected a SQLite source error, got %v", err)
	}
}

func TestOpenSQLiteReadOnlyDoesNotModifySourceOrSidecars(t *testing.T) {
	path := createSQLiteFixture(t, false)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if suffix == "" {
			continue
		}
		if err := os.WriteFile(path+suffix, []byte("preserve me"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	before := fileState(t, path, path+"-wal", path+"-shm")
	source, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	closeGormDB(source)
	after := fileState(t, path, path+"-wal", path+"-shm")
	if before != after {
		t.Fatalf("read-only open modified SQLite source or sidecars: before=%q after=%q", before, after)
	}
}

func fileState(t *testing.T, paths ...string) string {
	t.Helper()
	var state strings.Builder
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		state.WriteString(filepath.Base(path))
		state.WriteString(":")
		state.WriteString(info.ModTime().UTC().Format("20060102150405.000000000"))
		state.WriteString(":")
		state.WriteString(string(contents))
		state.WriteString("\n")
	}
	return state.String()
}
