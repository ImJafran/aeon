package agent

import (
	"context"
	"testing"
	"time"

	"github.com/jafran/aeon/internal/bus"
)

func TestApprovalGateApprove(t *testing.T) {
	msgBus := bus.New(64)
	outCh := msgBus.Subscribe()
	gate := NewApprovalGate(msgBus, 5*time.Second)

	ctx := context.Background()

	// Start approval request in background
	result := make(chan bool, 1)
	go func() {
		approved, err := gate.RequestApproval(ctx, "test", "1", "dangerous command")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		result <- approved
	}()

	// Wait for approval message to be sent
	select {
	case msg := <-outCh:
		if msg.Content == "" {
			t.Error("expected approval message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for approval message")
	}

	// Approve
	handled := gate.HandleApprovalCommand("/approve")
	if !handled {
		t.Error("expected command to be handled")
	}

	select {
	case approved := <-result:
		if !approved {
			t.Error("expected approval to be true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestApprovalGateDeny(t *testing.T) {
	msgBus := bus.New(64)
	msgBus.Subscribe() // drain output
	gate := NewApprovalGate(msgBus, 5*time.Second)

	ctx := context.Background()

	result := make(chan bool, 1)
	go func() {
		approved, _ := gate.RequestApproval(ctx, "test", "1", "test")
		result <- approved
	}()

	time.Sleep(50 * time.Millisecond) // let goroutine start
	gate.HandleApprovalCommand("/deny")

	select {
	case approved := <-result:
		if approved {
			t.Error("expected denial")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestApprovalGateTimeout(t *testing.T) {
	msgBus := bus.New(64)
	msgBus.Subscribe()
	gate := NewApprovalGate(msgBus, 100*time.Millisecond)

	ctx := context.Background()
	_, err := gate.RequestApproval(ctx, "test", "1", "test")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestApprovalGateNoPending(t *testing.T) {
	msgBus := bus.New(64)
	gate := NewApprovalGate(msgBus, 5*time.Second)

	handled := gate.HandleApprovalCommand("/approve")
	if handled {
		t.Error("should not handle when no pending approvals")
	}
}
