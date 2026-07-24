package ai

import (
	"context"
	"fmt"
	"strings"
)

type Provider interface {
	Chat(context.Context, ChatRequest) (ChatResult, error)
	Stream(context.Context, ChatRequest, func(StreamChunk) error) (Usage, error)
	Health(context.Context) HealthResult
}

type ChatRequest struct {
	Messages []Message
	Model    string
}

type ChatResult struct {
	Content string
	Usage   Usage
}

type StreamChunk struct {
	Content string
}

type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type HealthResult struct {
	Healthy   bool              `json:"healthy"`
	ErrorKind ProviderErrorKind `json:"errorKind,omitempty"`
}

func NewProvider(cfg Config) (Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "deepseek"
	}

	switch provider {
	case "deepseek":
		cfg.DisableThinking = true
		return NewOpenAICompatibleProvider(cfg)
	case "openai-compatible":
		return NewOpenAICompatibleProvider(cfg)
	default:
		return nil, &ProviderError{Kind: ProviderErrorConfig, cause: fmt.Errorf("unsupported provider")}
	}
}

func NewConfiguredClient(cfg Config) (ChatClient, error) {
	provider, err := NewProvider(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{provider: provider}, nil
}
