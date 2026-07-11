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

func TestShouldAutoBanAfterFailureBurst(t *testing.T) {
	if ShouldAutoBan(29, 404) {
		t.Fatal("29 failures should not trigger a ban")
	}
	if !ShouldAutoBan(30, 404) {
		t.Fatal("30 failures should trigger a ban")
	}
	if ShouldAutoBan(100, 200) {
		t.Fatal("successful requests should not trigger a failure ban")
	}
}
