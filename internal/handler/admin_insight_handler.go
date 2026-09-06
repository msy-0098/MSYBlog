package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/ai"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

type AdminInsightHandler struct {
	db            *gorm.DB
	aiClient      ai.ChatClient
	runtime       *service.AIRuntime
	model         string
	notifications *service.NotificationService
	ipLocations   *service.IPLocationResolver
}

type AdminVisitorDTO struct {
	ID        uint   `json:"id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Nickname  string `json:"nickname"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
}

type TopIPDTO struct {
	IP       string `json:"ip"`
	Requests int64  `json:"requests"`
	Failures int64  `json:"failures"`
	LastSeen string `json:"lastSeen"`
	Banned   bool   `json:"banned"`
}

type TopPathDTO struct {
	Path     string `json:"path"`
	Requests int64  `json:"requests"`
}

type AnalyticsDTO struct {
	TotalRequests  int64        `json:"totalRequests"`
	TodayRequests  int64        `json:"todayRequests"`
	UniqueIPs      int64        `json:"uniqueIPs"`
	FailedRequests int64        `json:"failedRequests"`
	TopIPs         []TopIPDTO   `json:"topIPs"`
	TopPaths       []TopPathDTO `json:"topPaths"`
	RecentBans     []IPBanDTO   `json:"recentBans"`
}

type IPBanDTO struct {
	ID        uint    `json:"id"`
	IP        string  `json:"ip"`
	Reason    string  `json:"reason"`
	Active    bool    `json:"active"`
	ExpiresAt *string `json:"expiresAt"`
	CreatedAt string  `json:"createdAt"`
}

type BanRequest struct {
	IP       string `json:"ip"`
	Reason   string `json:"reason"`
	Duration int    `json:"duration"`
}

type IPLocationRequest struct {
	IP string `json:"ip"`
}

type AIChatRequest struct {
	Messages []ai.Message `json:"messages"`
}

type AIChatResponse struct {
	Answer string `json:"answer"`
	Mode   string `json:"mode"`
	Model  string `json:"model"`
}

type BeautifyRequest struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Content string `json:"content"`
}

type BeautifyResponse struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Content string `json:"content"`
}

func NewAdminInsightHandler(db *gorm.DB, aiClient ai.ChatClient, modelName string) AdminInsightHandler {
	return AdminInsightHandler{db: db, aiClient: aiClient, model: modelName, ipLocations: service.NewIPLocationResolver(nil)}
}

func NewAdminInsightHandlerWithRuntime(db *gorm.DB, aiClient ai.ChatClient, runtime *service.AIRuntime, modelName string) AdminInsightHandler {
	return AdminInsightHandler{db: db, aiClient: aiClient, runtime: runtime, model: modelName, ipLocations: service.NewIPLocationResolver(nil)}
}

func (h AdminInsightHandler) WithNotifications(notifications *service.NotificationService) AdminInsightHandler {
	h.notifications = notifications
	return h
}

// ListUsers 管理端用户列表
// @Summary 管理端用户列表
// @Description 分页查询已注册用户/访客账号
// @Tags 仪表盘与统计分析
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(10)
// @Success 200 {object} response.Envelope{data=PageDTO[AdminVisitorDTO]} "用户列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/users [get]
func (h AdminInsightHandler) ListUsers(c *gin.Context) {
	page, pageSize := pagination(c)
	var total int64
	if err := h.db.Model(&model.User{}).Count(&total).Error; err != nil {
		internalError(c)
		return
	}
	var users []model.User
	if err := h.db.Order("created_at desc").Limit(pageSize).Offset((page - 1) * pageSize).Find(&users).Error; err != nil {
		internalError(c)
		return
	}
	list := make([]AdminVisitorDTO, 0, len(users))
	for _, user := range users {
		list = append(list, adminVisitorDTO(user))
	}
	response.Success(c, PageDTO[AdminVisitorDTO]{List: list, Page: page, PageSize: pageSize, Total: total})
}

// LookupIPLocation 查询 IP 地理位置
// @Summary 查询 IP 地理位置
// @Description 查询指定 IP 的归属城市/运营商信息（带配额与缓存）
// @Tags 仪表盘与统计分析
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body IPLocationRequest true "IP 查询请求"
// @Success 200 {object} response.Envelope{data=object} "IP 地理位置"
// @Failure 400 {object} response.ErrorResponse "IP 格式无效"
// @Failure 429 {object} response.ErrorResponse "查询额度超限"
// @Router /admin/ip-locations [post]
func (h AdminInsightHandler) LookupIPLocation(c *gin.Context) {
	var req IPLocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	location, err := h.ipLocations.Lookup(c.Request.Context(), req.IP)
	switch {
	case err == nil:
		response.Success(c, location)
	case errors.Is(err, service.ErrInvalidIPAddress):
		response.Error(c, http.StatusBadRequest, 400, "IP 地址格式无效")
	case errors.Is(err, service.ErrIPLocationQuotaReached):
		response.Error(c, http.StatusTooManyRequests, 429, "今日查询额度已用完，为保证后台稳定运行，所属地服务将在额度恢复后自动可用。")
	default:
		response.Error(c, http.StatusServiceUnavailable, 503, "所属地服务暂时不可用，请稍后重试")
	}
}

// GetAnalytics 获取站点访问统计分析
// @Summary 获取站点访问统计分析
// @Description 查询访问日志聚合数据，包括请求量、独立 IP、异常请求与高频 IP/路径
// @Tags 仪表盘与统计分析
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=AnalyticsDTO} "统计分析数据"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/analytics [get]
func (h AdminInsightHandler) GetAnalytics(c *gin.Context) {
	analytics, err := h.readAnalytics()
	if err != nil {
		internalError(c)
		return
	}
	response.Success(c, analytics)
}

type AIAnalysisDTO struct {
	Mode    string   `json:"mode"`
	Summary string   `json:"summary"`
	Signals []string `json:"signals"`
}

// GenerateInsights 生成 AI 站点运营洞察
// @Summary 生成 AI 站点运营洞察
// @Description 结合访问统计与博客现状，调用 AI 生成运营分析建议
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=AIAnalysisDTO} "AI 分析结果"
// @Failure 502 {object} response.ErrorResponse "大模型响应错误"
// @Failure 503 {object} response.ErrorResponse "AI 未配置"
// @Router /admin/ai/insights/generate [post]
func (h AdminInsightHandler) GenerateInsights(c *gin.Context) {
	if !h.configured() {
		response.Error(c, http.StatusServiceUnavailable, 503, "DeepSeek 尚未配置，请在服务器环境变量中设置 BLOG_AI_API_KEY")
		return
	}
	stats, err := (AdminDashboardHandler{db: h.db}).readStats()
	if err != nil {
		internalError(c)
		return
	}
	analytics, err := h.readAnalytics()
	if err != nil {
		internalError(c)
		return
	}
	payload, err := json.Marshal(struct {
		Stats     DashboardStatsDTO `json:"stats"`
		Analytics AnalyticsDTO      `json:"analytics"`
	}{Stats: stats, Analytics: analytics})
	if err != nil {
		internalError(c)
		return
	}
	answer, err := h.chat(c, []ai.Message{
		{Role: "system", Content: "你是博客运营分析助手，请用简洁中文给出可执行建议。"},
		{Role: "user", Content: "请分析以下博客数据并给出一段不超过120字的运营建议：" + string(payload)},
	})
	if err != nil {
		response.Error(c, http.StatusBadGateway, 502, aiErrorMessage(err))
		return
	}
	response.Success(c, AIAnalysisDTO{Mode: "deepseek", Summary: answer, Signals: []string{
		fmt.Sprintf("累计阅读 %d 次", stats.TotalViews),
		fmt.Sprintf("今日请求 %d 次，独立 IP %d 个", analytics.TodayRequests, analytics.UniqueIPs),
		fmt.Sprintf("注册访客 %d 位，评论 %d 条", stats.VisitorCount, stats.CommentCount),
	}})
}
// ListBans 获取 IP 封禁黑名单
// @Summary 获取 IP 封禁黑名单
// @Description 查询全部被封禁的 IP 地址记录及生效状态
// @Tags 仪表盘与统计分析
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=ListDTO[IPBanDTO]} "黑名单列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ip-bans [get]
func (h AdminInsightHandler) ListBans(c *gin.Context) {
	var bans []model.IPBan
	if err := h.db.Order("created_at desc").Find(&bans).Error; err != nil {
		internalError(c)
		return
	}
	list := make([]IPBanDTO, 0, len(bans))
	for _, ban := range bans {
		list = append(list, ipBanDTO(ban))
	}
	response.Success(c, ListDTO[IPBanDTO]{List: list})
}

// CreateBan 新增 IP 封禁
// @Summary 新增 IP 封禁
// @Description 手动添加或延长指定 IP 的访问封禁时间
// @Tags 仪表盘与统计分析
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body BanRequest true "封禁请求"
// @Success 200 {object} response.Envelope{data=IPBanDTO} "封禁成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ip-bans [post]
func (h AdminInsightHandler) CreateBan(c *gin.Context) {
	var req BanRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.IP) == "" {
		badRequest(c)
		return
	}
	duration := req.Duration
	if duration <= 0 || duration > 24*365 {
		duration = 24
	}
	expires := time.Now().Add(time.Duration(duration) * time.Hour)
	ip := strings.TrimSpace(req.IP)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "管理员手动封禁"
	}

	var ban model.IPBan
	err := h.db.Where("ip = ?", ip).First(&ban).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		ban = model.IPBan{IP: ip, Reason: reason, Active: true, ExpiresAt: &expires}
		if err := h.db.Create(&ban).Error; err != nil {
			internalError(c)
			return
		}
	case err != nil:
		internalError(c)
		return
	default:
		ban.Reason = reason
		ban.Active = true
		ban.ExpiresAt = &expires
		if err := h.db.Save(&ban).Error; err != nil {
			internalError(c)
			return
		}
	}
	if h.notifications != nil {
		body := "IP " + ip + " 已封禁：" + reason
		go h.notifications.NotifyAdmins(context.Background(), service.NotifyInput{
			Kind:       model.NotificationKindSecurity,
			Title:      "IP 封禁",
			Body:       body,
			RefType:    "ip_ban",
			RefID:      ban.ID,
			ActionPath: "/admin/security",
		})
	}
	response.Success(c, ipBanDTO(ban))
}

// RemoveBan 解除 IP 封禁
// @Summary 解除 IP 封禁
// @Description 根据记录 ID 解除对指定 IP 的拦截封禁
// @Tags 仪表盘与统计分析
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "封禁记录 ID"
// @Success 200 {object} response.Envelope{data=object} "解封成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "记录不存在"
// @Router /admin/ip-bans/{id} [delete]
func (h AdminInsightHandler) RemoveBan(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		badRequest(c)
		return
	}
	result := h.db.Model(&model.IPBan{}).Where("id = ?", id).Update("active", false)
	if result.Error != nil {
		internalError(c)
		return
	}
	if result.RowsAffected == 0 {
		response.Error(c, http.StatusNotFound, 404, "封禁记录不存在")
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// Chat AI 单次对话
// @Summary AI 单次对话
// @Description 携带历史消息列表向后台配置的 AI 大模型发起单次问答
// @Tags AI 助手与智能工具
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body AIChatRequest true "对话消息列表"
// @Success 200 {object} response.Envelope{data=AIChatResponse} "AI 回复"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 502 {object} response.ErrorResponse "模型调用失败"
// @Router /admin/ai/chat [post]
func (h AdminInsightHandler) Chat(c *gin.Context) {
	var req AIChatRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Messages) == 0 || len(req.Messages) > 20 {
		badRequest(c)
		return
	}
	answer, err := h.chat(c, req.Messages)
	if err != nil {
		response.Error(c, http.StatusBadGateway, 502, aiErrorMessage(err))
		return
	}
	response.Success(c, AIChatResponse{Answer: answer, Mode: "deepseek", Model: h.model})
}

// Beautify 文章 AI 润色美化
// @Summary 文章 AI 润色美化
// @Description 调用大模型自动优化文章标题、摘要和 Markdown 正文排版
// @Tags AI 助手与智能工具
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param request body BeautifyRequest true "文章润色请求"
// @Success 200 {object} response.Envelope{data=BeautifyResponse} "润色结果"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 502 {object} response.ErrorResponse "润色处理失败"
// @Router /admin/ai/beautify [post]
func (h AdminInsightHandler) Beautify(c *gin.Context) {
	var req BeautifyRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		badRequest(c)
		return
	}
	prompt := fmt.Sprintf("请把下面这篇技术文章润色成适合个人技术博客发布的中文 Markdown。保留事实和代码，不要编造内容；优化标题、摘要、层级、段落和可读性。只返回 JSON，字段必须是 title、summary、content，不要 Markdown 代码围栏。\n原标题：%s\n原摘要：%s\n原正文：\n%s", req.Title, req.Summary, req.Content)
	answer, err := h.chat(c, []ai.Message{{Role: "system", Content: "你是严谨的技术博客编辑。"}, {Role: "user", Content: prompt}})
	if err != nil {
		response.Error(c, http.StatusBadGateway, 502, aiErrorMessage(err))
		return
	}
	result, err := parseBeautifyResponse(answer, req)
	if err != nil {
		response.Error(c, http.StatusBadGateway, 502, "AI 返回格式无法识别，请重试")
		return
	}
	response.Success(c, result)
}

func (h AdminInsightHandler) configured() bool {
	if h.runtime != nil {
		return h.runtime.Configured()
	}
	return h.aiClient != nil && h.aiClient.Configured()
}

func (h AdminInsightHandler) chat(c *gin.Context, messages []ai.Message) (string, error) {
	if h.runtime != nil {
		adminID, ok := adminIDFromContext(c)
		if !ok {
			return "", service.ErrAIClientUnavailable
		}
		result, err := h.runtime.Chat(c.Request.Context(), adminID, ai.ChatRequest{Messages: messages, Model: h.model})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	}
	if h.aiClient == nil || !h.aiClient.Configured() {
		return "", service.ErrAIClientUnavailable
	}
	return h.aiClient.Chat(c.Request.Context(), messages)
}

func (h AdminInsightHandler) readAnalytics() (AnalyticsDTO, error) {
	var result AnalyticsDTO
	if err := h.db.Model(&model.AccessLog{}).Count(&result.TotalRequests).Error; err != nil {
		return result, err
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if err := h.db.Model(&model.AccessLog{}).Where("created_at >= ?", start).Count(&result.TodayRequests).Error; err != nil {
		return result, err
	}
	if err := h.db.Model(&model.AccessLog{}).Where("status >= ?", 400).Count(&result.FailedRequests).Error; err != nil {
		return result, err
	}
	if err := h.db.Model(&model.AccessLog{}).Distinct("ip").Count(&result.UniqueIPs).Error; err != nil {
		return result, err
	}
	var ips []struct {
		IP       string
		Requests int64
		Failures int64
		LastSeen time.Time
	}
	if err := h.db.Model(&model.AccessLog{}).Select("ip, COUNT(*) AS requests, SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) AS failures, MAX(created_at) AS last_seen").Group("ip").Order("requests desc").Limit(10).Scan(&ips).Error; err != nil {
		return result, err
	}
	var bans []model.IPBan
	if err := h.db.Where("active = ?", true).Find(&bans).Error; err != nil {
		return result, err
	}
	banSet := map[string]bool{}
	for _, ban := range bans {
		banSet[ban.IP] = true
	}
	for _, item := range ips {
		result.TopIPs = append(result.TopIPs, TopIPDTO{IP: item.IP, Requests: item.Requests, Failures: item.Failures, LastSeen: formatTime(item.LastSeen), Banned: banSet[item.IP]})
	}
	var paths []TopPathDTO
	if err := h.db.Model(&model.AccessLog{}).Select("path, COUNT(*) AS requests").Group("path").Order("requests desc").Limit(10).Scan(&paths).Error; err != nil {
		return result, err
	}
	result.TopPaths = paths
	var recent []model.IPBan
	if err := h.db.Order("created_at desc").Limit(10).Find(&recent).Error; err != nil {
		return result, err
	}
	for _, ban := range recent {
		result.RecentBans = append(result.RecentBans, ipBanDTO(ban))
	}
	return result, nil
}

func adminVisitorDTO(user model.User) AdminVisitorDTO {
	return AdminVisitorDTO{ID: user.ID, Username: user.Username, Email: user.Email, Nickname: user.Nickname, Role: user.Role, CreatedAt: formatTime(user.CreatedAt)}
}

func ipBanDTO(ban model.IPBan) IPBanDTO {
	var expires *string
	if ban.ExpiresAt != nil {
		value := formatTime(*ban.ExpiresAt)
		expires = &value
	}
	return IPBanDTO{ID: ban.ID, IP: ban.IP, Reason: ban.Reason, Active: ban.Active, ExpiresAt: expires, CreatedAt: formatTime(ban.CreatedAt)}
}

func parseBeautifyResponse(answer string, fallback BeautifyRequest) (BeautifyResponse, error) {
	answer = strings.TrimSpace(answer)
	answer = strings.TrimPrefix(answer, "```json")
	answer = strings.TrimPrefix(answer, "```")
	answer = strings.TrimSuffix(answer, "```")
	answer = strings.TrimSpace(answer)
	var result BeautifyResponse
	if err := json.Unmarshal([]byte(answer), &result); err != nil {
		return result, err
	}
	if result.Title == "" {
		result.Title = fallback.Title
	}
	if result.Summary == "" {
		result.Summary = fallback.Summary
	}
	if result.Content == "" {
		return result, errors.New("empty content")
	}
	return result, nil
}

func aiErrorMessage(err error) string {
	var providerErr *ai.ProviderError
	if errors.As(err, &providerErr) && providerErr.Kind == ai.ProviderErrorConfig {
		err = errors.New("provider is not configured")
	}
	if strings.Contains(err.Error(), "not configured") {
		return "DeepSeek 尚未配置，请在服务器环境变量中设置 BLOG_AI_API_KEY"
	}
	return "DeepSeek 请求失败，请稍后重试"
}
