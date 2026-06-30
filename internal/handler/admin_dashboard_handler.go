package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type AdminDashboardHandler struct {
	db  *gorm.DB
	cfg config.Config
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

type AIAnalysisDTO struct {
	Mode    string   `json:"mode"`
	Summary string   `json:"summary"`
	Signals []string `json:"signals"`
}

type DashboardDTO struct {
	Stats          DashboardStatsDTO `json:"stats"`
	AIAnalysis     AIAnalysisDTO     `json:"aiAnalysis"`
	RecentComments []CommentDTO      `json:"recentComments"`
}

func NewAdminDashboardHandler(db *gorm.DB, cfg config.Config) AdminDashboardHandler {
	return AdminDashboardHandler{db: db, cfg: cfg}
}

func (h AdminDashboardHandler) GetDashboard(c *gin.Context) {
	stats, err := h.readStats()
	if err != nil {
		internalError(c)
		return
	}

	recentComments, err := h.readRecentComments()
	if err != nil {
		internalError(c)
		return
	}

	response.Success(c, DashboardDTO{
		Stats:          stats,
		AIAnalysis:     h.analyze(stats),
		RecentComments: recentComments,
	})
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
	if err := h.db.Preload("User").Preload("Post").
		Order("created_at desc").
		Limit(5).
		Find(&comments).Error; err != nil {
		return nil, err
	}

	list := make([]CommentDTO, 0, len(comments))
	for _, comment := range comments {
		list = append(list, commentDTO(comment))
	}

	return list, nil
}

func (h AdminDashboardHandler) analyze(stats DashboardStatsDTO) AIAnalysisDTO {
	mode := "local"
	if h.cfg.AI.APIKey != "" && h.cfg.AI.Provider != "" && h.cfg.AI.Provider != "local" {
		mode = "configured"
	}

	signals := []string{
		fmt.Sprintf("累计阅读 %d 次", stats.TotalViews),
		fmt.Sprintf("当前评论 %d 条", stats.CommentCount),
		fmt.Sprintf("注册访客 %d 位", stats.VisitorCount),
	}
	summary := "评论功能已接入，建议优先关注高阅读但低评论的文章，并在文章结尾加入明确提问来提升互动。"
	if stats.CommentCount == 0 {
		summary = "当前还没有评论数据，可以先在热门文章结尾增加问题，引导访客登录后参与讨论。"
	} else if stats.HiddenCommentCount > 0 {
		summary = "已有隐藏评论，建议定期巡检评论区，保持讨论质量，同时保留真实访客反馈。"
	}

	return AIAnalysisDTO{
		Mode:    mode,
		Summary: summary,
		Signals: signals,
	}
}
