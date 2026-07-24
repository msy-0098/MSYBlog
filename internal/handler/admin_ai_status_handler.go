package handler

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

const aiHealthCheckTimeout = 8 * time.Second

type AIHealthDTO struct {
	CheckedAt string               `json:"checkedAt"`
	Healthy   bool                 `json:"healthy"`
	ErrorKind ai.ProviderErrorKind `json:"errorKind,omitempty"`
}

type AIStatusDTO struct {
	Enabled        bool         `json:"enabled"`
	Provider       string       `json:"provider"`
	Model          string       `json:"model"`
	Configured     bool         `json:"configured"`
	BaseURLSummary string       `json:"baseURLSummary"`
	LastHealth     *AIHealthDTO `json:"lastHealth"`
}

type AdminAIStatusHandler struct {
	runtime        *service.AIRuntime
	provider       string
	model          string
	baseURLSummary string

	mu         sync.RWMutex
	lastHealth *AIHealthDTO
}

func NewAdminAIStatusHandler(runtime *service.AIRuntime, provider, model, baseURL string) *AdminAIStatusHandler {
	return &AdminAIStatusHandler{
		runtime:        runtime,
		provider:       strings.TrimSpace(provider),
		model:          strings.TrimSpace(model),
		baseURLSummary: summarizeAIBaseURL(baseURL),
	}
}

func (h *AdminAIStatusHandler) Status(c *gin.Context) {
	response.Success(c, h.status())
}

func (h *AdminAIStatusHandler) HealthCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), aiHealthCheckTimeout)
	defer cancel()
	result := ai.HealthResult{ErrorKind: ai.ProviderErrorConfig}
	if h.runtime != nil {
		result = h.runtime.Health(ctx)
	}
	checked := &AIHealthDTO{CheckedAt: time.Now().UTC().Format(time.RFC3339), Healthy: result.Healthy, ErrorKind: result.ErrorKind}
	h.mu.Lock()
	h.lastHealth = checked
	h.mu.Unlock()
	response.Success(c, checked)
}

func (h *AdminAIStatusHandler) status() AIStatusDTO {
	var last *AIHealthDTO
	h.mu.RLock()
	if h.lastHealth != nil {
		copy := *h.lastHealth
		last = &copy
	}
	h.mu.RUnlock()
	configured := h.runtime != nil && h.runtime.Configured()
	return AIStatusDTO{
		Enabled:        h.runtime != nil,
		Provider:       h.provider,
		Model:          h.model,
		Configured:     configured,
		BaseURLSummary: h.baseURLSummary,
		LastHealth:     last,
	}
}

func summarizeAIBaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + strings.TrimRight(parsed.EscapedPath(), "/")
}
