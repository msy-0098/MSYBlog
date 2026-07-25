package security

import "testing"

func TestIsSuspiciousPath(t *testing.T) {
	for _, path := range []string{"/.env", "/wp-admin/install.php", "/phpmyadmin", "/../../etc/passwd"} {
		if !IsSuspiciousPath(path) {
			t.Fatalf("expected %q to be suspicious", path)
		}
	}
	if IsSuspiciousPath("/api/posts") {
		t.Fatal("normal API path must not be suspicious")
	}
}

func TestShouldAutoBanIgnoresClientAuthFailures(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 422, 429} {
		if CountsTowardAutoBan(status) {
			t.Fatalf("status %d must not count toward auto-ban", status)
		}
		if ShouldAutoBan(100, status) {
			t.Fatalf("status %d must never auto-ban", status)
		}
	}
}

func TestShouldAutoBanAfterServerErrorBurst(t *testing.T) {
	if ShouldAutoBan(29, 500) {
		t.Fatal("29 server errors should not trigger a ban")
	}
	if !ShouldAutoBan(30, 500) {
		t.Fatal("30 server errors should trigger a ban")
	}
	if ShouldAutoBan(100, 200) {
		t.Fatal("successful requests should not trigger a failure ban")
	}
}