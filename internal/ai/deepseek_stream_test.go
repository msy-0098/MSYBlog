package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestClientStreamChatCallsCallbackForAllChoiceDeltasInOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"甲\"}},{\"delta\":{\"content\":\"乙\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	var got strings.Builder
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		got.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("stream chat returned error: %v", err)
	}
	if got.String() != "甲乙" {
		t.Fatalf("unexpected streamed deltas %q", got.String())
	}
}

func TestClientStreamChatIgnoresHTTPClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	httpClient := server.Client()
	httpClient.Timeout = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: httpClient})
	var got strings.Builder
	err := client.StreamChat(ctx, []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		got.WriteString(delta)
		return nil
	})
	if err != nil {
		t.Fatalf("stream chat returned error: %v", err)
	}
	if got.String() != "hello" {
		t.Fatalf("unexpected streamed delta %q", got.String())
	}
}

func TestClientStreamChatRejectsEOFBeforeDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	var got strings.Builder
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		got.WriteString(delta)
		return nil
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("expected unexpected EOF error, got %v", err)
	}
	if got.String() != "hello" {
		t.Fatalf("unexpected streamed delta %q", got.String())
	}
}

func TestClientStreamChatReturnsScannerErrorForOversizedDataLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + strings.Repeat("a", (2<<20)+1) + "\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(string) error {
		t.Fatal("callback must not be called for an oversized data line")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("expected scanner token-too-long error, got %v", err)
	}
}

func TestStreamContextLimitsBackgroundContextToFiveMinutes(t *testing.T) {
	before := time.Now()
	ctx, cancel := streamContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected stream context deadline")
	}
	if deadline.Before(before.Add(4*time.Minute+59*time.Second)) || deadline.After(before.Add(5*time.Minute+time.Second)) {
		t.Fatalf("unexpected stream context deadline %v", deadline)
	}
}

func TestStreamContextDoesNotExtendShorterCallerDeadline(t *testing.T) {
	callerDeadline := time.Now().Add(time.Minute)
	callerCtx, callerCancel := context.WithDeadline(context.Background(), callerDeadline)
	defer callerCancel()
	ctx, cancel := streamContext(callerCtx)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected stream context deadline")
	}
	if deadline.After(callerDeadline) {
		t.Fatalf("stream deadline %v extended caller deadline %v", deadline, callerDeadline)
	}
}

func TestClientStreamChatDeliversFirstDeltaBeforeServerCompletes(t *testing.T) {
	firstDeltaDelivered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n"))
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("response writer does not support flushing")
			return
		}
		flusher.Flush()

		select {
		case <-firstDeltaDelivered:
		case <-time.After(time.Second):
			t.Error("first delta callback did not run before server completion")
			return
		}

		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"second\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: server.Client()})
	var got strings.Builder
	err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, func(delta string) error {
		got.WriteString(delta)
		if delta == "first" {
			close(firstDeltaDelivered)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("stream chat returned error: %v", err)
	}
	if got.String() != "firstsecond" {
		t.Fatalf("unexpected streamed deltas %q", got.String())
	}
}
