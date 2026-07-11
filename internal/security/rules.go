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

func ShouldAutoBan(failureCount int64, status int) bool {
	return status >= 400 && failureCount >= 30
}
