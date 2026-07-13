package router_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/router"
)

type fakeAdminAIClient struct {
	configured  bool
	deltas      []string
	streamErr   error
	chatAnswer  string
	chatErr     error
	panicOnChat bool
}

var _ ai.ChatClient = (*fakeAdminAIClient)(nil)

func (f *fakeAdminAIClient) Configured() bool { return f.configured }
func (f *fakeAdminAIClient) Chat(_ context.Context, _ []ai.Message) (string, error) {
	if f.panicOnChat {
		panic("dashboard must not call AI")
	}
	if f.chatErr != nil {
		return "", f.chatErr
	}
	return f.chatAnswer, nil
}
func (f *fakeAdminAIClient) StreamChat(_ context.Context, _ []ai.Message, callback func(string) error) error {
	if f.streamErr != nil {
		return f.streamErr
	}
	for _, delta := range f.deltas {
		if err := callback(delta); err != nil {
			return err
		}
	}
	return nil
}

func newAdminAITestEngine(t *testing.T, client *fakeAdminAIClient) http.Handler {
	t.Helper()
	t.Setenv("BLOG_ADMIN_INITIAL_PASSWORD", "admin-test-password")
	t.Setenv("BLOG_JWT_SECRET", "admin-test-secret")
	cfg := testDatabaseConfig(t)
	resetPostgresSchema(t, cfg)
	db, err := database.Open(database.Options{Config: cfg})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	trackSQLDatabase(t, db)
	return router.New(router.Dependencies{Config: cfg, Database: db, AIClient: client})
}

func TestAdminAIRoutesRequireJWTAndEmitSSE(t *testing.T) {
	engine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, deltas: []string{"A", "B"}})
	if got := performRequest(engine, http.MethodGet, "/api/admin/ai/conversations"); got.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized conversations request, got %d", got.Code)
	}
	token := loginAndGetToken(t, engine)
	created := performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, token)
	if created.Code != http.StatusOK {
		t.Fatalf("create conversation: %d %s", created.Code, created.Body.String())
	}
	id := decodeConversationID(t, created)
	streamed := performJSONRequest(engine, http.MethodPost, fmt.Sprintf("/api/admin/ai/conversations/%d/messages/stream", id), map[string]any{"content": "hi"}, token)
	if got := strings.Split(streamed.Header().Get("Content-Type"), ";")[0]; got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	body := streamed.Body.String()
	for _, event := range []string{"event: meta", "event: delta", "event: done"} {
		if !strings.Contains(body, event) {
			t.Fatalf("stream missing %s: %s", event, body)
		}
	}
}

func TestAdminAIConversationCRUD(t *testing.T) {
	engine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, deltas: []string{"ok"}})
	token := loginAndGetToken(t, engine)
	created := performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, token)
	id := decodeConversationID(t, created)
	renamed := performJSONRequest(engine, http.MethodPatch, fmt.Sprintf("/api/admin/ai/conversations/%d", id), map[string]any{"title": "manual"}, token)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename conversation: %d %s", renamed.Code, renamed.Body.String())
	}
	fetched := performJSONRequest(engine, http.MethodGet, fmt.Sprintf("/api/admin/ai/conversations/%d", id), nil, token)
	if fetched.Code != http.StatusOK {
		t.Fatalf("get conversation: %d %s", fetched.Code, fetched.Body.String())
	}
	deleted := performJSONRequest(engine, http.MethodDelete, fmt.Sprintf("/api/admin/ai/conversations/%d", id), nil, token)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete conversation: %d %s", deleted.Code, deleted.Body.String())
	}
	cleared := performJSONRequest(engine, http.MethodDelete, "/api/admin/ai/conversations", nil, token)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear conversations: %d %s", cleared.Code, cleared.Body.String())
	}
}

func decodeConversationID(t *testing.T, recorder *httptest.ResponseRecorder) uint {
	t.Helper()
	var body struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	decodeJSON(t, recorder, &body)
	if body.Data.ID == 0 {
		t.Fatalf("expected created conversation ID, got body %s", recorder.Body.String())
	}
	return body.Data.ID
}
