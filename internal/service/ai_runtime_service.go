package service

import (
	"context"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"masenyu.top/blog/backend/internal/ai"
)

var (
	ErrAIInputTooLong          = errors.New("AI input exceeds the allowed length")
	ErrAIContextTooLong        = errors.New("AI context exceeds the allowed length")
	ErrAIRateLimit             = errors.New("AI request rate limit exceeded")
	ErrAIConcurrentLimit       = errors.New("AI concurrent request limit exceeded")
	ErrAIGlobalConcurrentLimit = errors.New("AI global concurrent request limit exceeded")
)

type AILimits struct {
	MaxInputChars         int
	MaxContextChars       int
	MaxRequestsPerMinute  int
	MaxConcurrentPerAdmin int
	MaxConcurrentGlobal   int
	Timeout               time.Duration
}

func DefaultAILimits() AILimits {
	return AILimits{
		MaxInputChars:         12000,
		MaxContextChars:       40000,
		MaxRequestsPerMinute:  30,
		MaxConcurrentPerAdmin: 1,
		MaxConcurrentGlobal:   4,
		Timeout:               45 * time.Second,
	}
}

type AIRuntime struct {
	provider ai.Provider
	limits   AILimits
	now      func() time.Time

	mu            sync.Mutex
	activeByAdmin map[uint]int
	active        int
	requestTimes  map[uint][]time.Time
}

func NewAIRuntime(provider ai.Provider, limits AILimits) *AIRuntime {
	defaults := DefaultAILimits()
	if limits.MaxInputChars <= 0 {
		limits.MaxInputChars = defaults.MaxInputChars
	}
	if limits.MaxContextChars <= 0 {
		limits.MaxContextChars = defaults.MaxContextChars
	}
	if limits.MaxRequestsPerMinute <= 0 {
		limits.MaxRequestsPerMinute = defaults.MaxRequestsPerMinute
	}
	if limits.MaxConcurrentPerAdmin <= 0 {
		limits.MaxConcurrentPerAdmin = defaults.MaxConcurrentPerAdmin
	}
	if limits.MaxConcurrentGlobal <= 0 {
		limits.MaxConcurrentGlobal = defaults.MaxConcurrentGlobal
	}
	if limits.Timeout <= 0 {
		limits.Timeout = defaults.Timeout
	}
	return &AIRuntime{
		provider:      provider,
		limits:        limits,
		now:           time.Now,
		activeByAdmin: make(map[uint]int),
		requestTimes:  make(map[uint][]time.Time),
	}
}

func (r *AIRuntime) Configured() bool {
	if r == nil || r.provider == nil {
		return false
	}
	if configured, ok := r.provider.(interface{ Configured() bool }); ok {
		return configured.Configured()
	}
	return true
}

func (r *AIRuntime) Chat(ctx context.Context, adminID uint, request ai.ChatRequest) (ai.ChatResult, error) {
	if err := r.validate(request); err != nil {
		return ai.ChatResult{}, err
	}
	release, err := r.acquire(ctx, adminID)
	if err != nil {
		return ai.ChatResult{}, err
	}
	defer release()

	callCtx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()
	result, err := r.provider.Chat(callCtx, request)
	if err == nil && callCtx.Err() != nil {
		return ai.ChatResult{}, callCtx.Err()
	}
	return result, err
}

func (r *AIRuntime) Stream(ctx context.Context, adminID uint, request ai.ChatRequest, emit func(ai.StreamChunk) error) (ai.Usage, error) {
	if emit == nil {
		return ai.Usage{}, errors.New("AI stream callback is required")
	}
	if err := r.validate(request); err != nil {
		return ai.Usage{}, err
	}
	release, err := r.acquire(ctx, adminID)
	if err != nil {
		return ai.Usage{}, err
	}
	defer release()

	callCtx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()
	usage, err := r.provider.Stream(callCtx, request, emit)
	if err == nil && callCtx.Err() != nil {
		return usage, callCtx.Err()
	}
	return usage, err
}

func (r *AIRuntime) Health(ctx context.Context) ai.HealthResult {
	if r == nil || r.provider == nil {
		return ai.HealthResult{ErrorKind: ai.ProviderErrorConfig}
	}
	return r.provider.Health(ctx)
}

func (r *AIRuntime) ActiveForAdmin(adminID uint) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeByAdmin[adminID]
}

func (r *AIRuntime) Active() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *AIRuntime) validate(request ai.ChatRequest) error {
	if r == nil || r.provider == nil || !r.Configured() {
		return &ai.ProviderError{Kind: ai.ProviderErrorConfig}
	}
	if len(request.Messages) == 0 {
		return ErrAIInputTooLong
	}
	last := len(request.Messages) - 1
	if utf8.RuneCountInString(request.Messages[last].Content) > r.limits.MaxInputChars {
		return ErrAIInputTooLong
	}
	contextChars := 0
	for index, message := range request.Messages {
		if index != last {
			contextChars += utf8.RuneCountInString(message.Content)
		}
	}
	if contextChars > r.limits.MaxContextChars {
		return ErrAIContextTooLong
	}
	return nil
}

func (r *AIRuntime) acquire(ctx context.Context, adminID uint) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := r.now()
	cutoff := now.Add(-time.Minute)
	r.mu.Lock()
	defer r.mu.Unlock()

	recent := r.requestTimes[adminID][:0]
	for _, requestedAt := range r.requestTimes[adminID] {
		if requestedAt.After(cutoff) {
			recent = append(recent, requestedAt)
		}
	}
	if len(recent) >= r.limits.MaxRequestsPerMinute {
		r.requestTimes[adminID] = recent
		return nil, ErrAIRateLimit
	}
	if r.activeByAdmin[adminID] >= r.limits.MaxConcurrentPerAdmin {
		r.requestTimes[adminID] = recent
		return nil, ErrAIConcurrentLimit
	}
	if r.active >= r.limits.MaxConcurrentGlobal {
		r.requestTimes[adminID] = recent
		return nil, ErrAIGlobalConcurrentLimit
	}
	r.requestTimes[adminID] = append(recent, now)
	r.activeByAdmin[adminID]++
	r.active++

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.activeByAdmin[adminID]--
			if r.activeByAdmin[adminID] == 0 {
				delete(r.activeByAdmin, adminID)
			}
			r.active--
		})
	}, nil
}
