package security

import (
	"sync"
	"time"
)

// AccountLock tracks failed credentials by account key (username/email).
// Process-local fixed-window counter with temporary lockout.
type AccountLock struct {
	mu       sync.Mutex
	maxFails int
	window   time.Duration
	lockFor  time.Duration
	now      func() time.Time
	entries  map[string]accountEntry
}

type accountEntry struct {
	fails       int
	windowStart time.Time
	lockedUntil time.Time
}

func NewAccountLock(maxFails int, window, lockFor time.Duration) *AccountLock {
	if maxFails <= 0 {
		maxFails = 5
	}
	if window <= 0 {
		window = 15 * time.Minute
	}
	if lockFor <= 0 {
		lockFor = 15 * time.Minute
	}
	return &AccountLock{
		maxFails: maxFails,
		window:   window,
		lockFor:  lockFor,
		now:      time.Now,
		entries:  make(map[string]accountEntry),
	}
}

func (l *AccountLock) WithClock(now func() time.Time) *AccountLock {
	if now != nil {
		l.now = now
	}
	return l
}

// Check returns remaining lock duration when the account is locked.
func (l *AccountLock) Check(key string) time.Duration {
	if l == nil || key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	item := l.entries[key]
	if item.lockedUntil.After(now) {
		return item.lockedUntil.Sub(now)
	}
	return 0
}

func (l *AccountLock) Fail(key string) time.Duration {
	if l == nil || key == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	item := l.entries[key]
	if item.lockedUntil.After(now) {
		return item.lockedUntil.Sub(now)
	}
	if item.windowStart.IsZero() || now.Sub(item.windowStart) >= l.window {
		item = accountEntry{fails: 1, windowStart: now}
	} else {
		item.fails++
	}
	if item.fails >= l.maxFails {
		item.lockedUntil = now.Add(l.lockFor)
		l.entries[key] = item
		return l.lockFor
	}
	l.entries[key] = item
	return 0
}

func (l *AccountLock) Success(key string) {
	if l == nil || key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}