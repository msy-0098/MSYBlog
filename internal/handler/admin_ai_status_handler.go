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

// Status 获取 AI 服务当前配置与运行状态
// @Summary 获取 AI 服务状态
// @Description 查询后台配置的 AI Provider、模型名与最近健康检查结果
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=AIStatusDTO} "AI 服务状态"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ai/status [get]
func (h *AdminAIStatusHandler) Status(c *gin.Context) {
	response.Success(c, h.status())
}

// HealthCheck 执行 AI 服务健康检查
// @Summary 执行 AI 服务健康检查
// @Description 向 AI 上游服务发起心跳探测，验证 API Key 与连通性
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=AIHealthDTO} "健康检查结果"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ai/health-check [post]
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
