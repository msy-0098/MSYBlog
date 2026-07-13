package router_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestDashboardDoesNotCallAIClient(t *testing.T) {
	engine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, panicOnChat: true})
	token := loginAndGetToken(t, engine)
	response := performJSONRequest(engine, http.MethodGet, "/api/admin/dashboard", nil, token)
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "aiAnalysis") {
		t.Fatalf("dashboard must not include AI insight: %s", response.Body.String())
	}
}

func TestAdminInsightGenerationIsIndependentAndRequiresConfiguredClient(t *testing.T) {
	configured := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, chatAnswer: "建议继续发布高质量文章。"})
	token := loginAndGetToken(t, configured)
	generated := performJSONRequest(configured, http.MethodPost, "/api/admin/ai/insights/generate", nil, token)
	if generated.Code != http.StatusOK {
		t.Fatalf("generate insight: %d %s", generated.Code, generated.Body.String())
	}
	if !strings.Contains(generated.Body.String(), "建议继续发布高质量文章。") {
		t.Fatalf("missing AI insight answer: %s", generated.Body.String())
	}

	unconfigured := newAdminAITestEngine(t, &fakeAdminAIClient{})
	unconfiguredToken := loginAndGetToken(t, unconfigured)
	unavailable := performJSONRequest(unconfigured, http.MethodPost, "/api/admin/ai/insights/generate", nil, unconfiguredToken)
	if unavailable.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unconfigured insight status 503, got %d %s", unavailable.Code, unavailable.Body.String())
	}
}
