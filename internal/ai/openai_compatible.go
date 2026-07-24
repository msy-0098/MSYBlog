package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const streamChatTimeout = 5 * time.Minute

type OpenAICompatibleProvider struct {
	apiKey          string
	model           string
	baseURL         string
	httpClient      *http.Client
	disableThinking bool
}

func NewOpenAICompatibleProvider(cfg Config) (*OpenAICompatibleProvider, error) {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "deepseek-chat"
	}
	return &OpenAICompatibleProvider{
		apiKey:          strings.TrimSpace(cfg.APIKey),
		model:           model,
		baseURL:         strings.TrimRight(NormalizeBaseURL(cfg.BaseURL), "/"),
		httpClient:      httpClient,
		disableThinking: cfg.DisableThinking,
	}, nil
}

func (p *OpenAICompatibleProvider) Configured() bool {
	return p != nil && p.apiKey != "" && p.baseURL != ""
}

func (p *OpenAICompatibleProvider) Chat(ctx context.Context, req ChatRequest) (ChatResult, error) {
	if err := p.validateRequest(req); err != nil {
		return ChatResult{}, err
	}

	payload, err := json.Marshal(p.request(req, false))
	if err != nil {
		return ChatResult{}, providerError(ProviderErrorUnavailable, err)
	}
	response, err := p.do(ctx, payload, false)
	if err != nil {
		return ChatResult{}, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return ChatResult{}, classifyTransportError(ctx, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ChatResult{}, classifyHTTPError(response.StatusCode, body)
	}
	var decoded chatResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ChatResult{}, providerError(ProviderErrorUnavailable, fmt.Errorf("decode chat response: %w", err))
	}
	if decoded.Error != nil || len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return ChatResult{}, providerError(ProviderErrorUnavailable, errors.New("empty provider response"))
	}
	return ChatResult{Content: strings.TrimSpace(decoded.Choices[0].Message.Content), Usage: decoded.Usage.toUsage()}, nil
}

func (p *OpenAICompatibleProvider) Stream(ctx context.Context, req ChatRequest, emit func(StreamChunk) error) (Usage, error) {
	if err := p.validateRequest(req); err != nil {
		return Usage{}, err
	}
	if emit == nil {
		return Usage{}, providerError(ProviderErrorConfig, errors.New("stream callback is required"))
	}

	streamCtx, cancel := streamContext(ctx)
	defer cancel()
	payload, err := json.Marshal(p.request(req, true))
	if err != nil {
		return Usage{}, providerError(ProviderErrorUnavailable, err)
	}
	response, err := p.do(streamCtx, payload, true)
	if err != nil {
		return Usage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if readErr != nil {
			return Usage{}, classifyTransportError(streamCtx, readErr)
		}
		return Usage{}, classifyHTTPError(response.StatusCode, body)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 1024), 2<<20)
	var usage Usage
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
			return usage, nil
		}

		var chunk streamChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return Usage{}, providerError(ProviderErrorUnavailable, fmt.Errorf("decode stream chunk: %w", err))
		}
		if chunk.Error != nil {
			return Usage{}, classifyUpstreamMessage(chunk.Error.Message)
		}
		usage = chunk.Usage.toUsage()
		for _, choice := range chunk.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			if err := emit(StreamChunk{Content: choice.Delta.Content}); err != nil {
				return Usage{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Usage{}, classifyTransportError(streamCtx, err)
	}
	return Usage{}, providerError(ProviderErrorUnavailable, io.ErrUnexpectedEOF)
}

func (p *OpenAICompatibleProvider) Health(ctx context.Context) HealthResult {
	if !p.Configured() {
		return HealthResult{ErrorKind: ProviderErrorConfig}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return HealthResult{ErrorKind: ProviderErrorConfig}
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	response, err := p.httpClient.Do(request)
	if err != nil {
		return HealthResult{ErrorKind: providerErrorKind(classifyTransportError(ctx, err))}
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return HealthResult{Healthy: true}
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if readErr != nil {
		return HealthResult{ErrorKind: providerErrorKind(classifyTransportError(ctx, readErr))}
	}
	return HealthResult{ErrorKind: providerErrorKind(classifyHTTPError(response.StatusCode, body))}
}

func (p *OpenAICompatibleProvider) validateRequest(req ChatRequest) error {
	if !p.Configured() {
		return providerError(ProviderErrorConfig, errors.New("provider is not configured"))
	}
	if len(req.Messages) == 0 {
		return providerError(ProviderErrorConfig, errors.New("at least one message is required"))
	}
	return nil
}

func (p *OpenAICompatibleProvider) request(req ChatRequest, stream bool) chatRequest {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.model
	}
	payload := chatRequest{Model: model, Messages: req.Messages, Stream: stream}
	if stream {
		payload.StreamOptions = &streamOptions{IncludeUsage: true}
		if p.disableThinking {
			payload.Thinking = &thinkingConfig{Type: "disabled"}
		}
	}
	return payload
}

func (p *OpenAICompatibleProvider) do(ctx context.Context, payload []byte, streaming bool) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, providerError(ProviderErrorConfig, err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")
	client := p.httpClient
	if streaming {
		streamClient := *p.httpClient
		streamClient.Timeout = 0
		client = &streamClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, classifyTransportError(ctx, err)
	}
	return response, nil
}

func streamContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, streamChatTimeout)
}

func classifyHTTPError(status int, body []byte) error {
	if status == http.StatusTooManyRequests {
		return providerError(ProviderErrorRateLimit, errors.New("upstream rate limit"))
	}
	if status >= http.StatusInternalServerError {
		return providerError(ProviderErrorUnavailable, fmt.Errorf("upstream status %d", status))
	}
	return providerError(ProviderErrorConfig, errors.New("upstream request rejected"))
}

func classifyUpstreamMessage(message string) error {
	message = strings.ToLower(message)
	if strings.Contains(message, "rate") || strings.Contains(message, "quota") || strings.Contains(message, "too many") {
		return providerError(ProviderErrorRateLimit, errors.New("upstream rate limit"))
	}
	return providerError(ProviderErrorUnavailable, errors.New("upstream stream error"))
}

func classifyTransportError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return providerError(ProviderErrorTimeout, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return providerError(ProviderErrorTimeout, err)
	}
	return providerError(ProviderErrorUnavailable, err)
}
