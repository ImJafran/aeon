package providers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
)

type mockProvider struct {
	name      string
	available bool
	fail      bool
}

func (m *mockProvider) Name() string     { return m.name }
func (m *mockProvider) Available() bool   { return m.available }
func (m *mockProvider) Complete(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
	if m.fail {
		return CompletionResponse{}, fmt.Errorf("mock failure")
	}
	return CompletionResponse{
		Content:  "response from " + m.name,
		Provider: m.name,
	}, nil
}

func TestChainSingleProvider(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	primary := &mockProvider{name: "primary", available: true}

	chain := NewChain(ChainConfig{Primary: primary}, logger)

	resp, err := chain.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "primary" {
		t.Errorf("expected provider 'primary', got '%s'", resp.Provider)
	}
}

func TestChainFallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	primary := &mockProvider{name: "primary", available: true, fail: true}
	fallback := &mockProvider{name: "fallback", available: true}

	chain := NewChain(ChainConfig{Primary: primary, Fallback: fallback}, logger)

	resp, err := chain.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "fallback" {
		t.Errorf("expected fallback provider, got '%s'", resp.Provider)
	}
}

func TestChainFastHint(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	primary := &mockProvider{name: "primary", available: true}
	fast := &mockProvider{name: "fast", available: true}

	chain := NewChain(ChainConfig{Primary: primary, Fast: fast}, logger)

	resp, err := chain.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Hint:     "fast",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Provider != "fast" {
		t.Errorf("expected fast provider, got '%s'", resp.Provider)
	}
}

func TestChainNoProvider(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	chain := NewChain(ChainConfig{}, logger)

	_, err := chain.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error with no providers")
	}
}
