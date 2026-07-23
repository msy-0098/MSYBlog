package middleware

import (
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/response"
)

// LimitDecision describes whether a request may proceed and, when rejected,
// how long remains until the earliest effective hit leaves the window.
type LimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// RateLimiter is a process-local fixed-window limiter keyed by caller + route.
// Good enough for a single-instance blog service; not shared across processes.
type RateLimiter struct {
	mu      sync.Mutex
	hits    map[string][]time.Time
	now     func() time.Time
	maxKeys int
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		hits:    make(map[string][]time.Time),
		now:     time.Now,
		maxKeys: 10000,
	}
}

func (rl *RateLimiter) Allow(key string, limit int, window time.Duration) LimitDecision {
	if limit <= 0 || window <= 0 {
		return LimitDecision{Allowed: true}
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()
	cutoff := now.Add(-window)
	valid := rl.hits[key][:0]
	var earliest time.Time
	hasEarliest := false
	for _, at := range rl.hits[key] {
		if at.After(cutoff) {
			valid = append(valid, at)
			if !hasEarliest || at.Before(earliest) {
				earliest = at
				hasEarliest = true
			}
		}
	}

	if len(valid) >= limit {
		rl.hits[key] = valid
		return LimitDecision{
			Allowed:    false,
			RetryAfter: earliest.Add(window).Sub(now),
		}
	}

	rl.hits[key] = append(valid, now)
	if len(rl.hits) > rl.maxKeys {
		rl.pruneLocked(cutoff)
	}
	return LimitDecision{Allowed: true}
}

func (rl *RateLimiter) pruneLocked(cutoff time.Time) {
	for key, times := range rl.hits {
		valid := times[:0]
		for _, at := range times {
			if at.After(cutoff) {
				valid = append(valid, at)
			}
		}
		if len(valid) == 0 {
			delete(rl.hits, key)
			continue
		}
		rl.hits[key] = valid
	}
}

// Limit returns a middleware that rate-limits by client IP and request path.
func (rl *RateLimiter) Limit(limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := ClientIP(c) + "|" + c.Request.Method + "|" + c.FullPath()
		if c.FullPath() == "" {
			key = ClientIP(c) + "|" + c.Request.Method + "|" + c.Request.URL.Path
		}

		decision := rl.Allow(key, limit, window)
		if !decision.Allowed {
			retryAfter := int(math.Ceil(decision.RetryAfter.Seconds()))
			if retryAfter < 1 {
				retryAfter = 1
			}
			response.ErrorWithData(
				c,
				http.StatusTooManyRequests,
				"请求过于频繁，请稍后再试",
				gin.H{"retryAfter": retryAfter},
			)
			c.Abort()
			return
		}

		c.Next()
	}
}
