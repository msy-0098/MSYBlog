package service

import (
	"sync"
	"testing"
	"time"
)

func TestParseCodePurposeAcceptsOnlyTrimmedExactValues(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "register", raw: "  register\t", want: "register"},
		{name: "reset", raw: "\nreset ", want: "reset"},
		{name: "uppercase", raw: "Register", wantErr: true},
		{name: "unknown", raw: "login", wantErr: true},
		{name: "empty", raw: "   ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCodePurpose(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCodePurpose(%q) expected error, got %q", tt.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCodePurpose(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseCodePurpose(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestVerificationCodeLimiterNormalizesEmailAndIsolatesKeys(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })

	reservation, retryAfter := limiter.Reserve("  Reader@Example.COM ", "register")
	if retryAfter != 0 || reservation == nil {
		t.Fatalf("initial reserve = (%v, %v), want reservation with no retry", reservation, retryAfter)
	}
	reservation.Commit()

	if got := limiter.RetryAfter("reader@example.com", "register"); got != time.Minute {
		t.Fatalf("normalized email retry = %v, want %v", got, time.Minute)
	}
	if got := limiter.RetryAfter("reader@example.com", "reset"); got != 0 {
		t.Fatalf("different purpose retry = %v, want 0", got)
	}
	if got := limiter.RetryAfter("other@example.com", "register"); got != 0 {
		t.Fatalf("different email retry = %v, want 0", got)
	}
}

func TestVerificationCodeLimiterExpiresAndCleansEntry(t *testing.T) {
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	now := base
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })
	reservation, retryAfter := limiter.Reserve("reader@example.com", "register")
	if retryAfter != 0 || reservation == nil {
		t.Fatalf("initial reserve = (%v, %v), want reservation with no retry", reservation, retryAfter)
	}
	reservation.Commit()

	now = base.Add(61 * time.Second)
	if got := limiter.RetryAfter("reader@example.com", "register"); got != 0 {
		t.Fatalf("expired retry = %v, want 0", got)
	}

	// Moving the fake clock backwards proves the expired entry was removed,
	// rather than merely treated as expired for the prior call.
	now = base.Add(30 * time.Second)
	if got := limiter.RetryAfter("reader@example.com", "register"); got != 0 {
		t.Fatalf("cleaned entry retry = %v, want 0", got)
	}
}

func TestVerificationCodeLimiterConcurrentReservationsAllowOnlyOne(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })

	var wg sync.WaitGroup
	var mu sync.Mutex
	var reservations []*VerificationCodeReservation
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, _ := limiter.Reserve(" Reader@Example.COM ", "register")
			if reservation != nil {
				mu.Lock()
				reservations = append(reservations, reservation)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(reservations) != 1 {
		t.Fatalf("successful concurrent reservations = %d, want 1", len(reservations))
	}
	reservations[0].Commit()
	if got := limiter.RetryAfter("reader@example.com", "register"); got != time.Minute {
		t.Fatalf("retry after concurrent access = %v, want %v", got, time.Minute)
	}
}

func TestVerificationCodeLimiterPrunesExpiredEntriesOnUnrelatedReserve(t *testing.T) {
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	now := base
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })
	limiter.maxEntries = 2

	first, _ := limiter.Reserve("first@example.com", "register")
	second, _ := limiter.Reserve("second@example.com", "register")
	first.Commit()
	second.Commit()

	now = base.Add(time.Minute + time.Second)
	third, retryAfter := limiter.Reserve("third@example.com", "register")
	if retryAfter != 0 || third == nil {
		t.Fatalf("reserve after expiry = (%v, %v), want reservation with no retry", third, retryAfter)
	}
	if got := len(limiter.entries); got != 1 {
		t.Fatalf("entries after unrelated prune = %d, want 1", got)
	}
}

func TestVerificationCodeLimiterCapacityDoesNotEvictInflightReservation(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })
	limiter.maxEntries = 1

	first, retryAfter := limiter.Reserve("first@example.com", "register")
	if retryAfter != 0 || first == nil {
		t.Fatalf("initial reserve = (%v, %v), want reservation with no retry", first, retryAfter)
	}
	second, retryAfter := limiter.Reserve("second@example.com", "register")
	if second != nil || retryAfter <= 0 {
		t.Fatalf("reserve at capacity = (%v, %v), want rejection with retry", second, retryAfter)
	}
	if got := limiter.RetryAfter("first@example.com", "register"); got <= 0 {
		t.Fatalf("in-flight reservation was evicted, retryAfter = %v", got)
	}

	first.Rollback()
	second, retryAfter = limiter.Reserve("second@example.com", "register")
	if retryAfter != 0 || second == nil {
		t.Fatalf("reserve after rollback = (%v, %v), want reservation with no retry", second, retryAfter)
	}
}

func TestVerificationCodeLimiterCapacityRejectsUntilCommittedEntryExpires(t *testing.T) {
	base := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	now := base
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })
	limiter.maxEntries = 1

	first, _ := limiter.Reserve("first@example.com", "register")
	first.Commit()
	second, retryAfter := limiter.Reserve("second@example.com", "register")
	if second != nil || retryAfter != time.Minute {
		t.Fatalf("reserve at committed capacity = (%v, %v), want nil and %v", second, retryAfter, time.Minute)
	}

	now = base.Add(time.Minute + time.Second)
	second, retryAfter = limiter.Reserve("second@example.com", "register")
	if retryAfter != 0 || second == nil {
		t.Fatalf("reserve after capacity expiry = (%v, %v), want reservation with no retry", second, retryAfter)
	}
}
