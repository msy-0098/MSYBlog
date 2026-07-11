package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewClient(cfg Config) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "deepseek-chat"
	}
	return &Client{
		apiKey:     strings.TrimSpace(cfg.APIKey),
		model:      model,
		baseURL:    strings.TrimRight(NormalizeBaseURL(cfg.BaseURL), "/"),
		httpClient: httpClient,
	}
}

func (c *Client) Configured() bool {
	return c != nil && c.apiKey != "" && c.baseURL != ""
}

func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if !c.Configured() {
		return "", errors.New("deepseek is not configured")
	}
	if len(messages) == 0 {
		return "", errors.New("at least one message is required")
	}

	payload, err := json.Marshal(chatRequest{Model: c.model, Messages: messages, Stream: false})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", err
	}
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("decode deepseek response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := response.Status
		if decoded.Error != nil && decoded.Error.Message != "" {
			message = decoded.Error.Message
		}
		return "", fmt.Errorf("deepseek request failed: %s", message)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", errors.New("deepseek returned an empty answer")
	}
	return strings.TrimSpace(decoded.Choices[0].Message.Content), nil
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
