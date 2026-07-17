package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterAllowWithinWindow(t *testing.T) {
	rl := NewRateLimiter()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }

	if !rl.Allow("k", 2, time.Minute) {
		t.Fatal("first request should be allowed")
	}
	if !rl.Allow("k", 2, time.Minute) {
		t.Fatal("second request should be allowed")
	}
	if rl.Allow("k", 2, time.Minute) {
		t.Fatal("third request should be blocked")
	}

	now = now.Add(61 * time.Second)
	if !rl.Allow("k", 2, time.Minute) {
		t.Fatal("request after window should be allowed")
	}
}

func TestRateLimiterMiddlewareReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter()
	engine := gin.New()
	engine.POST("/login", rl.Limit(1, time.Minute), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	first := httptest.NewRequest(http.MethodPost, "/login", nil)
	first.RemoteAddr = "203.0.113.10:1234"
	firstRec := httptest.NewRecorder()
	engine.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d", firstRec.Code)
	}

	second := httptest.NewRequest(http.MethodPost, "/login", nil)
	second.RemoteAddr = "203.0.113.10:1234"
	secondRec := httptest.NewRecorder()
	engine.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second status 429, got %d body %s", secondRec.Code, secondRec.Body.String())
	}
}