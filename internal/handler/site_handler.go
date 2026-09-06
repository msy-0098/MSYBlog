package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/config"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

type SiteHandler struct {
	db       *gorm.DB
	fallback config.SiteConfig
}

type SiteProfile struct {
	SiteTitle   string   `json:"siteTitle"`
	Subtitle    string   `json:"subtitle"`
	Owner       string   `json:"owner"`
	Domain      string   `json:"domain"`
	Description string   `json:"description"`
	NavItems    []string `json:"navItems"`
}

func NewSiteHandler(db *gorm.DB, cfg config.Config) SiteHandler {
	return SiteHandler{
		db:       db,
		fallback: cfg.Site,
	}
}

// GetSite 获取站点公开配置
// @Summary 获取站点公开配置
// @Description 获取博客站点的基础公开信息，包括站点标题、副标题、站长名、域名、描述及导航项
// @Tags 站点与公开信息
// @Produce json
// @Success 200 {object} response.Envelope{data=SiteProfile} "获取成功"
// @Failure 500 {object} response.ErrorResponse "服务器错误"
// @Router /site [get]
func (h SiteHandler) GetSite(c *gin.Context) {
	settings, err := h.loadSettings()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, "服务器错误")
		return
	}

	response.SuccessPublic(c, SiteProfile{
		SiteTitle:   firstNonEmpty(settings["siteTitle"], h.fallback.SiteTitle),
		Subtitle:    firstNonEmpty(settings["subtitle"], h.fallback.Subtitle),
		Owner:       firstNonEmpty(settings["owner"], h.fallback.Owner),
		Domain:      firstNonEmpty(settings["domain"], h.fallback.Domain),
		Description: firstNonEmpty(settings["description"], h.fallback.Description),
		NavItems:    navItems(settings["navItems"], h.fallback.NavItems),
	}, 60*time.Second)
}

func (h SiteHandler) loadSettings() (map[string]string, error) {
	var rows []model.SiteSetting
	if err := h.db.Find(&rows).Error; err != nil {
		return nil, err
	}

	settings := make(map[string]string, len(rows))
	for _, row := range rows {
		settings[row.Key] = row.Value
	}

	return settings, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func navItems(value string, fallback []string) []string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}

	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return fallback
	}

	return items
}
