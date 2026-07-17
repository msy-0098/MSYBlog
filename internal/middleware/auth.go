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
		claims, ok := parseAuthClaims(c, secret, VisitorTokenCookie, AdminTokenCookie)
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
		claims, ok := parseAuthClaims(c, secret, AdminTokenCookie)
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

// parseAuthClaims accepts Authorization Bearer token first, then httpOnly cookies.
func parseAuthClaims(c *gin.Context, secret string, cookieNames ...string) (*auth.Claims, bool) {
	header := c.GetHeader("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		claims, err := auth.ParseToken(secret, strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err == nil {
			return claims, true
		}
	}

	for _, name := range cookieNames {
		raw, err := c.Cookie(name)
		if err != nil || strings.TrimSpace(raw) == "" {
			continue
		}
		claims, err := auth.ParseToken(secret, strings.TrimSpace(raw))
		if err == nil {
			return claims, true
		}
	}

	return nil, false
}