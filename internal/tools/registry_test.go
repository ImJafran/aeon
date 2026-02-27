package tools

import (
	"context"
	"encoding/json"
	"testing"
)

type mockTool struct {
	name string
}

func (m *mockTool) Name() string        { return m.name }
func (m *mockTool) Description() string  { return "mock tool" }
func (m *mockTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (m *mockTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	return ToolResult{ForLLM: "executed " + m.name}, nil
}

func TestRegistryRegisterAndExecute(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "test_tool"})

	if r.Count() != 1 {
		t.Fatalf("expected 1 tool, got %d", r.Count())
	}

	result, err := r.Execute(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ForLLM != "executed test_tool" {
		t.Errorf("unexpected result: %s", result.ForLLM)
	}
}

func TestRegistryToolDefsSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "zebra"})
	r.Register(&mockTool{name: "alpha"})
	r.Register(&mockTool{name: "middle"})

	defs := r.ToolDefs()
	if len(defs) != 3 {
		t.Fatalf("expected 3 defs, got %d", len(defs))
	}
	if defs[0].Name != "alpha" || defs[1].Name != "middle" || defs[2].Name != "zebra" {
		t.Errorf("tool defs not sorted: %v, %v, %v", defs[0].Name, defs[1].Name, defs[2].Name)
	}
}

func TestRegistryExecuteNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}
