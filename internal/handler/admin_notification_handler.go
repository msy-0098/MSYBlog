package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

// AdminNotificationHandler serves administrator in-app notifications.
type AdminNotificationHandler struct {
	notifications *service.NotificationService
}

func NewAdminNotificationHandler(notifications *service.NotificationService) AdminNotificationHandler {
	return AdminNotificationHandler{notifications: notifications}
}

type NotificationDTO struct {
	ID        uint    `json:"id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	RefType   string  `json:"refType,omitempty"`
	RefID     uint    `json:"refId,omitempty"`
	Unread    bool    `json:"unread"`
	CreatedAt string  `json:"createdAt"`
	ReadAt    *string `json:"readAt,omitempty"`
}

// List 管理端通知列表
// @Summary 管理端通知列表
// @Description 分页查询当前管理员的站内通知消息（支持按类型或仅未读过滤）
// @Tags 站内通知管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param page query int false "当前页码" default(1)
// @Param pageSize query int false "每页条数" default(20)
// @Param kind query string false "通知类别(comment/user/security/system)"
// @Param unread query bool false "仅查未读通知"
// @Success 200 {object} response.Envelope{data=PageDTO[NotificationDTO]} "通知列表"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/notifications [get]
func (h AdminNotificationHandler) List(c *gin.Context) {
	adminID, ok := adminUserID(c)
	if !ok {
		return
	}
	if h.notifications == nil {
		response.Success(c, PageDTO[NotificationDTO]{List: []NotificationDTO{}, Page: 1, PageSize: 20, Total: 0})
		return
	}
	page, pageSize := pagination(c)
	kind := strings.TrimSpace(c.Query("kind"))
	unreadOnly := strings.EqualFold(c.Query("unread"), "true") || c.Query("unread") == "1"
	if kind == "unread" {
		unreadOnly = true
		kind = ""
	}
	list, total, err := h.notifications.List(c.Request.Context(), adminID, kind, unreadOnly, page, pageSize)
	if err != nil {
		internalError(c)
		return
	}
	items := make([]NotificationDTO, 0, len(list))
	for _, item := range list {
		items = append(items, toNotificationDTO(item))
	}
	response.Success(c, PageDTO[NotificationDTO]{List: items, Page: page, PageSize: pageSize, Total: total})
}

// UnreadCount 获取未读通知数量
// @Summary 获取未读通知数量
// @Description 统计当前管理员未读通知总数，用于角标展示
// @Tags 站内通知管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=object} "未读数统计"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/notifications/unread-count [get]
func (h AdminNotificationHandler) UnreadCount(c *gin.Context) {
	adminID, ok := adminUserID(c)
	if !ok {
		return
	}
	if h.notifications == nil {
		response.Success(c, gin.H{"count": 0})
		return
	}
	count, err := h.notifications.UnreadCount(c.Request.Context(), adminID)
	if err != nil {
		internalError(c)
		return
	}
	response.Success(c, gin.H{"count": count})
}

// MarkRead 标记单个通知已读
// @Summary 标记单个通知已读
// @Description 将指定 ID 的通知标记为已读
// @Tags 站内通知管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Param id path int true "通知 ID"
// @Success 200 {object} response.Envelope{data=object} "标记成功"
// @Failure 400 {object} response.ErrorResponse "参数错误"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Failure 404 {object} response.ErrorResponse "通知不存在"
// @Router /admin/notifications/{id}/read [post]
func (h AdminNotificationHandler) MarkRead(c *gin.Context) {
	adminID, ok := adminUserID(c)
	if !ok {
		return
	}
	if h.notifications == nil {
		response.Error(c, http.StatusNotFound, 404, "通知不存在")
		return
	}
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := h.notifications.MarkRead(c.Request.Context(), adminID, id); err != nil {
		response.Error(c, http.StatusNotFound, 404, "通知不存在")
		return
	}
	response.Success(c, gin.H{"updated": true})
}

// MarkAllRead 标记全部通知已读
// @Summary 标记全部通知已读
// @Description 将当前管理员名下的所有未读通知一键标记为已读
// @Tags 站内通知管理
// @Produce json
// @Security BearerAuth
// @Security CsrfToken
// @Success 200 {object} response.Envelope{data=object} "批量标记成功"
// @Failure 401 {object} response.ErrorResponse "未登录"
// @Router /admin/notifications/read-all [post]
func (h AdminNotificationHandler) MarkAllRead(c *gin.Context) {
	adminID, ok := adminUserID(c)
	if !ok {
		return
	}
	if h.notifications == nil {
		response.Success(c, gin.H{"updated": 0})
		return
	}
	updated, err := h.notifications.MarkAllRead(c.Request.Context(), adminID)
	if err != nil {
		internalError(c)
		return
	}
	response.Success(c, gin.H{"updated": updated})
}

func toNotificationDTO(item model.Notification) NotificationDTO {
	dto := NotificationDTO{
		ID:        item.ID,
		Kind:      item.Kind,
		Title:     item.Title,
		Body:      item.Body,
		RefType:   item.RefType,
		RefID:     item.RefID,
		Unread:    item.ReadAt == nil,
		CreatedAt: item.CreatedAt.Format(time.RFC3339),
	}
	if item.ReadAt != nil {
		value := item.ReadAt.Format(time.RFC3339)
		dto.ReadAt = &value
	}
	return dto
}

func adminUserID(c *gin.Context) (uint, bool) {
	claims, ok := c.Get(middleware.CurrentUserKey)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return 0, false
	}
	current, ok := claims.(*auth.Claims)
	if !ok || current.UserID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
		return 0, false
	}
	return current.UserID, true
}

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	raw := strings.TrimSpace(c.Param(name))
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value < 1 {
		response.Error(c, http.StatusBadRequest, 400, "无效的通知 ID")
		return 0, false
	}
	return uint(value), true
}