package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/response"
)

const CurrentUserKey = "currentUser"

func RequireAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
			c.Abort()
			return
		}

		claims, err := auth.ParseToken(secret, strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
			c.Abort()
			return
		}

		c.Set(CurrentUserKey, claims)
		c.Next()
	}
}
