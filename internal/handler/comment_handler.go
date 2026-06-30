package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/middleware"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type CommentHandler struct {
	db *gorm.DB
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

func (h CommentHandler) ListPostComments(c *gin.Context) {
	post, ok := h.findPublishedPostBySlug(c)
	if !ok {
		return
	}

	var comments []model.Comment
	if err := h.db.Preload("User").
		Where("post_id = ? AND status = ?", post.ID, model.CommentStatusApproved).
		Order("created_at desc").
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
		Status:  model.CommentStatusApproved,
	}
	if err := h.db.Create(&comment).Error; err != nil {
		internalError(c)
		return
	}

	if err := h.db.Preload("User").Preload("Post").First(&comment, comment.ID).Error; err != nil {
		internalError(c)
		return
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
	if status != model.CommentStatusApproved && status != model.CommentStatusHidden {
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
