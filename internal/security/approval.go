package security

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ApprovalRequest struct {
	ID        string
	Command   string
	Reason    string
	CreatedAt time.Time
	Response  chan bool
}

type ApprovalQueue struct {
	pending map[string]*ApprovalRequest
	timeout time.Duration
	mu      sync.Mutex
}

func NewApprovalQueue(timeout time.Duration) *ApprovalQueue {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &ApprovalQueue{
		pending: make(map[string]*ApprovalRequest),
		timeout: timeout,
	}
}

// RequestApproval blocks until the user approves/denies or timeout.
func (q *ApprovalQueue) RequestApproval(ctx context.Context, command, reason string) (bool, error) {
	id := fmt.Sprintf("approval_%d", time.Now().UnixNano())
	req := &ApprovalRequest{
		ID:        id,
		Command:   command,
		Reason:    reason,
		CreatedAt: time.Now(),
		Response:  make(chan bool, 1),
	}

	q.mu.Lock()
	q.pending[id] = req
	q.mu.Unlock()

	defer func() {
		q.mu.Lock()
		delete(q.pending, id)
		q.mu.Unlock()
	}()

	timer := time.NewTimer(q.timeout)
	defer timer.Stop()

	select {
	case approved := <-req.Response:
		return approved, nil
	case <-timer.C:
		return false, fmt.Errorf("approval timed out after %v", q.timeout)
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Approve resolves a pending approval as approved.
func (q *ApprovalQueue) Approve(id string) bool {
	q.mu.Lock()
	req, ok := q.pending[id]
	q.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case req.Response <- true:
		return true
	default:
		return false
	}
}

// Deny resolves a pending approval as denied.
func (q *ApprovalQueue) Deny(id string) bool {
	q.mu.Lock()
	req, ok := q.pending[id]
	q.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case req.Response <- false:
		return true
	default:
		return false
	}
}

// Pending returns all pending approval requests.
func (q *ApprovalQueue) Pending() []*ApprovalRequest {
	q.mu.Lock()
	defer q.mu.Unlock()

	result := make([]*ApprovalRequest, 0, len(q.pending))
	for _, req := range q.pending {
		result = append(result, req)
	}
	return result
}
