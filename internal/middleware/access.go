package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
	"masenyu.top/blog/backend/internal/security"
)

type AccessTracker struct {
	db  *gorm.DB
	now func() time.Time
}

func NewAccessTracker(db *gorm.DB) AccessTracker {
	return AccessTracker{db: db, now: time.Now}
}

func (t AccessTracker) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := ClientIP(c)
		if t.isBanned(ip) {
			response.Error(c, http.StatusForbidden, 403, "当前 IP 已被安全策略封禁")
			c.Abort()
			return
		}

		c.Next()
		status := c.Writer.Status()
		log := model.AccessLog{
			IP:        ip,
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			Status:    status,
			UserAgent: c.Request.UserAgent(),
		}
		if err := t.db.Create(&log).Error; err != nil {
			// Tracking must never make the actual blog request fail.
			return
		}

		if security.IsSuspiciousPath(c.Request.URL.Path) {
			_ = t.ban(ip, "命中恶意扫描路径", 24*time.Hour)
			return
		}
		if status >= http.StatusBadRequest {
			var failures int64
			t.db.Model(&model.AccessLog{}).
				Where("ip = ? AND status >= ? AND created_at >= ?", ip, http.StatusBadRequest, t.now().Add(-5*time.Minute)).
				Count(&failures)
			if security.ShouldAutoBan(failures, status) {
				_ = t.ban(ip, "短时间内连续请求失败", 1*time.Hour)
			}
		}
	}
}

func ClientIP(c *gin.Context) string {
	if value := strings.TrimSpace(c.GetHeader("X-Real-IP")); value != "" {
		return value
	}
	if value := strings.TrimSpace(c.GetHeader("X-Forwarded-For")); value != "" {
		return strings.TrimSpace(strings.Split(value, ",")[0])
	}
	value, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
	if err == nil {
		return value
	}
	return strings.TrimSpace(c.Request.RemoteAddr)
}

func (t AccessTracker) isBanned(ip string) bool {
	var ban model.IPBan
	if err := t.db.Where("ip = ? AND active = ?", ip, true).First(&ban).Error; err != nil {
		return false
	}
	if ban.ExpiresAt != nil && ban.ExpiresAt.Before(t.now()) {
		_ = t.db.Model(&ban).Update("active", false).Error
		return false
	}
	return true
}

func (t AccessTracker) ban(ip, reason string, duration time.Duration) error {
	if strings.TrimSpace(ip) == "" {
		return nil
	}
	expiresAt := t.now().Add(duration)
	return t.db.Where("ip = ?", ip).Assign(model.IPBan{
		IP:        ip,
		Reason:    reason,
		Active:    true,
		ExpiresAt: &expiresAt,
	}).FirstOrCreate(&model.IPBan{}).Error
}
