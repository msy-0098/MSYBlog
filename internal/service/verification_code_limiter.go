package service

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var errInvalidCodePurpose = errors.New("invalid verification code purpose")

const defaultVerificationCodeLimiterMaxEntries = 10000

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

type verificationCodeLimitEntry struct {
	reserved      bool
	reservationID uint64
	expiresAt     time.Time
}

// VerificationCodeReservation holds one in-flight send slot.
type VerificationCodeReservation struct {
	limiter *VerificationCodeLimiter
	key     string
	id      uint64
}

// VerificationCodeLimiter tracks in-flight and successful verification-code sends in memory.
type VerificationCodeLimiter struct {
	mu                sync.Mutex
	cooldown          time.Duration
	now               func() time.Time
	entries           map[string]verificationCodeLimitEntry
	maxEntries        int
	nextReservationID uint64
}

func NewVerificationCodeLimiter(cooldown time.Duration, now func() time.Time) *VerificationCodeLimiter {
	if now == nil {
		now = time.Now
	}
	return &VerificationCodeLimiter{
		cooldown:   cooldown,
		now:        now,
		entries:    make(map[string]verificationCodeLimitEntry),
		maxEntries: defaultVerificationCodeLimiterMaxEntries,
	}
}

// Reserve atomically claims a send slot. A nil reservation means the caller must retry later.
func (l *VerificationCodeLimiter) Reserve(email string, purpose string) (*VerificationCodeReservation, time.Duration) {
	if l == nil || l.cooldown <= 0 {
		return &VerificationCodeReservation{}, 0
	}

	key := verificationCodeLimitKey(email, purpose)
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneExpiredLocked(now)
	if entry, ok := l.entries[key]; ok {
		if entry.reserved {
			return nil, l.cooldown
		}
		return nil, entry.expiresAt.Sub(now)
	}

	if len(l.entries) >= l.maxEntries {
		return nil, l.capacityRetryAfterLocked(now)
	}

	l.nextReservationID++
	reservation := &VerificationCodeReservation{limiter: l, key: key, id: l.nextReservationID}
	l.entries[key] = verificationCodeLimitEntry{reserved: true, reservationID: reservation.id}
	return reservation, 0
}

func (l *VerificationCodeLimiter) RetryAfter(email string, purpose string) time.Duration {
	if l == nil || l.cooldown <= 0 {
		return 0
	}

	key := verificationCodeLimitKey(email, purpose)
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneExpiredLocked(now)
	entry, ok := l.entries[key]
	if !ok {
		return 0
	}
	if entry.reserved {
		return l.cooldown
	}
	return entry.expiresAt.Sub(now)
}

// Commit starts the full cooldown from the successful completion time.
func (r *VerificationCodeReservation) Commit() {
	if r == nil || r.limiter == nil {
		return
	}
	r.limiter.finishReservation(r, true)
}

// Rollback releases a failed in-flight send so the caller can retry immediately.
func (r *VerificationCodeReservation) Rollback() {
	if r == nil || r.limiter == nil {
		return
	}
	r.limiter.finishReservation(r, false)
}

func (l *VerificationCodeLimiter) finishReservation(reservation *VerificationCodeReservation, commit bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[reservation.key]
	if !ok || !entry.reserved || entry.reservationID != reservation.id {
		return
	}
	if !commit {
		delete(l.entries, reservation.key)
		return
	}

	entry.reserved = false
	entry.expiresAt = l.now().Add(l.cooldown)
	l.entries[reservation.key] = entry
}

func (l *VerificationCodeLimiter) pruneExpiredLocked(now time.Time) {
	for key, entry := range l.entries {
		if !entry.reserved && !entry.expiresAt.After(now) {
			delete(l.entries, key)
		}
	}
}

func (l *VerificationCodeLimiter) capacityRetryAfterLocked(now time.Time) time.Duration {
	var earliestExpiry time.Time
	for _, entry := range l.entries {
		if entry.reserved {
			continue
		}
		if earliestExpiry.IsZero() || entry.expiresAt.Before(earliestExpiry) {
			earliestExpiry = entry.expiresAt
		}
	}
	if !earliestExpiry.IsZero() {
		return earliestExpiry.Sub(now)
	}
	return l.cooldown
}

func verificationCodeLimitKey(email string, purpose string) string {
	return strings.ToLower(strings.TrimSpace(email)) + "\x00" + purpose
}
