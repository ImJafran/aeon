package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jafran/aeon/internal/bus"
)

// ApprovalGate handles human-in-the-loop approval for dangerous tool operations.
type ApprovalGate struct {
	bus     *bus.MessageBus
	timeout time.Duration
	mu      sync.Mutex
	pending map[string]chan bool
}

func NewApprovalGate(b *bus.MessageBus, timeout time.Duration) *ApprovalGate {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &ApprovalGate{
		bus:     b,
		timeout: timeout,
		pending: make(map[string]chan bool),
	}
}

// RequestApproval sends an approval request to the user and waits for /approve or /deny.
func (g *ApprovalGate) RequestApproval(ctx context.Context, channel, chatID, description string) (bool, error) {
	id := fmt.Sprintf("approval_%d", time.Now().UnixNano())
	ch := make(chan bool, 1)

	g.mu.Lock()
	g.pending[id] = ch
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.pending, id)
		g.mu.Unlock()
	}()

	// Send approval request to user
	g.bus.Send(bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: fmt.Sprintf("⚠️ Approval required:\n%s\n\nReply /approve or /deny", description),
	})

	timer := time.NewTimer(g.timeout)
	defer timer.Stop()

	select {
	case approved := <-ch:
		return approved, nil
	case <-timer.C:
		return false, fmt.Errorf("approval timed out after %v", g.timeout)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// HandleApprovalCommand processes /approve or /deny commands.
// Returns true if the command was handled (was an approval response).
func (g *ApprovalGate) HandleApprovalCommand(command string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.pending) == 0 {
		return false
	}

	cmd := strings.TrimSpace(strings.ToLower(command))
	approved := cmd == "/approve"
	denied := cmd == "/deny"

	if !approved && !denied {
		return false
	}

	// Resolve the most recent pending approval
	for id, ch := range g.pending {
		select {
		case ch <- approved:
		default:
		}
		delete(g.pending, id)
		return true
	}

	return false
}

// HasPending returns true if there are pending approval requests.
func (g *ApprovalGate) HasPending() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.pending) > 0
}
