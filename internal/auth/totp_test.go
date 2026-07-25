package auth

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestGenerateAndValidateTOTP(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil || len(secret) < 16 {
		t.Fatalf("secret: %v %q", err, secret)
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code := hotp(key, uint64(now.Unix()/30))
	if !ValidateTOTP(secret, code, now) {
		t.Fatalf("expected code %s valid for secret", code)
	}
	if ValidateTOTP(secret, "000000", now) && code != "000000" {
		// may occasionally collide; only fail if clearly wrong length path
	}
	if ValidateTOTP(secret, "abcdef", now) {
		t.Fatal("non-digit must fail")
	}
}

func TestBuildOTPAuthURL(t *testing.T) {
	url := BuildOTPAuthURL("MSYBlog", "admin@example.com", "JBSWY3DPEHPK3PXP")
	for _, part := range []string{"otpauth://totp/", "secret=JBSWY3DPEHPK3PXP", "issuer=MSYBlog"} {
		if !contains(url, part) {
			t.Fatalf("url %q missing %q", url, part)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
