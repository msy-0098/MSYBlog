package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"masenyu.top/blog/backend/internal/ai"
)

type runtimeFakeProvider struct {
	chat   func(context.Context, ai.ChatRequest) (ai.ChatResult, error)
	stream func(context.Context, ai.ChatRequest, func(ai.StreamChunk) error) (ai.Usage, error)
}

func (p runtimeFakeProvider) Chat(ctx context.Context, request ai.ChatRequest) (ai.ChatResult, error) {
	if p.chat != nil {
		return p.chat(ctx, request)
	}
	return ai.ChatResult{Content: "ok"}, nil
}

func (p runtimeFakeProvider) Stream(ctx context.Context, request ai.ChatRequest, emit func(ai.StreamChunk) error) (ai.Usage, error) {
	if p.stream != nil {
		return p.stream(ctx, request, emit)
	}
	return ai.Usage{}, emit(ai.StreamChunk{Content: "ok"})
}

func (p runtimeFakeProvider) Health(_ context.Context) ai.HealthResult {
	return ai.HealthResult{Healthy: true}
}

func TestAIRuntimeRejectsSecondConcurrentStream(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runtime := NewAIRuntime(runtimeFakeProvider{stream: func(ctx context.Context, _ ai.ChatRequest, emit func(ai.StreamChunk) error) (ai.Usage, error) {
		close(started)
		select {
		case <-release:
			return ai.Usage{}, emit(ai.StreamChunk{Content: "ok"})
		case <-ctx.Done():
			return ai.Usage{}, ctx.Err()
		}
	}}, AILimits{MaxConcurrentPerAdmin: 1})

	done := make(chan error, 1)
	go func() {
		_, err := runtime.Stream(context.Background(), 7, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "first"}}}, func(ai.StreamChunk) error { return nil })
		done <- err
	}()
	<-started
	if runtime.ActiveForAdmin(7) != 1 {
		t.Fatalf("active streams = %d, want 1", runtime.ActiveForAdmin(7))
	}
	_, err := runtime.Stream(context.Background(), 7, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "second"}}}, func(ai.StreamChunk) error { return nil })
	if !errors.Is(err, ErrAIConcurrentLimit) {
		t.Fatalf("second concurrent stream error = %v, want ErrAIConcurrentLimit", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first stream: %v", err)
	}
}

func TestAIRuntimeRejectsOverlongInputAndContext(t *testing.T) {
	runtime := NewAIRuntime(runtimeFakeProvider{}, AILimits{MaxInputChars: 3, MaxContextChars: 4})
	_, err := runtime.Chat(context.Background(), 1, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "four"}}})
	if !errors.Is(err, ErrAIInputTooLong) {
		t.Fatalf("input error = %v, want ErrAIInputTooLong", err)
	}
	_, err = runtime.Chat(context.Background(), 1, ai.ChatRequest{Messages: []ai.Message{{Role: "system", Content: "abcde"}, {Role: "user", Content: "ok"}}})
	if !errors.Is(err, ErrAIContextTooLong) {
		t.Fatalf("context error = %v, want ErrAIContextTooLong", err)
	}
}

func TestAIRuntimeRateAndGlobalLimitsReleaseAfterCanceledStream(t *testing.T) {
	started := make(chan struct{})
	runtime := NewAIRuntime(runtimeFakeProvider{stream: func(ctx context.Context, _ ai.ChatRequest, _ func(ai.StreamChunk) error) (ai.Usage, error) {
		close(started)
		<-ctx.Done()
		return ai.Usage{}, ctx.Err()
	}}, AILimits{MaxRequestsPerMinute: 1, MaxConcurrentGlobal: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runtime.Stream(ctx, 1, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "first"}}}, func(ai.StreamChunk) error { return nil })
		done <- err
	}()
	<-started
	_, err := runtime.Chat(context.Background(), 2, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "other"}}})
	if !errors.Is(err, ErrAIGlobalConcurrentLimit) {
		t.Fatalf("global limit error = %v, want ErrAIGlobalConcurrentLimit", err)
	}
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("canceled stream did not return context cancellation")
	}
	if runtime.ActiveForAdmin(1) != 0 || runtime.Active() != 0 {
		t.Fatalf("canceled stream leaked active slots: admin=%d global=%d", runtime.ActiveForAdmin(1), runtime.Active())
	}
	if _, err := runtime.Chat(context.Background(), 2, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "other"}}}); err != nil {
		t.Fatalf("global slot not released after cancellation: %v", err)
	}
	if _, err := runtime.Chat(context.Background(), 2, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "again"}}}); !errors.Is(err, ErrAIRateLimit) {
		t.Fatalf("second request rate error = %v, want ErrAIRateLimit", err)
	}
}

func TestAIRuntimeRejectsCanceledRequestWithoutLeakingSlots(t *testing.T) {
	var calls int
	var mu sync.Mutex
	runtime := NewAIRuntime(runtimeFakeProvider{stream: func(_ context.Context, _ ai.ChatRequest, _ func(ai.StreamChunk) error) (ai.Usage, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return ai.Usage{}, nil
	}}, AILimits{MaxConcurrentGlobal: 1, Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.Stream(ctx, 1, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "hi"}}}, func(ai.StreamChunk) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request error = %v", err)
	}
	if runtime.Active() != 0 {
		t.Fatalf("canceled request leaked active slots: %d", runtime.Active())
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Fatalf("provider called for canceled request %d times", calls)
	}
}

func TestAIRuntimeChatRejectsLateSuccessAfterTimeoutOrCancellation(t *testing.T) {
	tests := []struct {
		name      string
		timeout   time.Duration
		cancelled bool
		want      error
	}{
		{name: "timeout", timeout: 10 * time.Millisecond, want: context.DeadlineExceeded},
		{name: "cancellation", timeout: time.Second, cancelled: true, want: context.Canceled},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			runtime := NewAIRuntime(runtimeFakeProvider{chat: func(_ context.Context, _ ai.ChatRequest) (ai.ChatResult, error) {
				close(started)
				<-release // Deliberately ignore the runtime context and return a late success.
				return ai.ChatResult{Content: "late success"}, nil
			}}, AILimits{MaxConcurrentGlobal: 1, Timeout: test.timeout})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := runtime.Chat(ctx, 1, ai.ChatRequest{Messages: []ai.Message{{Role: "user", Content: "hi"}}})
				done <- err
			}()
			<-started
			if test.cancelled {
				cancel()
			} else {
				time.Sleep(2 * test.timeout)
			}
			close(release)
			if err := <-done; !errors.Is(err, test.want) {
				t.Fatalf("late chat error = %v, want %v", err, test.want)
			}
			if runtime.Active() != 0 || runtime.ActiveForAdmin(1) != 0 {
				t.Fatalf("late chat leaked active slots: global=%d admin=%d", runtime.Active(), runtime.ActiveForAdmin(1))
			}
		})
	}
}
