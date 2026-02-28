package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
	"github.com/ImJafran/aeon/internal/providers"
	"github.com/ImJafran/aeon/internal/tools"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockProvider returns configurable response sequences.
type mockProvider struct {
	responses []providers.CompletionResponse
	errors    []error
	idx       int
	name      string
}

func newMockProvider(name string, resps ...providers.CompletionResponse) *mockProvider {
	return &mockProvider{name: name, responses: resps}
}

func (m *mockProvider) Name() string     { return m.name }
func (m *mockProvider) Available() bool   { return true }

func (m *mockProvider) Complete(_ context.Context, _ providers.CompletionRequest) (providers.CompletionResponse, error) {
	i := m.idx
	m.idx++
	if i < len(m.errors) && m.errors[i] != nil {
		return providers.CompletionResponse{}, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return providers.CompletionResponse{Content: "fallback response", Provider: m.name}, nil
}

// mockTestTool is a simple tool for testing.
type mockTestTool struct {
	name   string
	result string
}

func (t *mockTestTool) Name() string        { return t.name }
func (t *mockTestTool) Description() string  { return "test tool" }
func (t *mockTestTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":[]}`)
}
func (t *mockTestTool) Execute(_ context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{ForLLM: t.result, ForUser: t.result}, nil
}

// mockScrubber replaces "SECRET" with "[REDACTED]".
type mockScrubber struct{}

func (s *mockScrubber) ScrubCredentials(text string) string {
	return strings.ReplaceAll(text, "SECRET", "[REDACTED]")
}

func setupTestLoop(provider providers.Provider) (*AgentLoop, *bus.MessageBus, chan bus.OutboundMessage) {
	msgBus := bus.New(64)
	registry := tools.NewRegistry()
	registry.Register(&mockTestTool{name: "echo_tool", result: "echo_result"})

	logger := newTestLogger()

	loop := NewAgentLoop(msgBus, provider, registry, logger)
	outCh := msgBus.Subscribe()

	return loop, msgBus, outCh
}

func TestSimpleTextResponse(t *testing.T) {
	provider := newMockProvider("test",
		providers.CompletionResponse{Content: "Hello!", Provider: "test", Usage: providers.TokenUsage{InputTokens: 10, OutputTokens: 5}},
	)
	loop, msgBus, outCh := setupTestLoop(provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go loop.Run(ctx)

	// Send a message
	msgBus.Publish(bus.InboundMessage{Channel: "test", ChatID: "1", Content: "hi"})

	// Read response
	select {
	case out := <-outCh:
		if out.Content != "Hello!" {
			t.Errorf("expected 'Hello!', got %q", out.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestToolCallFlow(t *testing.T) {
	provider := newMockProvider("test",
		// First response: tool call
		providers.CompletionResponse{
			ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "echo_tool", Arguments: `{}`}},
			Provider:  "test",
			Usage:     providers.TokenUsage{InputTokens: 20, OutputTokens: 10},
		},
		// Second response: text after tool result
		providers.CompletionResponse{
			Content:  "Done!",
			Provider: "test",
			Usage:    providers.TokenUsage{InputTokens: 30, OutputTokens: 15},
		},
	)
	loop, msgBus, outCh := setupTestLoop(provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go loop.Run(ctx)

	msgBus.Publish(bus.InboundMessage{Channel: "test", ChatID: "1", Content: "do something"})

	// Collect non-status messages (tool ForUser output + final response)
	var messages []string
	deadline := time.After(3 * time.Second)
	for {
		select {
		case out := <-outCh:
			// Skip status update messages
			if out.Metadata != nil && out.Metadata[bus.MetaStatus] == "true" {
				continue
			}
			messages = append(messages, out.Content)
			if out.Content == "Done!" {
				goto done
			}
		case <-deadline:
			t.Fatalf("timeout waiting for messages, got %d so far: %v", len(messages), messages)
		}
	}
done:

	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d: %v", len(messages), messages)
	}
	if messages[len(messages)-1] != "Done!" {
		t.Errorf("expected last message to be 'Done!', got %q", messages[len(messages)-1])
	}
}

func TestMaxIterationsStop(t *testing.T) {
	// Provider always returns tool calls — loop should stop at max iterations
	resp := providers.CompletionResponse{
		ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "echo_tool", Arguments: `{}`}},
		Provider:  "test",
	}
	resps := make([]providers.CompletionResponse, 25)
	for i := range resps {
		resps[i] = resp
	}
	provider := newMockProvider("test", resps...)
	loop, msgBus, outCh := setupTestLoop(provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go loop.Run(ctx)

	msgBus.Publish(bus.InboundMessage{Channel: "test", ChatID: "1", Content: "loop forever"})

	// Should eventually get the "max iterations" message
	deadline := time.After(5 * time.Second)
	for {
		select {
		case out := <-outCh:
			if strings.Contains(out.Content, "Max tool iterations") {
				return // success
			}
		case <-deadline:
			t.Fatal("timeout waiting for max iterations message")
		}
	}
}

func TestCredentialScrubbing(t *testing.T) {
	provider := newMockProvider("test",
		// Tool call that returns SECRET
		providers.CompletionResponse{
			ToolCalls: []providers.ToolCall{{ID: "tc1", Name: "secret_tool", Arguments: `{}`}},
			Provider:  "test",
		},
		providers.CompletionResponse{Content: "done", Provider: "test"},
	)

	msgBus := bus.New(64)
	registry := tools.NewRegistry()
	registry.Register(&mockTestTool{name: "secret_tool", result: "key=SECRET"})
	logger := newTestLogger()

	loop := NewAgentLoop(msgBus, provider, registry, logger)
	loop.SetScrubber(&mockScrubber{})
	outCh := msgBus.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go loop.Run(ctx)

	msgBus.Publish(bus.InboundMessage{Channel: "test", ChatID: "1", Content: "get secret"})

	// The ForUser output from the tool should be scrubbed
	deadline := time.After(2 * time.Second)
	for {
		select {
		case out := <-outCh:
			if strings.Contains(out.Content, "SECRET") {
				t.Errorf("credential leaked in output: %q", out.Content)
				return
			}
			if out.Content == "done" {
				return // success, no leak
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}
}

func TestCostTrackerIntegration(t *testing.T) {
	provider := newMockProvider("test",
		providers.CompletionResponse{
			Content:  "hi",
			Provider: "test",
			Usage:    providers.TokenUsage{InputTokens: 100, OutputTokens: 50},
		},
	)
	loop, msgBus, outCh := setupTestLoop(provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go loop.Run(ctx)

	msgBus.Publish(bus.InboundMessage{Channel: "test", ChatID: "1", Content: "test"})

	select {
	case <-outCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	summary := loop.costTracker.Summary()
	if !strings.Contains(summary, "100 in") || !strings.Contains(summary, "50 out") {
		t.Errorf("cost tracker should have recorded tokens, got: %s", summary)
	}
}
