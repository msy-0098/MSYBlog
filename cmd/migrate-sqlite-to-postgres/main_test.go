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
