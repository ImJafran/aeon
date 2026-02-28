package agent

import (
	"fmt"
	"sync"

	"github.com/jafran/aeon/internal/providers"
)

// CostTracker records token usage across provider calls.
type CostTracker struct {
	mu           sync.Mutex
	inputTokens  int
	outputTokens int
	requests     int
	perProvider  map[string]*providerUsage
}

type providerUsage struct {
	inputTokens  int
	outputTokens int
	requests     int
}

func NewCostTracker() *CostTracker {
	return &CostTracker{
		perProvider: make(map[string]*providerUsage),
	}
}

// Record adds a provider response's token usage to the tracker.
func (ct *CostTracker) Record(usage providers.TokenUsage, providerName string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.inputTokens += usage.InputTokens
	ct.outputTokens += usage.OutputTokens
	ct.requests++

	pu, ok := ct.perProvider[providerName]
	if !ok {
		pu = &providerUsage{}
		ct.perProvider[providerName] = pu
	}
	pu.inputTokens += usage.InputTokens
	pu.outputTokens += usage.OutputTokens
	pu.requests++
}

// Summary returns a formatted string of the current session's token usage.
func (ct *CostTracker) Summary() string {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	total := ct.inputTokens + ct.outputTokens
	s := fmt.Sprintf("Session Token Usage:\n  Total: %d tokens (%d in / %d out)\n  Requests: %d",
		total, ct.inputTokens, ct.outputTokens, ct.requests)

	if len(ct.perProvider) > 1 {
		s += "\n  Per provider:"
		for name, pu := range ct.perProvider {
			s += fmt.Sprintf("\n    %s: %d tokens (%d in / %d out), %d requests",
				name, pu.inputTokens+pu.outputTokens, pu.inputTokens, pu.outputTokens, pu.requests)
		}
	}

	return s
}

// Reset clears all tracked usage.
func (ct *CostTracker) Reset() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.inputTokens = 0
	ct.outputTokens = 0
	ct.requests = 0
	ct.perProvider = make(map[string]*providerUsage)
}
