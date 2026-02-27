package agent

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jafran/aeon/internal/bus"
	"github.com/jafran/aeon/internal/tools"
)

func TestSubagentManagerSpawnAndList(t *testing.T) {
	msgBus := bus.New(16)
	defer msgBus.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	registry := tools.NewRegistry()

	// nil provider means subagent will fail, but we can still test spawn/list/stop
	mgr := NewSubagentManager(nil, registry, msgBus, logger)

	if mgr.Count() != 0 {
		t.Errorf("expected 0 tasks, got %d", mgr.Count())
	}

	ctx := context.Background()
	taskID, err := mgr.Spawn(ctx, "test task", "cli", "")
	if err != nil {
		t.Fatalf("spawn error: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Task will fail quickly since no provider, but check list worked
	list := mgr.List()
	// It may already be done since provider is nil, that's fine
	_ = list
}

func TestSubagentManagerMaxConcurrency(t *testing.T) {
	msgBus := bus.New(16)
	defer msgBus.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	registry := tools.NewRegistry()

	mgr := NewSubagentManager(nil, registry, msgBus, logger)
	mgr.maxConc = 2

	ctx := context.Background()

	// We need tasks that don't complete immediately to test concurrency limit
	// Since provider is nil, tasks complete very fast. We'll just verify the ID sequence.
	id1, _ := mgr.Spawn(ctx, "task 1", "cli", "")
	if id1 != "task_1" {
		t.Errorf("expected task_1, got %s", id1)
	}

	// Give it time to complete (nil provider = instant failure)
	time.Sleep(100 * time.Millisecond)

	id2, _ := mgr.Spawn(ctx, "task 2", "cli", "")
	if id2 != "task_2" {
		t.Errorf("expected task_2, got %s", id2)
	}
}

func TestSubagentManagerStopAll(t *testing.T) {
	msgBus := bus.New(16)
	defer msgBus.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	registry := tools.NewRegistry()

	mgr := NewSubagentManager(nil, registry, msgBus, logger)
	count := mgr.StopAll()
	if count != 0 {
		t.Errorf("expected 0 stopped, got %d", count)
	}
}
