package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	AdminTokenCookie   = "admin_token"
	VisitorTokenCookie = "visitor_token"
	Pending2FACookie   = "admin_2fa_pending"
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
// Always mark Secure when the public site is served over HTTPS (via proxy header),
// so browsers accept the cookie on masenyu.top.
func SetAuthCookie(c *gin.Context, name, token string) {
	secure := requestSecure(c)
	c.SetSameSite(http.SameSiteLaxMode)
	// Path must be "/" so both /admin and /api routes can send it; Domain empty = host-only.
	c.SetCookie(name, token, AuthCookieMaxAge, "/", "", secure, true)
	// Also expose a non-httpOnly marker is intentionally NOT used; JWT stays httpOnly.
}

// ClearAuthCookie removes an auth cookie.
func ClearAuthCookie(c *gin.Context, name string) {
	secure := requestSecure(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", "", secure, true)
}