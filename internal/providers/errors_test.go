package providers

import (
	"fmt"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err    error
		expect FailoverReason
	}{
		{fmt.Errorf("API error (status 401): unauthorized"), ReasonAuth},
		{fmt.Errorf("API error (status 403): forbidden"), ReasonAuth},
		{fmt.Errorf("API error (status 429): rate limit exceeded"), ReasonRateLimit},
		{fmt.Errorf("API error (status 402): payment required"), ReasonBilling},
		{fmt.Errorf("API error (status 400): bad request"), ReasonFormat},
		{fmt.Errorf("API error (status 529): overloaded"), ReasonOverloaded},
		{fmt.Errorf("API error (status 503): service unavailable"), ReasonOverloaded},
		{fmt.Errorf("API error (status 500): internal server error"), ReasonServerError},
		{fmt.Errorf("context deadline exceeded"), ReasonTimeout},
		{fmt.Errorf("connection refused"), ReasonTimeout},
		{fmt.Errorf("something random"), ReasonUnknown},
		{nil, ReasonUnknown},
	}

	for _, tt := range tests {
		got := ClassifyError(tt.err)
		if got != tt.expect {
			errStr := "<nil>"
			if tt.err != nil {
				errStr = tt.err.Error()
			}
			t.Errorf("ClassifyError(%q) = %s, want %s", errStr, got, tt.expect)
		}
	}
}

func TestRetriable(t *testing.T) {
	retriable := []FailoverReason{ReasonRateLimit, ReasonTimeout, ReasonOverloaded, ReasonServerError, ReasonUnknown}
	nonRetriable := []FailoverReason{ReasonAuth, ReasonBilling, ReasonFormat}

	for _, r := range retriable {
		if !r.Retriable() {
			t.Errorf("%s should be retriable", r)
		}
	}
	for _, r := range nonRetriable {
		if r.Retriable() {
			t.Errorf("%s should not be retriable", r)
		}
	}
}
