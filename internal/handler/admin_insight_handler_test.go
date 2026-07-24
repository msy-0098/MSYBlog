package handler

import (
	"errors"
	"strings"
	"testing"

	"masenyu.top/blog/backend/internal/ai"
)

func TestAIErrorMessageRecognizesProviderConfigurationError(t *testing.T) {
	configuredMessage := aiErrorMessage(errors.New("provider is not configured"))
	if !strings.Contains(configuredMessage, "BLOG_AI_API_KEY") {
		t.Fatalf("legacy configured message must provide remediation, got %q", configuredMessage)
	}

	if got := aiErrorMessage(&ai.ProviderError{Kind: ai.ProviderErrorConfig}); got != configuredMessage {
		t.Fatalf("provider configuration error message = %q, want %q", got, configuredMessage)
	}

	if got := aiErrorMessage(&ai.ProviderError{Kind: ai.ProviderErrorUnavailable}); strings.Contains(got, "BLOG_AI_API_KEY") {
		t.Fatalf("unavailable provider error must not include configuration guidance: %q", got)
	}
}
