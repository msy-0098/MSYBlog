package ai

import (
	"bufio"
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

type ChatClient interface {
	Configured() bool
	Chat(context.Context, []Message) (string, error)
	StreamChat(context.Context, []Message, func(string) error) error
}

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

var _ ChatClient = (*Client)(nil)

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

type streamChatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
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

func (c *Client) StreamChat(ctx context.Context, messages []Message, callback func(string) error) error {
	if !c.Configured() {
		return errors.New("deepseek is not configured")
	}
	if len(messages) == 0 {
		return errors.New("at least one message is required")
	}
	if callback == nil {
		return errors.New("stream callback is required")
	}

	payload, err := json.Marshal(chatRequest{Model: c.model, Messages: messages, Stream: true})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return streamRequestError(response)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 1024), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			return nil
		}

		var chunk streamChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decode deepseek stream chunk: %w", err)
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return fmt.Errorf("deepseek stream failed: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if err := callback(chunk.Choices[0].Delta.Content); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func streamRequestError(response *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	message := response.Status
	var decoded chatResponse
	if json.Unmarshal(body, &decoded) == nil && decoded.Error != nil && decoded.Error.Message != "" {
		message = decoded.Error.Message
	}
	return fmt.Errorf("deepseek request failed: %s", message)
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
