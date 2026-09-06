package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

type AdminAIHandler struct {
	service *service.AIConversationService
}

type ConversationRequest struct {
	Title string `json:"title"`
}
type ConversationMessageRequest struct {
	Content string `json:"content"`
}

type AIConversationDTO struct {
	ID            uint    `json:"id"`
	Title         string  `json:"title"`
	TitleMode     string  `json:"titleMode"`
	Model         string  `json:"model"`
	MessageCount  int     `json:"messageCount"`
	LastMessageAt *string `json:"lastMessageAt"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
}

type AIMessageDTO struct {
	ID           uint   `json:"id"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	Status       string `json:"status"`
	Sequence     int    `json:"sequence"`
	Model        string `json:"model"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

type AIConversationDetailDTO struct {
	AIConversationDTO
	Messages []AIMessageDTO `json:"messages"`
}

func NewAdminAIHandler(service *service.AIConversationService) AdminAIHandler {
	return AdminAIHandler{service: service}
}

// List 获取 AI 对话会话列表
// @Summary 获取 AI 对话会话列表
// @Description 查询当前管理员创建的全部 AI 对话会话
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=[]AIConversationDTO} "会话列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ai/conversations [get]
func (h AdminAIHandler) List(c *gin.Context) {
	adminID, ok := adminIDFromContext(c)
	if !ok {
		return
	}
	conversations, err := h.service.List(c.Request.Context(), adminID)
	if err != nil {
		internalError(c)
		return
	}
	list := make([]AIConversationDTO, 0, len(conversations))
	for _, conversation := range conversations {
		list = append(list, aiConversationDTO(conversation))
	}
	response.Success(c, list)
}

// Create 新建 AI 对话会话
// @Summary 新建 AI 对话会话
// @Description 创建一个空对话会话，随后可调用流式对话接口交流
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=AIConversationDTO} "新建会话"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ai/conversations [post]
func (h AdminAIHandler) Create(c *gin.Context) {
	adminID, ok := adminIDFromContext(c)
	if !ok {
		return
	}
	conversation, err := h.service.Create(c.Request.Context(), adminID)
	if err != nil {
		internalError(c)
		return
	}
	response.Success(c, aiConversationDTO(conversation))
}

// Get 获取指定 AI 会话详情与消息历史
// @Summary 获取指定 AI 会话详情与历史消息
// @Description 根据会话 ID 获取会话元数据及完整的历史消息记录
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "会话 ID"
// @Success 200 {object} response.Envelope{data=AIConversationDetailDTO} "会话详情"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "会话不存在"
// @Router /admin/ai/conversations/{id} [get]
func (h AdminAIHandler) Get(c *gin.Context) {
	adminID, id, ok := adminConversationID(c)
	if !ok {
		return
	}
	conversation, messages, err := h.service.Get(c.Request.Context(), adminID, id)
	if !h.respondServiceError(c, err) {
		return
	}
	list := make([]AIMessageDTO, 0, len(messages))
	for _, message := range messages {
		list = append(list, aiMessageDTO(message))
	}
	response.Success(c, AIConversationDetailDTO{AIConversationDTO: aiConversationDTO(conversation), Messages: list})
}

// Rename 重命名 AI 对话会话
// @Summary 重命名 AI 对话会话
// @Description 修改会话标题名称
// @Tags AI 助手与智能工具
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "会话 ID"
// @Param request body ConversationRequest true "新标题"
// @Success 200 {object} response.Envelope{data=AIConversationDTO} "更新成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "会话不存在"
// @Router /admin/ai/conversations/{id} [patch]
func (h AdminAIHandler) Rename(c *gin.Context) {
	adminID, id, ok := adminConversationID(c)
	if !ok {
		return
	}
	var req ConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	conversation, err := h.service.Rename(c.Request.Context(), adminID, id, req.Title)
	if !h.respondServiceError(c, err) {
		return
	}
	response.Success(c, aiConversationDTO(conversation))
}

// Delete 删除单个 AI 对话会话
// @Summary 删除单个 AI 对话会话
// @Description 根据会话 ID 删除对话及其所有消息
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "会话 ID"
// @Success 200 {object} response.Envelope{data=object} "删除成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "会话不存在"
// @Router /admin/ai/conversations/{id} [delete]
func (h AdminAIHandler) Delete(c *gin.Context) {
	adminID, id, ok := adminConversationID(c)
	if !ok {
		return
	}
	if !h.respondServiceError(c, h.service.Delete(c.Request.Context(), adminID, id)) {
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// Clear 清空当前管理员的所有 AI 对话会话
// @Summary 清空全部 AI 对话会话
// @Description 批量删除当前管理员名下的全部对话记录
// @Tags AI 助手与智能工具
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=object} "清空成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/ai/conversations [delete]
func (h AdminAIHandler) Clear(c *gin.Context) {
	adminID, ok := adminIDFromContext(c)
	if !ok {
		return
	}
	if !h.respondServiceError(c, h.service.Clear(c.Request.Context(), adminID)) {
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

// StreamMessage 发送消息并流式接收 AI 回复 (SSE)
// @Summary 发送消息并流式接收 AI 回复 (SSE)
// @Description 向指定会话发送提问，并通过 Server-Sent Events (SSE) 持续接收流式输出
// @Tags AI 助手与智能工具
// @Accept json
// @Produce text/event-stream
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "会话 ID"
// @Param request body ConversationMessageRequest true "消息内容"
// @Success 200 {string} string "SSE 流式事件 (meta/delta/done/error)"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "会话不存在"
// @Router /admin/ai/conversations/{id}/messages/stream [post]
func (h AdminAIHandler) StreamMessage(c *gin.Context) {
	adminID, id, ok := adminConversationID(c)
	if !ok {
		return
	}
	var req ConversationMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Content) == "" {
		badRequest(c)
		return
	}
	if _, _, err := h.service.Get(c.Request.Context(), adminID, id); !h.respondServiceError(c, err) {
		return
	}
	response.PrepareSSE(c)
	var message model.AIMessage
	var messageID uint
	message, err := h.service.StreamMessageWithStart(c.Request.Context(), adminID, id, req.Content, func(started model.AIMessage) error {
		messageID = started.ID
		return response.WriteSSE(c, "meta", gin.H{"conversationId": id, "messageId": messageID, "status": model.AIMessageStatusStreaming})
	}, func(delta string) error {
		return response.WriteSSE(c, "delta", gin.H{"messageId": messageID, "content": delta})
	})
	if err != nil {
		payload := gin.H{"code": streamErrorCode(err), "message": streamErrorMessage(err), "status": streamErrorStatus(message, err)}
		if message.ID != 0 {
			payload["messageId"] = message.ID
		}
		_ = response.WriteSSE(c, "error", payload)
		return
	}
	if message.Status != model.AIMessageStatusCompleted {
		_ = response.WriteSSE(c, "error", gin.H{"messageId": message.ID, "status": streamErrorStatus(message, err), "code": 502, "message": "AI stream did not complete"})
		return
	}
	_ = response.WriteSSE(c, "done", gin.H{"messageId": message.ID, "status": model.AIMessageStatusCompleted, "message": aiMessageDTO(message)})
}

func (h AdminAIHandler) respondServiceError(c *gin.Context, err error) bool {
	if err == nil {
		return true
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Error(c, http.StatusNotFound, 404, "资源不存在")
	case errors.Is(err, service.ErrConversationTitleRequired), errors.Is(err, service.ErrConversationContentRequired), errors.Is(err, service.ErrStreamCallbackRequired):
		badRequest(c)
	default:
		internalError(c)
	}
	return false
}

func adminIDFromContext(c *gin.Context) (uint, bool) {
	claims, ok := c.Get(middleware.CurrentUserKey)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return 0, false
	}
	current, ok := claims.(*auth.Claims)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return 0, false
	}
	return current.UserID, true
}

func adminConversationID(c *gin.Context) (uint, uint, bool) {
	adminID, ok := adminIDFromContext(c)
	if !ok {
		return 0, 0, false
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		badRequest(c)
		return 0, 0, false
	}
	return adminID, uint(id), true
}

func aiConversationDTO(value model.AIConversation) AIConversationDTO {
	var lastMessageAt *string
	if value.LastMessageAt != nil {
		formatted := formatTime(*value.LastMessageAt)
		lastMessageAt = &formatted
	}
	return AIConversationDTO{ID: value.ID, Title: value.Title, TitleMode: value.TitleMode, Model: value.Model, MessageCount: value.MessageCount, LastMessageAt: lastMessageAt, CreatedAt: formatTime(value.CreatedAt), UpdatedAt: formatTime(value.UpdatedAt)}
}

func aiMessageDTO(value model.AIMessage) AIMessageDTO {
	return AIMessageDTO{ID: value.ID, Role: value.Role, Content: value.Content, Status: value.Status, Sequence: value.Sequence, Model: value.Model, ErrorMessage: value.ErrorMessage, CreatedAt: formatTime(value.CreatedAt)}
}
func streamErrorStatus(message model.AIMessage, err error) string {
	if err != nil && (message.Status == "" || message.Status == model.AIMessageStatusCompleted) {
		return model.AIMessageStatusFailed
	}
	if message.Status != "" {
		return message.Status
	}
	return model.AIMessageStatusFailed
}
func streamErrorCode(err error) int {
	if errors.Is(err, service.ErrAIClientUnavailable) {
		return 503
	}
	return 502
}
func streamErrorMessage(err error) string {
	if errors.Is(err, service.ErrAIClientUnavailable) {
		return "DeepSeek 尚未配置，请在服务器环境变量中设置 BLOG_AI_API_KEY"
	}
	return "AI 流式请求失败，请稍后重试"
}
