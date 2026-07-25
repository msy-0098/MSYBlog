package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/service"
)

type CommentHandler struct {
	db            *gorm.DB
	notifications *service.NotificationService
}

type CommentRequest struct {
	Content string `json:"content"`
}

type CommentAuthorDTO struct {
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
}

type CommentDTO struct {
	ID        uint             `json:"id"`
	PostID    uint             `json:"postId"`
	PostTitle string           `json:"postTitle,omitempty"`
	Content   string           `json:"content"`
	Status    string           `json:"status"`
	Author    CommentAuthorDTO `json:"author"`
	CreatedAt string           `json:"createdAt"`
}

func NewCommentHandler(db *gorm.DB) CommentHandler {
	return CommentHandler{db: db}
}

func NewCommentHandlerWithNotifications(db *gorm.DB, notifications *service.NotificationService) CommentHandler {
	return CommentHandler{db: db, notifications: notifications}
}

func (h CommentHandler) ListPostComments(c *gin.Context) {
	post, ok := h.findPublishedPostBySlug(c)
	if !ok {
		return
	}

	var comments []model.Comment
	if err := h.db.Preload("User").
		Where("post_id = ? AND status = ?", post.ID, model.CommentStatusApproved).
		Order("CASE WHEN status = 'pending' THEN 0 ELSE 1 END, created_at DESC").
		Find(&comments).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]CommentDTO, 0, len(comments))
	for _, comment := range comments {
		list = append(list, commentDTO(comment))
	}

	response.Success(c, PageDTO[CommentDTO]{List: list, Page: 1, PageSize: len(list), Total: int64(len(list))})
}

func (h CommentHandler) CreatePostComment(c *gin.Context) {
	claims, ok := c.MustGet(middleware.CurrentUserKey).(*auth.Claims)
	if !ok || claims.Role != model.UserRoleVisitor {
		response.Error(c, http.StatusForbidden, 403, "请使用访客账号评论")
		return
	}

	post, found := h.findPublishedPostBySlug(c)
	if !found {
		return
	}

	var req CommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}
	content := strings.TrimSpace(req.Content)
	if len(content) < 2 || len(content) > 1000 {
		badRequest(c)
		return
	}

	comment := model.Comment{
		PostID:  post.ID,
		UserID:  claims.UserID,
		Content: content,
		Status:  model.CommentStatusPending,
	}
	if err := h.db.Create(&comment).Error; err != nil {
		internalError(c)
		return
	}

	if err := h.db.Preload("User").Preload("Post").First(&comment, comment.ID).Error; err != nil {
		internalError(c)
		return
	}

	if h.notifications != nil {
		author := strings.TrimSpace(comment.User.Nickname)
		if author == "" {
			author = strings.TrimSpace(comment.User.Email)
		}
		if author == "" {
			author = "访客"
		}
		postTitle := strings.TrimSpace(comment.Post.Title)
		if postTitle == "" {
			postTitle = post.Title
		}
		body := service.CommentNotificationBody(author, postTitle, comment.Content)
		go h.notifications.NotifyAdmins(context.Background(), service.NotifyInput{
			Kind:         model.NotificationKindComment,
			Title:        "新评论待处理",
			Body:         body,
			RefType:      "comment",
			RefID:        comment.ID,
			EmailSubject: "【马森雨的博客】有新评论",
			EmailBody:    body,
			ActionPath:   "/admin/comments",
		})
	}

	response.Success(c, commentDTO(comment))
}

func (h CommentHandler) ListAdminComments(c *gin.Context) {
	page, pageSize := pagination(c)

	var total int64
	if err := h.db.Model(&model.Comment{}).Count(&total).Error; err != nil {
		internalError(c)
		return
	}

	var comments []model.Comment
	if err := h.db.Preload("User").Preload("Post").
		Order("created_at desc").
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&comments).Error; err != nil {
		internalError(c)
		return
	}

	list := make([]CommentDTO, 0, len(comments))
	for _, comment := range comments {
		list = append(list, commentDTO(comment))
	}

	response.Success(c, PageDTO[CommentDTO]{List: list, Page: page, PageSize: pageSize, Total: total})
}

func (h CommentHandler) UpdateAdminComment(c *gin.Context) {
	var comment model.Comment
	if !h.findCommentByID(c, &comment) {
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c)
		return
	}

	status := strings.TrimSpace(req.Status)
	if status != model.CommentStatusPending && status != model.CommentStatusApproved && status != model.CommentStatusHidden {
		badRequest(c)
		return
	}

	comment.Status = status
	if err := h.db.Save(&comment).Error; err != nil {
		internalError(c)
		return
	}

	if err := h.db.Preload("User").Preload("Post").First(&comment, comment.ID).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, commentDTO(comment))
}

func (h CommentHandler) DeleteAdminComment(c *gin.Context) {
	var comment model.Comment
	if !h.findCommentByID(c, &comment) {
		return
	}

	if err := h.db.Delete(&comment).Error; err != nil {
		internalError(c)
		return
	}

	response.Success(c, gin.H{"deleted": true})
}

func (h CommentHandler) findPublishedPostBySlug(c *gin.Context) (model.Post, bool) {
	var post model.Post
	if err := h.db.Where("slug = ? AND status = ?", c.Param("slug"), model.PostStatusPublished).First(&post).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Error(c, http.StatusNotFound, 404, "文章不存在")
			return post, false
		}
		internalError(c)
		return post, false
	}

	return post, true
}

func (h CommentHandler) findCommentByID(c *gin.Context, comment *model.Comment) bool {
	if !findByIDParam(c, h.db, comment) {
		return false
	}

	return true
}

func commentDTO(comment model.Comment) CommentDTO {
	return CommentDTO{
		ID:        comment.ID,
		PostID:    comment.PostID,
		PostTitle: comment.Post.Title,
		Content:   comment.Content,
		Status:    comment.Status,
		Author: CommentAuthorDTO{
			Email:    comment.User.Email,
			Nickname: comment.User.Nickname,
		},
		CreatedAt: formatTime(comment.CreatedAt),
	}
}
