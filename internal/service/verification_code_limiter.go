package service

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var errInvalidCodePurpose = errors.New("invalid verification code purpose")

// ParseCodePurpose accepts only the public verification-code purposes.
func ParseCodePurpose(raw string) (string, error) {
	purpose := strings.TrimSpace(raw)
	switch purpose {
	case "register", "reset":
		return purpose, nil
	default:
		return "", errInvalidCodePurpose
	}
}

// VerificationCodeLimiter tracks successful verification-code sends in memory.
type VerificationCodeLimiter struct {
	mu       sync.Mutex
	cooldown time.Duration
	now      func() time.Time
	sentAt   map[string]time.Time
}

func NewVerificationCodeLimiter(cooldown time.Duration, now func() time.Time) *VerificationCodeLimiter {
	if now == nil {
		now = time.Now
	}
	return &VerificationCodeLimiter{
		cooldown: cooldown,
		now:      now,
		sentAt:   make(map[string]time.Time),
	}
}

func (l *VerificationCodeLimiter) MarkSent(email string, purpose string) {
	if l == nil || l.cooldown <= 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.sentAt[verificationCodeLimitKey(email, purpose)] = l.now()
}

func (l *VerificationCodeLimiter) RetryAfter(email string, purpose string) time.Duration {
	if l == nil || l.cooldown <= 0 {
		return 0
	}

	key := verificationCodeLimitKey(email, purpose)
	l.mu.Lock()
	defer l.mu.Unlock()

	sentAt, ok := l.sentAt[key]
	if !ok {
		return 0
	}

	remaining := sentAt.Add(l.cooldown).Sub(l.now())
	if remaining <= 0 {
		delete(l.sentAt, key)
		return 0
	}
	return remaining
}

func verificationCodeLimitKey(email string, purpose string) string {
	return strings.ToLower(strings.TrimSpace(email)) + "\x00" + purpose
}
