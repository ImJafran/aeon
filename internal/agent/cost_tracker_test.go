package agent

import (
	"strings"
	"testing"

	"github.com/ImJafran/aeon/internal/providers"
)

func TestCostTracker(t *testing.T) {
	ct := NewCostTracker()

	ct.Record(providers.TokenUsage{InputTokens: 100, OutputTokens: 50}, "anthropic")
	ct.Record(providers.TokenUsage{InputTokens: 200, OutputTokens: 80}, "anthropic")
	ct.Record(providers.TokenUsage{InputTokens: 50, OutputTokens: 20}, "gemini")

	summary := ct.Summary()
	if !strings.Contains(summary, "350 in") {
		t.Errorf("expected 350 input tokens in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "150 out") {
		t.Errorf("expected 150 output tokens in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "Requests: 3") {
		t.Errorf("expected 3 requests in summary, got: %s", summary)
	}

	ct.Reset()
	summary = ct.Summary()
	if !strings.Contains(summary, "Total: 0") {
		t.Errorf("expected 0 tokens after reset, got: %s", summary)
	}
}
