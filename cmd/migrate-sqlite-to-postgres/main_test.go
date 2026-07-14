package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandRejectsMissingSQLitePath(t *testing.T) {
	goCommand := filepath.Join(os.Getenv("GOROOT"), "bin", "go.exe")
	if _, err := os.Stat(goCommand); err != nil {
		goCommand = "go"
	}
	command := exec.Command(goCommand, "run", ".", "--dry-run")
	command.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "go-cache"))
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("expected command to reject a missing SQLite path")
	}
	if !strings.Contains(string(output), "--sqlite-path is required") {
		t.Fatalf("expected missing SQLite path error, got %s", output)
	}
}

func TestMigrationWorkflowStopsWritesAndUsesSnapshot(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "migrate-sqlite-to-postgres.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read migration workflow: %v", err)
	}
	contents := string(workflow)
	stopIndex := strings.Index(contents, "sudo -n systemctl stop blog.service")
	copyIndex := strings.Index(contents, "cp --preserve=mode,timestamps \"${source_path}\" \"${backup_dir}/blog.db\"")
	if stopIndex < 0 || copyIndex < 0 || stopIndex > copyIndex {
		t.Fatal("workflow must stop blog.service before it snapshots SQLite")
	}
	if !strings.Contains(contents, "snapshot_path=\"${backup_dir}/blog.db\"") {
		t.Fatal("workflow must define a snapshot path in the protected backup directory")
	}
	if strings.Count(contents, "--sqlite-path \"${snapshot_path}\"") != 2 {
		t.Fatal("dry-run and migration must both read the SQLite snapshot, never the live source path")
	}
	if strings.Contains(contents, "--sqlite-path \"${source_path}\"") {
		t.Fatal("workflow must not pass the active SQLite source to the migrator")
	}
	if !strings.Contains(contents, "trap restart_service EXIT") {
		t.Fatal("workflow must restart blog.service through an EXIT trap after success or failure")
	}
}

func TestRedactSensitiveRedactsPostgresURLCredentials(t *testing.T) {
	message := "migration failed while opening postgres://blog_user:secret-value@example.test/blog"
	redacted := redactSensitive(message, "host=localhost user=other password=unrelated dbname=other")
	if strings.Contains(redacted, "secret-value") {
		t.Fatalf("PostgreSQL URL password leaked in %q", redacted)
	}
	if !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("expected a redaction marker in %q", redacted)
	}
}
