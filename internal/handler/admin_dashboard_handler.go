package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type AdminDashboardHandler struct {
	db *gorm.DB
}

type DashboardStatsDTO struct {
	PostCount            int64 `json:"postCount"`
	PublishedPostCount   int64 `json:"publishedPostCount"`
	DraftPostCount       int64 `json:"draftPostCount"`
	TotalViews           int64 `json:"totalViews"`
	CommentCount         int64 `json:"commentCount"`
	ApprovedCommentCount int64 `json:"approvedCommentCount"`
	HiddenCommentCount   int64 `json:"hiddenCommentCount"`
	VisitorCount         int64 `json:"visitorCount"`
}

type DashboardDTO struct {
	Stats          DashboardStatsDTO `json:"stats"`
	Analytics      AnalyticsDTO      `json:"analytics"`
	RecentComments []CommentDTO      `json:"recentComments"`
}

func NewAdminDashboardHandler(db *gorm.DB) AdminDashboardHandler {
	return AdminDashboardHandler{db: db}
}

func (h AdminDashboardHandler) GetDashboard(c *gin.Context) {
	stats, err := h.readStats()
	if err != nil {
		internalError(c)
		return
	}
	analytics, err := (AdminInsightHandler{db: h.db}).readAnalytics()
	if err != nil {
		internalError(c)
		return
	}
	recentComments, err := h.readRecentComments()
	if err != nil {
		internalError(c)
		return
	}
	response.Success(c, DashboardDTO{Stats: stats, Analytics: analytics, RecentComments: recentComments})
}

func (h AdminDashboardHandler) readStats() (DashboardStatsDTO, error) {
	var stats DashboardStatsDTO
	if err := h.db.Model(&model.Post{}).Count(&stats.PostCount).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.Post{}).Where("status = ?", model.PostStatusPublished).Count(&stats.PublishedPostCount).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.Post{}).Where("status = ?", model.PostStatusDraft).Count(&stats.DraftPostCount).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.Post{}).Select("COALESCE(SUM(view_count), 0)").Scan(&stats.TotalViews).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.Comment{}).Count(&stats.CommentCount).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.Comment{}).Where("status = ?", model.CommentStatusApproved).Count(&stats.ApprovedCommentCount).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.Comment{}).Where("status = ?", model.CommentStatusHidden).Count(&stats.HiddenCommentCount).Error; err != nil {
		return stats, err
	}
	if err := h.db.Model(&model.User{}).Where("role = ?", model.UserRoleVisitor).Count(&stats.VisitorCount).Error; err != nil {
		return stats, err
	}
	return stats, nil
}

func (h AdminDashboardHandler) readRecentComments() ([]CommentDTO, error) {
	var comments []model.Comment
	if err := h.db.Preload("User").Preload("Post").Order("created_at desc").Limit(5).Find(&comments).Error; err != nil {
		return nil, err
	}
	list := make([]CommentDTO, 0, len(comments))
	for _, comment := range comments {
		list = append(list, commentDTO(comment))
	}
	return list, nil
}
