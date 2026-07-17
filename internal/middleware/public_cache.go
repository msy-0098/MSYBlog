package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// PublicReadCache attaches short Cache-Control for anonymous public GET APIs.
// Skips /api/admin and mutating methods. Does not override handlers that already set Cache-Control.
func PublicReadCache() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if c.Request.Method != "GET" {
			return
		}

		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/admin") {
			return
		}
		if c.Writer.Status() < 200 || c.Writer.Status() >= 300 {
			return
		}
		if c.Writer.Header().Get("Cache-Control") != "" {
			return
		}
		// Health checks and auth endpoints stay uncached.
		if path == "/api/health" || strings.HasPrefix(path, "/api/auth") || strings.HasPrefix(path, "/api/visitor") {
			return
		}

		// /api/posts/:slug increments view_count; leave uncached unless handler sets Cache-Control.
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 3 && parts[0] == "api" && parts[1] == "posts" {
			return
		}

		c.Header("Cache-Control", "public, max-age=30, stale-while-revalidate=60")
	}
}
