package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestShellExecBasic(t *testing.T) {
	tool := NewShellExec()

	params, _ := json.Marshal(shellExecParams{Command: "echo hello"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", result.ForLLM)
	}
}

func TestShellExecEmptyCommand(t *testing.T) {
	tool := NewShellExec()

	params, _ := json.Marshal(shellExecParams{Command: ""})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "required") {
		t.Errorf("expected error about required command, got: %s", result.ForLLM)
	}
}

func TestShellExecNonZeroExit(t *testing.T) {
	tool := NewShellExec()

	params, _ := json.Marshal(shellExecParams{Command: "exit 1"})
	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "Exit code 1") && !strings.Contains(result.ForLLM, "exit code 1") {
		t.Errorf("expected exit code in output, got: %s", result.ForLLM)
	}
}
