package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientChatUsesDeepSeekChatCompletions(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		buf, _ := io.ReadAll(r.Body)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hello from deepseek"}}]}`))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", Model: "deepseek-chat", BaseURL: server.URL, HTTPClient: server.Client()})
	answer, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("chat returned error: %v", err)
	}
	if answer != "hello from deepseek" {
		t.Fatalf("unexpected answer %q", answer)
	}
	if gotAuth != "Bearer test-key" || gotPath != "/chat/completions" {
		t.Fatalf("unexpected request auth=%q path=%q", gotAuth, gotPath)
	}
	if !strings.Contains(gotBody, `"model":"deepseek-chat"`) || !strings.Contains(gotBody, `"content":"hi"`) {
		t.Fatalf("request body did not contain model/messages: %s", gotBody)
	}
}

func TestNormalizeBaseURLMapsDeepSeekPlatformHostToAPIHost(t *testing.T) {
	if got := NormalizeBaseURL("https://platform.deepseek.com"); got != "https://api.deepseek.com" {
		t.Fatalf("unexpected normalized URL %q", got)
	}
}
