package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeProvider struct{}

func (fakeProvider) Chat(context.Context, ChatRequest) (ChatResult, error) {
	return ChatResult{Content: "ok"}, nil
}

func (fakeProvider) Stream(_ context.Context, _ ChatRequest, emit func(StreamChunk) error) (Usage, error) {
	return Usage{}, emit(StreamChunk{Content: "ok"})
}

func (fakeProvider) Health(context.Context) HealthResult {
	return HealthResult{Healthy: true}
}

func TestProviderContract(t *testing.T) {
	var _ Provider = fakeProvider{}
}

func TestOpenAICompatibleProviderChatReturnsUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q", got)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"test-model"`) {
			t.Errorf("unexpected request body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(Config{APIKey: "test-key", Model: "test-model", BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	result, err := provider.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if result.Content != "answer" || result.Usage != (Usage{InputTokens: 3, OutputTokens: 5, TotalTokens: 8}) {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestOpenAICompatibleProviderClassifiesHTTPFailuresWithoutLeakingDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited for secret account"}}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(Config{APIKey: "secret-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	_, err = provider.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ProviderErrorRateLimit {
		t.Fatalf("expected rate-limit provider error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("provider error leaked upstream details: %q", err)
	}
}

func TestOpenAICompatibleProviderClassifiesUnavailableAndTimeoutFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream internal detail"}}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	_, err = provider.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ProviderErrorUnavailable {
		t.Fatalf("expected unavailable provider error, got %v", err)
	}

	timeoutClient := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	provider, err = NewOpenAICompatibleProvider(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: timeoutClient})
	if err != nil {
		t.Fatalf("new timeout provider: %v", err)
	}
	_, err = provider.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if !errors.As(err, &providerErr) || providerErr.Kind != ProviderErrorTimeout {
		t.Fatalf("expected timeout provider error, got %v", err)
	}
}

func TestOpenAICompatibleProviderStreamReturnsUsageAndPropagatesEmitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	var content strings.Builder
	usage, err := provider.Stream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, func(chunk StreamChunk) error {
		content.WriteString(chunk.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if content.String() != "hello" || usage != (Usage{InputTokens: 2, OutputTokens: 1, TotalTokens: 3}) {
		t.Fatalf("unexpected stream result content=%q usage=%#v", content.String(), usage)
	}

	emitErr := errors.New("stop emitting")
	_, err = provider.Stream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}, func(StreamChunk) error {
		return emitErr
	})
	if !errors.Is(err, emitErr) {
		t.Fatalf("expected emit error, got %v", err)
	}
}

func TestOpenAICompatibleProviderHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(Config{APIKey: "test-key", BaseURL: server.URL + "/v1", HTTPClient: server.Client()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if health := provider.Health(context.Background()); !health.Healthy {
		t.Fatalf("expected healthy result, got %#v", health)
	}
}

func TestOpenAICompatibleProviderHealthReportsConfigurationFailure(t *testing.T) {
	provider, err := NewOpenAICompatibleProvider(Config{})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	health := provider.Health(context.Background())
	if health.Healthy || health.ErrorKind != ProviderErrorConfig {
		t.Fatalf("unexpected health result: %#v", health)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
