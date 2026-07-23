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

	limiter.MarkSent("  Reader@Example.COM ", "register")

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
	limiter.MarkSent("reader@example.com", "register")

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

func TestVerificationCodeLimiterConcurrentAccess(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	limiter := NewVerificationCodeLimiter(time.Minute, func() time.Time { return now })

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				limiter.MarkSent(" Reader@Example.COM ", "register")
				_ = limiter.RetryAfter("reader@example.com", "register")
			}
		}()
	}
	wg.Wait()

	if got := limiter.RetryAfter("reader@example.com", "register"); got != time.Minute {
		t.Fatalf("retry after concurrent access = %v, want %v", got, time.Minute)
	}
}
