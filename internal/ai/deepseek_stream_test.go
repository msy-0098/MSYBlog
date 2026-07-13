package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClientStreamChatPostsStreamingDeltasUntilDone(t *testing.T) {
	var request chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", Model: "deepseek-chat", BaseURL: server.URL, HTTPClient: server.Client()})
	var got []string
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		got = append(got, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("stream chat returned error: %v", err)
	}
	if !request.Stream {
		t.Fatal("expected request stream to be true")
	}
	if request.Model != "deepseek-chat" || !reflect.DeepEqual(request.Messages, []Message{{Role: "user", Content: "hi"}}) {
		t.Fatalf("unexpected request: %#v", request)
	}
	if !reflect.DeepEqual(got, []string{"hello", " world"}) {
		t.Fatalf("unexpected streamed deltas: %#v", got)
	}
}

func TestClientStreamChatHandlesMultipleSSELinesAndKeepalives(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("\n: keepalive\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"one\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: \n\ndata: {\"choices\":[{\"delta\":{\"content\":\" two\"}}]}\n\ndata: [DONE]\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	var got []string
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		got = append(got, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("stream chat returned error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"one", " two"}) {
		t.Fatalf("unexpected streamed deltas: %#v", got)
	}
}

func TestClientStreamChatReturnsNon2xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) error {
		t.Fatal("callback must not be called for a non-2xx response")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
}

func TestClientStreamChatReturnsMalformedChunkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: not-json\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) error {
		t.Fatal("callback must not be called for a malformed chunk")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "decode deepseek stream chunk") {
		t.Fatalf("expected malformed-chunk error, got %v", err)
	}
}

func TestClientStreamChatReturnsCallbackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
	}))
	defer server.Close()

	callbackErr := errors.New("stop streaming")
	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("expected callback error, got %v", err)
	}
}

func TestClientStreamChatStopsWhenRequestContextIsCanceled(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	errCh := make(chan error, 1)
	go func() {
		errCh <- client.StreamChat(ctx, []Message{{Role: "user", Content: "hi"}}, func(string) error { return nil })
	}()

	select {
	case <-requestStarted:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("stream request did not start")
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream chat did not stop after context cancellation")
	}

	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("server request context was not canceled")
	}
}
