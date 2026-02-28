package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const anthropicAPIURL = "https://api.anthropic.com/v1/messages"
const anthropicAPIVersion = "2023-06-01"

type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropic(apiKey, model string) *AnthropicProvider {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string     { return "anthropic:" + p.model }
func (p *AnthropicProvider) Available() bool   { return p.apiKey != "" }

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	body := p.buildRequest(req)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", anthropicAPIURL, bytes.NewReader(jsonBody))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("creating request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return p.parseResponse(respBody)
}

type anthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	System    string              `json:"system,omitempty"`
	Messages  []anthropicMessage  `json:"messages"`
	Tools     []anthropicTool     `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	StopReason string `json:"stop_reason"`
}

type anthropicContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

func (p *AnthropicProvider) buildRequest(req CompletionRequest) anthropicRequest {
	// Sanitize: remove orphaned tool messages whose tool_use_id has no matching
	// tool_use in a preceding assistant message. This happens when trimHistory
	// cuts in the middle of a tool call/result sequence, or after provider switches.
	cleaned := sanitizeToolMessages(req.Messages)

	msgs := make([]anthropicMessage, 0, len(cleaned))
	for _, m := range cleaned {
		if m.Role == "tool" {
			// Merge consecutive tool results into a single user message
			toolBlock := map[string]any{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}
			if n := len(msgs); n > 0 && msgs[n-1].Role == "user" {
				// Previous message is already a user message with tool results — append
				if blocks, ok := msgs[n-1].Content.([]map[string]any); ok {
					msgs[n-1].Content = append(blocks, toolBlock)
					continue
				}
			}
			msgs = append(msgs, anthropicMessage{
				Role:    "user",
				Content: []map[string]any{toolBlock},
			})
			continue
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			content := make([]map[string]any, 0)
			if m.Content != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
					input = map[string]any{}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			msgs = append(msgs, anthropicMessage{Role: "assistant", Content: content})
			continue
		}

		msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	tools := make([]anthropicTool, 0, len(req.Tools))
	for _, t := range req.Tools {
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	ar := anthropicRequest{
		Model:     p.model,
		MaxTokens: 4096,
		System:    req.SystemPrompt,
		Messages:  msgs,
	}
	if len(tools) > 0 {
		ar.Tools = tools
	}
	return ar
}

func (p *AnthropicProvider) parseResponse(body []byte) (CompletionResponse, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return CompletionResponse{}, fmt.Errorf("parsing response: %w", err)
	}

	var result CompletionResponse
	result.Provider = p.Name()
	result.Usage = TokenUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(inputJSON),
			})
		}
	}

	return result, nil
}

// sanitizeToolMessages removes tool-result messages whose tool_use_id
// doesn't appear in a preceding assistant message's ToolCalls. This
// prevents Anthropic API errors when history trimming or provider
// switches leave orphaned tool results.
func sanitizeToolMessages(messages []Message) []Message {
	// Collect all tool_use IDs from assistant messages
	toolUseIDs := make(map[string]bool)
	for _, m := range messages {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				toolUseIDs[tc.ID] = true
			}
		}
	}

	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role == "tool" && !toolUseIDs[m.ToolCallID] {
			continue // orphaned tool result, skip
		}
		out = append(out, m)
	}

	// Also strip any assistant tool_use messages whose results were all dropped
	// (i.e., the assistant message has tool calls but none of the following
	// messages are tool results for those calls)
	resultIDs := make(map[string]bool)
	for _, m := range out {
		if m.Role == "tool" {
			resultIDs[m.ToolCallID] = true
		}
	}

	final := make([]Message, 0, len(out))
	for _, m := range out {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Check if at least one tool call has a result
			hasResult := false
			for _, tc := range m.ToolCalls {
				if resultIDs[tc.ID] {
					hasResult = true
					break
				}
			}
			if !hasResult {
				// Convert to plain text message (drop tool calls)
				if m.Content != "" {
					final = append(final, Message{Role: "assistant", Content: m.Content})
				}
				continue
			}
		}
		final = append(final, m)
	}

	return final
}
