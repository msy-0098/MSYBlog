package router_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/database"
	"masenyu.top/blog/backend/internal/model"
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

type fakeAdminAIProvider struct {
	healthCalls int
}

var _ ai.Provider = (*fakeAdminAIProvider)(nil)

func (p *fakeAdminAIProvider) Chat(_ context.Context, _ ai.ChatRequest) (ai.ChatResult, error) {
	return ai.ChatResult{Content: "ok"}, nil
}

func (p *fakeAdminAIProvider) Stream(_ context.Context, _ ai.ChatRequest, emit func(ai.StreamChunk) error) (ai.Usage, error) {
	return ai.Usage{}, emit(ai.StreamChunk{Content: "ok"})
}

func (p *fakeAdminAIProvider) Health(_ context.Context) ai.HealthResult {
	p.healthCalls++
	return ai.HealthResult{Healthy: true}
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
	engine, _ := newAdminAITestEngineWithDatabase(t, client)
	return engine
}

func newAdminAITestEngineWithDatabase(t *testing.T, client *fakeAdminAIClient) (http.Handler, *gorm.DB) {
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
	return router.New(router.Dependencies{Config: cfg, Database: db, AIClient: client}), db
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

func TestAdminAIStatusAndHealthCheckAreProtectedAndSanitized(t *testing.T) {
	provider := &fakeAdminAIProvider{}
	cfg := testDatabaseConfig(t)
	resetPostgresSchema(t, cfg)
	statusDB, err := database.Open(database.Options{Config: cfg})
	if err != nil {
		t.Fatalf("open status database: %v", err)
	}
	trackSQLDatabase(t, statusDB)
	cfg.AI.Provider = "openai-compatible"
	cfg.AI.Model = "status-model"
	cfg.AI.APIKey = "test-api-key-must-never-appear"
	cfg.AI.BaseURL = "https://api.example.test/v1"
	engine := router.New(router.Dependencies{Config: cfg, Database: statusDB, AIClient: &fakeAdminAIClient{configured: true}, AIProvider: provider})
	if got := performRequest(engine, http.MethodGet, "/api/admin/ai/status"); got.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status request, got %d", got.Code)
	}
	token := loginAndGetToken(t, engine)

	status := performJSONRequest(engine, http.MethodGet, "/api/admin/ai/status", nil, token)
	if status.Code != http.StatusOK {
		t.Fatalf("AI status: %d %s", status.Code, status.Body.String())
	}
	if strings.Contains(status.Body.String(), cfg.AI.APIKey) || !strings.Contains(status.Body.String(), "api.example.test") {
		t.Fatalf("status leaked API key or omitted safe base URL: %s", status.Body.String())
	}

	health := performJSONRequest(engine, http.MethodPost, "/api/admin/ai/health-check", map[string]any{}, token)
	if health.Code != http.StatusOK {
		t.Fatalf("AI health check: %d %s", health.Code, health.Body.String())
	}
	if provider.healthCalls != 1 || !strings.Contains(health.Body.String(), `"healthy":true`) {
		t.Fatalf("health check result = %s, calls = %d", health.Body.String(), provider.healthCalls)
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

type sseFrame struct {
	Event string
	Data  map[string]any
}

func TestAdminAIConversationPayloadsMatchFrontendContract(t *testing.T) {
	engine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, deltas: []string{"answer"}})
	token := loginAndGetToken(t, engine)

	created := performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, token)
	id := decodeConversationID(t, created)

	listed := performRequestWithToken(engine, http.MethodGet, "/api/admin/ai/conversations", token)
	if listed.Code != http.StatusOK {
		t.Fatalf("list conversations: %d %s", listed.Code, listed.Body.String())
	}
	var listEnvelope struct {
		Data json.RawMessage `json:"data"`
	}
	decodeJSON(t, listed, &listEnvelope)
	if !strings.HasPrefix(strings.TrimSpace(string(listEnvelope.Data)), "[") {
		t.Fatalf("list data must be a direct array, got %s", listEnvelope.Data)
	}
	var summaries []struct {
		ID            uint            `json:"id"`
		LastMessageAt json.RawMessage `json:"lastMessageAt"`
	}
	if err := json.Unmarshal(listEnvelope.Data, &summaries); err != nil {
		t.Fatalf("decode summaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != id || string(summaries[0].LastMessageAt) != "null" {
		t.Fatalf("unexpected conversation summaries: %s", listEnvelope.Data)
	}

	detail := performRequestWithToken(engine, http.MethodGet, fmt.Sprintf("/api/admin/ai/conversations/%d", id), token)
	if detail.Code != http.StatusOK {
		t.Fatalf("get conversation: %d %s", detail.Code, detail.Body.String())
	}
	var detailEnvelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	decodeJSON(t, detail, &detailEnvelope)
	if _, wrapped := detailEnvelope.Data["conversation"]; wrapped {
		t.Fatalf("detail must expose conversation fields directly, got %s", detail.Body.String())
	}
	if rawID, ok := detailEnvelope.Data["id"]; !ok || string(rawID) != fmt.Sprintf("%d", id) {
		t.Fatalf("detail is missing direct id: %s", detail.Body.String())
	}
	if _, ok := detailEnvelope.Data["messages"]; !ok {
		t.Fatalf("detail is missing messages: %s", detail.Body.String())
	}

	cleared := performRequestWithToken(engine, http.MethodDelete, "/api/admin/ai/conversations", token)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear conversations: %d %s", cleared.Code, cleared.Body.String())
	}
	var clearEnvelope struct {
		Data map[string]bool `json:"data"`
	}
	decodeJSON(t, cleared, &clearEnvelope)
	if !clearEnvelope.Data["deleted"] {
		t.Fatalf("clear response must expose deleted=true, got %s", cleared.Body.String())
	}
}

func TestAdminAIStreamFramesMatchFrontendContract(t *testing.T) {
	engine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, deltas: []string{"A", "B"}})
	token := loginAndGetToken(t, engine)
	id := decodeConversationID(t, performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, token))

	streamed := performJSONRequest(engine, http.MethodPost, fmt.Sprintf("/api/admin/ai/conversations/%d/messages/stream", id), map[string]any{"content": "hi"}, token)
	if streamed.Code != http.StatusOK {
		t.Fatalf("stream conversation: %d %s", streamed.Code, streamed.Body.String())
	}
	if got := strings.Split(streamed.Header().Get("Content-Type"), ";")[0]; got != "text/event-stream" {
		t.Fatalf("expected SSE content type, got %q", got)
	}
	for header, want := range map[string]string{"Cache-Control": "no-cache", "Connection": "keep-alive", "X-Accel-Buffering": "no"} {
		if got := streamed.Header().Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}

	frames := parseSSEFrames(t, streamed.Body.String())
	if len(frames) != 4 {
		t.Fatalf("expected meta, two deltas, and done; got %#v", frames)
	}
	meta, firstDelta, secondDelta, done := frames[0], frames[1], frames[2], frames[3]
	if meta.Event != "meta" || asUint(t, meta.Data["conversationId"]) != id || asUint(t, meta.Data["messageId"]) == 0 || meta.Data["status"] != "streaming" {
		t.Fatalf("unexpected meta frame: %#v", meta)
	}
	messageID := asUint(t, meta.Data["messageId"])
	for _, frame := range []sseFrame{firstDelta, secondDelta} {
		if frame.Event != "delta" || asUint(t, frame.Data["messageId"]) != messageID {
			t.Fatalf("unexpected delta frame: %#v", frame)
		}
	}
	if firstDelta.Data["content"] != "A" || secondDelta.Data["content"] != "B" {
		t.Fatalf("unexpected delta contents: %#v %#v", firstDelta, secondDelta)
	}
	if done.Event != "done" || asUint(t, done.Data["messageId"]) != messageID || done.Data["status"] != "completed" {
		t.Fatalf("unexpected done frame: %#v", done)
	}
	message, ok := done.Data["message"].(map[string]any)
	if !ok || asUint(t, message["id"]) != messageID || message["status"] != "completed" || message["content"] != "AB" {
		t.Fatalf("unexpected completed message: %#v", done.Data["message"])
	}
}

func TestAdminAIStreamErrorFrameIncludesMessageStatusAndCode(t *testing.T) {
	engine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, streamErr: errors.New("upstream unavailable")})
	token := loginAndGetToken(t, engine)
	id := decodeConversationID(t, performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, token))

	streamed := performJSONRequest(engine, http.MethodPost, fmt.Sprintf("/api/admin/ai/conversations/%d/messages/stream", id), map[string]any{"content": "hi"}, token)
	frames := parseSSEFrames(t, streamed.Body.String())
	if len(frames) != 2 || frames[0].Event != "meta" || frames[1].Event != "error" {
		t.Fatalf("expected meta and error frames, got %#v", frames)
	}
	errorFrame := frames[1]
	if asUint(t, errorFrame.Data["messageId"]) == 0 || errorFrame.Data["status"] != "failed" || asUint(t, errorFrame.Data["code"]) != 502 {
		t.Fatalf("error frame omits frontend recovery fields: %#v", errorFrame)
	}
	if message, ok := errorFrame.Data["message"].(string); !ok || strings.TrimSpace(message) == "" {
		t.Fatalf("error frame omits user-facing message: %#v", errorFrame)
	}
}

func TestAdminAIConversationEndpointsHideUnknownAndOtherAdminData(t *testing.T) {
	engine, db := newAdminAITestEngineWithDatabase(t, &fakeAdminAIClient{configured: true, deltas: []string{"answer"}})
	ownerToken := loginAndGetToken(t, engine)
	id := decodeConversationID(t, performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations", map[string]any{}, ownerToken))

	unknown := performJSONRequest(engine, http.MethodPost, "/api/admin/ai/conversations/999999/messages/stream", map[string]any{"content": "hi"}, ownerToken)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown conversation must be hidden as not found, got %d %s", unknown.Code, unknown.Body.String())
	}

	other := model.User{Username: fmt.Sprintf("router-ai-other-%s", t.Name()), Email: fmt.Sprintf("router-ai-other-%s@example.test", t.Name()), Role: model.UserRoleAdmin, PasswordHash: "not-used"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create other admin: %v", err)
	}
	otherToken, err := auth.GenerateTokenWithRole("admin-test-secret", other.ID, other.Username, other.Role, time.Now())
	if err != nil {
		t.Fatalf("create other token: %v", err)
	}
	foreignGet := performRequestWithToken(engine, http.MethodGet, fmt.Sprintf("/api/admin/ai/conversations/%d", id), otherToken)
	if foreignGet.Code != http.StatusNotFound {
		t.Fatalf("other admin must not read conversation, got %d %s", foreignGet.Code, foreignGet.Body.String())
	}
	foreignStream := performJSONRequest(engine, http.MethodPost, fmt.Sprintf("/api/admin/ai/conversations/%d/messages/stream", id), map[string]any{"content": "hi"}, otherToken)
	if foreignStream.Code != http.StatusNotFound {
		t.Fatalf("other admin must not stream conversation, got %d %s", foreignStream.Code, foreignStream.Body.String())
	}
}

func TestAdminAILegacyChatAndBeautifyRoutesRemainAvailable(t *testing.T) {
	chatEngine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, chatAnswer: "legacy answer"})
	chatToken := loginAndGetToken(t, chatEngine)
	chat := performJSONRequest(chatEngine, http.MethodPost, "/api/admin/ai/chat", map[string]any{"messages": []map[string]string{{"role": "user", "content": "hello"}}}, chatToken)
	if chat.Code != http.StatusOK {
		t.Fatalf("legacy chat route: %d %s", chat.Code, chat.Body.String())
	}

	beautifyEngine := newAdminAITestEngine(t, &fakeAdminAIClient{configured: true, chatAnswer: `{"title":"title","summary":"summary","content":"content"}`})
	beautifyToken := loginAndGetToken(t, beautifyEngine)
	beautify := performJSONRequest(beautifyEngine, http.MethodPost, "/api/admin/ai/beautify", map[string]any{"title": "old", "summary": "old", "content": "body"}, beautifyToken)
	if beautify.Code != http.StatusOK {
		t.Fatalf("beautify compatibility route: %d %s", beautify.Code, beautify.Body.String())
	}
}

func performRequestWithToken(handler http.Handler, method, target, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func parseSSEFrames(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	for _, raw := range strings.Split(strings.TrimSpace(body), "\n\n") {
		var event string
		var data json.RawMessage
		for _, line := range strings.Split(raw, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = json.RawMessage(strings.TrimPrefix(line, "data: "))
			}
		}
		if event == "" || len(data) == 0 {
			t.Fatalf("invalid SSE frame %q in body %q", raw, body)
		}
		payload := map[string]any{}
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode SSE data %q: %v", data, err)
		}
		frames = append(frames, sseFrame{Event: event, Data: payload})
	}
	return frames
}

func asUint(t *testing.T, value any) uint {
	t.Helper()
	number, ok := value.(float64)
	if !ok || number <= 0 || number != float64(uint(number)) {
		t.Fatalf("expected positive integral number, got %#v", value)
	}
	return uint(number)
}
