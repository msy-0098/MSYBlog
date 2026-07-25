package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GenerateTOTPSecret returns a base32 secret suitable for Google Authenticator.
func GenerateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func BuildOTPAuthURL(issuer, account, secret string) string {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = "MSYBlog"
	}
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	label := url.PathEscape(issuer + ":" + account)
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// ValidateTOTP checks a 6-digit code against the secret with ±1 step skew.
func ValidateTOTP(secret, code string, now time.Time) bool {
	secret = strings.ToUpper(strings.TrimSpace(secret))
	code = strings.TrimSpace(code)
	if secret == "" || len(code) != 6 {
		return false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		// try with padding
		key, err = base32.StdEncoding.DecodeString(secret)
		if err != nil {
			return false
		}
	}
	counter := now.Unix() / 30
	for _, delta := range []int64{-1, 0, 1} {
		if hotp(key, uint64(counter+delta)) == code {
			return true
		}
	}
	return false
}

func hotp(key []byte, counter uint64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := int64(((int(sum[offset]) & 0x7f) << 24) |
		((int(sum[offset+1]) & 0xff) << 16) |
		((int(sum[offset+2]) & 0xff) << 8) |
		(int(sum[offset+3]) & 0xff))
	return fmt.Sprintf("%06d", value%1000000)
}
