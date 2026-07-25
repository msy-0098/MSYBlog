package security

import (
	"testing"
	"time"
)

func TestAccountLockLocksAfterMaxFails(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	lock := NewAccountLock(3, time.Minute, 10*time.Minute).WithClock(func() time.Time { return now })

	if d := lock.Check("admin"); d != 0 {
		t.Fatalf("expected unlocked, got %v", d)
	}
	if d := lock.Fail("admin"); d != 0 {
		t.Fatalf("fail1 should not lock, got %v", d)
	}
	if d := lock.Fail("admin"); d != 0 {
		t.Fatalf("fail2 should not lock, got %v", d)
	}
	if d := lock.Fail("admin"); d != 10*time.Minute {
		t.Fatalf("fail3 should lock 10m, got %v", d)
	}
	if d := lock.Check("admin"); d != 10*time.Minute {
		t.Fatalf("check should report lock, got %v", d)
	}
	if d := lock.Fail("other"); d != 0 {
		t.Fatalf("other account should not lock, got %v", d)
	}
	lock.Success("admin")
	if d := lock.Check("admin"); d != 0 {
		t.Fatalf("success should clear lock, got %v", d)
	}
}