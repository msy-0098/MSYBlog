package middleware

import (
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"masenyu.top/blog/backend/internal/response"
)

const rateLimiterPruneInterval = time.Second

// LimitDecision describes whether a request may proceed and, when rejected,
// how long remains until the earliest effective hit leaves the window.
type LimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// RateLimiter is a process-local fixed-window limiter keyed by caller + route.
// Good enough for a single-instance blog service; not shared across processes.
type RateLimiter struct {
	mu        sync.Mutex
	hits      map[string][]time.Time
	now       func() time.Time
	maxKeys   int
	nextPrune time.Time
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
	if _, exists := rl.hits[key]; !exists && len(rl.hits) >= rl.maxKeys {
		if !now.Before(rl.nextPrune) {
			rl.pruneLocked(now)
			rl.nextPrune = now.Add(rateLimiterPruneInterval)
		}
		if len(rl.hits) >= rl.maxKeys {
			retryAfter := rl.nextPrune.Sub(now)
			if retryAfter <= 0 {
				retryAfter = time.Nanosecond
			}
			return LimitDecision{RetryAfter: retryAfter}
		}
	}

	valid := rl.hits[key][:0]
	var earliest time.Time
	hasEarliest := false
	for _, expiresAt := range rl.hits[key] {
		if expiresAt.After(now) {
			valid = append(valid, expiresAt)
			if !hasEarliest || expiresAt.Before(earliest) {
				earliest = expiresAt
				hasEarliest = true
			}
		}
	}

	if len(valid) >= limit {
		rl.hits[key] = valid
		return LimitDecision{
			Allowed:    false,
			RetryAfter: earliest.Sub(now),
		}
	}

	rl.hits[key] = append(valid, now.Add(window))
	return LimitDecision{Allowed: true}
}

func (rl *RateLimiter) pruneLocked(now time.Time) {
	for key, expirations := range rl.hits {
		valid := expirations[:0]
		for _, expiresAt := range expirations {
			if expiresAt.After(now) {
				valid = append(valid, expiresAt)
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
