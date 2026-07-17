package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	AdminTokenCookie   = "admin_token"
	VisitorTokenCookie = "visitor_token"
	// Match JWT expiry (24h).
	AuthCookieMaxAge = 24 * 60 * 60
)

func requestSecure(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

// SetAuthCookie stores a JWT in an httpOnly cookie.
func SetAuthCookie(c *gin.Context, name, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, token, AuthCookieMaxAge, "/", "", requestSecure(c), true)
}

// ClearAuthCookie removes an auth cookie.
func ClearAuthCookie(c *gin.Context, name string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", "", requestSecure(c), true)
}