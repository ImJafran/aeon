package providers

import "context"

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"input_schema"`
}

type CompletionRequest struct {
	SystemPrompt string
	Messages     []Message
	Tools        []ToolDef
	Hint         string // "fast", "normal", "complex"
}

type CompletionResponse struct {
	Content   string
	ToolCalls []ToolCall
	Usage     TokenUsage
	Provider  string
}

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	Name() string
	Available() bool
}
