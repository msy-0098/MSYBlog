package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDashboardDTOOmitsAIAnalysis(t *testing.T) {
	encoded, err := json.Marshal(DashboardDTO{})
	if err != nil {
		t.Fatalf("marshal dashboard: %v", err)
	}
	if strings.Contains(string(encoded), "aiAnalysis") {
		t.Fatalf("dashboard payload must not synchronously include AI analysis: %s", encoded)
	}
}
