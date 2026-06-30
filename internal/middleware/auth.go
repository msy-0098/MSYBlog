package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/auth"
	"masenyu.top/blog/backend/internal/model"
	"masenyu.top/blog/backend/internal/response"
)

const CurrentUserKey = "currentUser"

func RequireAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseBearerClaims(c, secret)
		if !ok {
			response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
			c.Abort()
			return
		}

		c.Set(CurrentUserKey, claims)
		c.Next()
	}
}

func RequireAdmin(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseBearerClaims(c, secret)
		if !ok {
			response.Error(c, http.StatusUnauthorized, 401, "未登录或 token 失效")
			c.Abort()
			return
		}

		if claims.Role != model.UserRoleAdmin {
			response.Error(c, http.StatusForbidden, 403, "无权访问")
			c.Abort()
			return
		}

		c.Set(CurrentUserKey, claims)
		c.Next()
	}
}

func parseBearerClaims(c *gin.Context, secret string) (*auth.Claims, bool) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, false
	}

	claims, err := auth.ParseToken(secret, strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
	if err != nil {
		return nil, false
	}

	return claims, true
}
