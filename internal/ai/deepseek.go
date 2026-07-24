package ai

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

type Config struct {
	Provider        string
	APIKey          string
	Model           string
	BaseURL         string
	HTTPClient      *http.Client
	DisableThinking bool
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatClient is the legacy application-facing contract. New integrations should use Provider.
type ChatClient interface {
	Configured() bool
	Chat(context.Context, []Message) (string, error)
	StreamChat(context.Context, []Message, func(string) error) error
}

type Client struct {
	provider Provider
}

var _ ChatClient = (*Client)(nil)

type thinkingConfig struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatRequest and streamChatChunk model the shared OpenAI-compatible protocol.
type chatRequest struct {
	Model         string          `json:"model"`
	Messages      []Message       `json:"messages"`
	Stream        bool            `json:"stream"`
	Thinking      *thinkingConfig `json:"thinking,omitempty"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
}

type completionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (u completionUsage) toUsage() Usage {
	return Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, TotalTokens: u.TotalTokens}
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage completionUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamChatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage completionUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewClient retains the historical DeepSeek-specific construction behavior.
func NewClient(cfg Config) *Client {
	cfg.Provider = "deepseek"
	provider, err := NewProvider(cfg)
	if err != nil {
		return &Client{}
	}
	return &Client{provider: provider}
}

func (c *Client) Configured() bool {
	if c == nil || c.provider == nil {
		return false
	}
	if configured, ok := c.provider.(interface{ Configured() bool }); ok {
		return configured.Configured()
	}
	return true
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if c == nil || c.provider == nil {
		return "", providerError(ProviderErrorConfig, nil)
	}
	result, err := c.provider.Chat(ctx, ChatRequest{Messages: messages})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c *Client) StreamChat(ctx context.Context, messages []Message, callback func(string) error) error {
	if c == nil || c.provider == nil {
		return providerError(ProviderErrorConfig, nil)
	}
	if callback == nil {
		return providerError(ProviderErrorConfig, nil)
	}
	_, err := c.provider.Stream(ctx, ChatRequest{Messages: messages}, func(chunk StreamChunk) error {
		return callback(chunk.Content)
	})
	return err
}

func NormalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "https://api.deepseek.com"
	}
	parsed, err := url.Parse(raw)
	if err == nil && strings.EqualFold(parsed.Hostname(), "platform.deepseek.com") {
		parsed.Scheme = "https"
		parsed.Host = "api.deepseek.com"
		parsed.Path = strings.TrimRight(parsed.Path, "/")
		return strings.TrimRight(parsed.String(), "/")
	}
	return strings.TrimRight(raw, "/")
}
