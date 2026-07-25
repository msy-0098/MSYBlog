package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/response"
)

const (
	CSRFCookieName   = "csrf_token"
	CSRFHeaderName   = "X-CSRF-Token"
	CSRFCookieMaxAge = AuthCookieMaxAge
)

// IssueCSRFToken sets a readable CSRF cookie for double-submit protection.
func IssueCSRFToken(c *gin.Context) string {
	token := newCSRFToken()
	secure := requestSecure(c)
	// Readable by JS so SPA can mirror it into X-CSRF-Token.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CSRFCookieName, token, CSRFCookieMaxAge, "/", "", secure, false)
	return token
}

func ClearCSRFToken(c *gin.Context) {
	secure := requestSecure(c)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(CSRFCookieName, "", -1, "/", "", secure, false)
}

// RequireCSRF enforces double-submit CSRF for cookie-authenticated mutating requests.
// Bearer-token clients (tests / non-browser tools) are exempt.
func RequireCSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}
		// Bearer auth is not vulnerable to classic browser CSRF.
		if strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
			c.Next()
			return
		}
		cookie, err := c.Cookie(CSRFCookieName)
		header := strings.TrimSpace(c.GetHeader(CSRFHeaderName))
		if err != nil || strings.TrimSpace(cookie) == "" || header == "" || cookie != header {
			response.Error(c, http.StatusForbidden, 403, "CSRF 校验失败")
			c.Abort()
			return
		}
		c.Next()
	}
}

func newCSRFToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte("csrf-fallback-token-please-rotate"))
	}
	return hex.EncodeToString(buf)
}