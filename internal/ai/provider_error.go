package ai

import "errors"

type ProviderErrorKind string

const (
	ProviderErrorConfig      ProviderErrorKind = "config"
	ProviderErrorRateLimit   ProviderErrorKind = "rate_limit"
	ProviderErrorTimeout     ProviderErrorKind = "timeout"
	ProviderErrorUnavailable ProviderErrorKind = "unavailable"
)

type ProviderError struct {
	Kind  ProviderErrorKind
	cause error
}

func (e *ProviderError) Error() string {
	if e == nil || e.Kind == "" {
		return "ai provider error"
	}
	return "ai provider error: " + string(e.Kind)
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func providerError(kind ProviderErrorKind, cause error) error {
	if cause == nil {
		cause = errors.New("provider failure")
	}
	return &ProviderError{Kind: kind, cause: cause}
}

func providerErrorKind(err error) ProviderErrorKind {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Kind
	}
	return ""
}
