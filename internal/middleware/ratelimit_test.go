package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterAllowReturnsDecisionAndRetryAfter(t *testing.T) {
	rl := NewRateLimiter()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }

	decision := rl.Allow("k", 2, time.Minute)
	if !decision.Allowed || decision.RetryAfter != 0 {
		t.Fatalf("expected first request allowed without retry delay, got %+v", decision)
	}

	now = now.Add(10 * time.Second)
	decision = rl.Allow("k", 2, time.Minute)
	if !decision.Allowed || decision.RetryAfter != 0 {
		t.Fatalf("expected second request allowed without retry delay, got %+v", decision)
	}

	now = now.Add(10 * time.Second)
	decision = rl.Allow("k", 2, time.Minute)
	if decision.Allowed {
		t.Fatalf("expected third request blocked, got %+v", decision)
	}
	if decision.RetryAfter != 40*time.Second {
		t.Fatalf("expected retry after 40s from earliest valid hit, got %s", decision.RetryAfter)
	}

	now = now.Add(40 * time.Second)
	decision = rl.Allow("k", 2, time.Minute)
	if !decision.Allowed || decision.RetryAfter != 0 {
		t.Fatalf("expected request at earliest hit expiry to be allowed, got %+v", decision)
	}
}

func TestRateLimiterAllowsDisabledLimitAndDifferentKeys(t *testing.T) {
	rl := NewRateLimiter()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }

	decision := rl.Allow("disabled", 0, time.Minute)
	if !decision.Allowed || decision.RetryAfter != 0 {
		t.Fatalf("expected disabled limiter to allow request, got %+v", decision)
	}

	if decision = rl.Allow("first-key", 1, time.Minute); !decision.Allowed {
		t.Fatalf("expected first key to be allowed, got %+v", decision)
	}
	if decision = rl.Allow("second-key", 1, time.Minute); !decision.Allowed {
		t.Fatalf("expected different key to have an independent window, got %+v", decision)
	}
}

func TestRateLimiterPrunesExpiredKeys(t *testing.T) {
	rl := NewRateLimiter()
	rl.maxKeys = 1
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }

	if decision := rl.Allow("expired", 1, time.Minute); !decision.Allowed {
		t.Fatalf("expected initial request allowed, got %+v", decision)
	}

	now = now.Add(61 * time.Second)
	if decision := rl.Allow("active", 1, time.Minute); !decision.Allowed {
		t.Fatalf("expected active request allowed, got %+v", decision)
	}

	if _, exists := rl.hits["expired"]; exists {
		t.Fatal("expected expired key to be pruned")
	}
	if _, exists := rl.hits["active"]; !exists {
		t.Fatal("expected active key to remain")
	}
}

func TestRateLimiterMiddlewareReturns429WithRoundedRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rl := NewRateLimiter()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }

	engine := gin.New()
	engine.POST("/login", rl.Limit(1, 1500*time.Millisecond), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	first := httptest.NewRequest(http.MethodPost, "/login", nil)
	first.RemoteAddr = "203.0.113.10:1234"
	firstRec := httptest.NewRecorder()
	engine.ServeHTTP(firstRec, first)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first status 200, got %d", firstRec.Code)
	}

	now = now.Add(100 * time.Millisecond)
	second := httptest.NewRequest(http.MethodPost, "/login", nil)
	second.RemoteAddr = "203.0.113.10:1234"
	secondRec := httptest.NewRecorder()
	engine.ServeHTTP(secondRec, second)
	if secondRec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second status 429, got %d body %s", secondRec.Code, secondRec.Body.String())
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			RetryAfter int `json:"retryAfter"`
		} `json:"data"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != http.StatusTooManyRequests {
		t.Fatalf("expected envelope code 429, got %d", body.Code)
	}
	if body.Message != "请求过于频繁，请稍后再试" {
		t.Fatalf("unexpected message %q", body.Message)
	}
	if body.Data.RetryAfter != 2 {
		t.Fatalf("expected retryAfter rounded up to 2 seconds, got %d", body.Data.RetryAfter)
	}
}
