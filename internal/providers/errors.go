package providers

import "strings"

// FailoverReason categorizes why a provider failed.
type FailoverReason int

const (
	ReasonUnknown     FailoverReason = iota
	ReasonAuth                       // 401/403 — invalid key
	ReasonRateLimit                  // 429 — rate limited
	ReasonBilling                    // 402 — billing issue
	ReasonTimeout                    // context deadline / network timeout
	ReasonFormat                     // bad request / invalid input
	ReasonOverloaded                 // 529 / 503 — server overloaded
	ReasonServerError                // 500+ — transient server error
)

func (r FailoverReason) String() string {
	switch r {
	case ReasonAuth:
		return "auth"
	case ReasonRateLimit:
		return "rate_limit"
	case ReasonBilling:
		return "billing"
	case ReasonTimeout:
		return "timeout"
	case ReasonFormat:
		return "format"
	case ReasonOverloaded:
		return "overloaded"
	case ReasonServerError:
		return "server_error"
	default:
		return "unknown"
	}
}

// Retriable returns true if the error is transient and worth retrying with another provider.
func (r FailoverReason) Retriable() bool {
	switch r {
	case ReasonRateLimit, ReasonTimeout, ReasonOverloaded, ReasonServerError, ReasonUnknown:
		return true
	default:
		return false
	}
}

// ClassifyError pattern-matches on error strings and HTTP status codes to determine the failure reason.
func ClassifyError(err error) FailoverReason {
	if err == nil {
		return ReasonUnknown
	}
	msg := err.Error()

	// Check for status codes in error messages
	if containsAny(msg, "status 401", "status 403", "unauthorized", "forbidden", "invalid.*api.*key") {
		return ReasonAuth
	}
	if containsAny(msg, "status 402", "billing", "payment required") {
		return ReasonBilling
	}
	if containsAny(msg, "status 429", "rate limit", "too many requests", "quota exceeded") {
		return ReasonRateLimit
	}
	if containsAny(msg, "status 400", "bad request", "invalid request", "malformed") {
		return ReasonFormat
	}
	if containsAny(msg, "status 529", "status 503", "overloaded", "service unavailable", "capacity") {
		return ReasonOverloaded
	}
	if containsAny(msg, "status 500", "status 502", "status 504", "internal server error", "bad gateway") {
		return ReasonServerError
	}
	if containsAny(msg, "timeout", "deadline exceeded", "context canceled", "connection refused") {
		return ReasonTimeout
	}

	return ReasonUnknown
}

func containsAny(s string, patterns ...string) bool {
	lower := strings.ToLower(s)
	for _, p := range patterns {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
