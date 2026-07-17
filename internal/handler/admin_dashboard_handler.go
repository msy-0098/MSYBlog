package handler

import (
	"time"

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

type TrendPointDTO struct {
	Date        string `json:"date"`
	Requests    int64  `json:"requests"`
	UniqueIPs   int64  `json:"uniqueIPs"`
	Comments    int64  `json:"comments"`
	NewVisitors int64  `json:"newVisitors"`
}

type TrendsDTO struct {
	Days   int             `json:"days"`
	Points []TrendPointDTO `json:"points"`
}

type DashboardDTO struct {
	Stats          DashboardStatsDTO `json:"stats"`
	Analytics      AnalyticsDTO      `json:"analytics"`
	Trends         TrendsDTO         `json:"trends"`
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
	trends, err := h.readTrends(14)
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
		Analytics:      analytics,
		Trends:         trends,
		RecentComments: recentComments,
	})
}

func (h AdminDashboardHandler) GetTrends(c *gin.Context) {
	days := 14
	if raw := c.Query("days"); raw != "" {
		if parsed := parsePositiveInt(raw, 14); parsed >= 7 && parsed <= 60 {
			days = parsed
		}
	}
	trends, err := h.readTrends(days)
	if err != nil {
		internalError(c)
		return
	}
	response.Success(c, trends)
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

func (h AdminDashboardHandler) readTrends(days int) (TrendsDTO, error) {
	if days < 1 {
		days = 14
	}

	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))

	requestsByDay := map[string]int64{}
	uniqueByDay := map[string]map[string]struct{}{}
	{
		var logs []model.AccessLog
		if err := h.db.Select("ip, created_at").Where("created_at >= ?", start).Find(&logs).Error; err != nil {
			return TrendsDTO{}, err
		}
		for _, log := range logs {
			day := log.CreatedAt.In(now.Location()).Format("2006-01-02")
			requestsByDay[day]++
			if uniqueByDay[day] == nil {
				uniqueByDay[day] = map[string]struct{}{}
			}
			uniqueByDay[day][log.IP] = struct{}{}
		}
	}

	commentsByDay := map[string]int64{}
	{
		var comments []model.Comment
		if err := h.db.Select("created_at").Where("created_at >= ?", start).Find(&comments).Error; err != nil {
			return TrendsDTO{}, err
		}
		for _, item := range comments {
			day := item.CreatedAt.In(now.Location()).Format("2006-01-02")
			commentsByDay[day]++
		}
	}

	visitorsByDay := map[string]int64{}
	{
		var users []model.User
		if err := h.db.Select("created_at").Where("role = ? AND created_at >= ?", model.UserRoleVisitor, start).Find(&users).Error; err != nil {
			return TrendsDTO{}, err
		}
		for _, user := range users {
			day := user.CreatedAt.In(now.Location()).Format("2006-01-02")
			visitorsByDay[day]++
		}
	}

	points := make([]TrendPointDTO, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		points = append(points, TrendPointDTO{
			Date:        day,
			Requests:    requestsByDay[day],
			UniqueIPs:   int64(len(uniqueByDay[day])),
			Comments:    commentsByDay[day],
			NewVisitors: visitorsByDay[day],
		})
	}

	return TrendsDTO{Days: days, Points: points}, nil
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