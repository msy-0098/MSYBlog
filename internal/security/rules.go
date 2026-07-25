package security

import "strings"

func IsSuspiciousPath(path string) bool {
	path = strings.ToLower(path)
	for _, marker := range []string{"..", "/.env", "/wp-admin", "/wp-login", "phpmyadmin", "/vendor/phpunit", "/shell", "/cgi-bin"} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

// CountsTowardAutoBan reports whether a response should accumulate toward the
// short-window auto-ban. Auth mistakes, validation errors, conflicts, and rate
// limits must never ban real users who are retrying login/register.
func CountsTowardAutoBan(status int) bool {
	if status < 400 {
		return false
	}
	switch status {
	case 400, 401, 403, 404, 409, 422, 429:
		return false
	default:
		// Keep auto-ban for repeated 5xx and other hard failures.
		return status >= 500
	}
}

func ShouldAutoBan(failureCount int64, status int) bool {
	return CountsTowardAutoBan(status) && failureCount >= 30
}