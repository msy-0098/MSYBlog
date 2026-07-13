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
	ID            uint   `json:"id"`
	Title         string `json:"title"`
	TitleMode     string `json:"titleMode"`
	Model         string `json:"model"`
	MessageCount  int    `json:"messageCount"`
	LastMessageAt string `json:"lastMessageAt"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
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

func NewAdminAIHandler(service *service.AIConversationService) AdminAIHandler {
	return AdminAIHandler{service: service}
}

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
	response.Success(c, ListDTO[AIConversationDTO]{List: list})
}

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
	response.Success(c, gin.H{"conversation": aiConversationDTO(conversation), "messages": list})
}

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

func (h AdminAIHandler) Clear(c *gin.Context) {
	adminID, ok := adminIDFromContext(c)
	if !ok {
		return
	}
	if !h.respondServiceError(c, h.service.Clear(c.Request.Context(), adminID)) {
		return
	}
	response.Success(c, gin.H{"cleared": true})
}

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
	if err := response.WriteSSE(c, "meta", gin.H{"conversationId": id}); err != nil {
		return
	}
	message, err := h.service.StreamMessage(c.Request.Context(), adminID, id, req.Content, func(delta string) error {
		return response.WriteSSE(c, "delta", gin.H{"content": delta})
	})
	if err != nil {
		_ = response.WriteSSE(c, "error", gin.H{"code": streamErrorCode(err), "message": streamErrorMessage(err), "messageId": message.ID})
		return
	}
	_ = response.WriteSSE(c, "done", gin.H{"message": aiMessageDTO(message)})
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
	lastMessageAt := ""
	if value.LastMessageAt != nil {
		lastMessageAt = formatTime(*value.LastMessageAt)
	}
	return AIConversationDTO{ID: value.ID, Title: value.Title, TitleMode: value.TitleMode, Model: value.Model, MessageCount: value.MessageCount, LastMessageAt: lastMessageAt, CreatedAt: formatTime(value.CreatedAt), UpdatedAt: formatTime(value.UpdatedAt)}
}

func aiMessageDTO(value model.AIMessage) AIMessageDTO {
	return AIMessageDTO{ID: value.ID, Role: value.Role, Content: value.Content, Status: value.Status, Sequence: value.Sequence, Model: value.Model, ErrorMessage: value.ErrorMessage, CreatedAt: formatTime(value.CreatedAt)}
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
