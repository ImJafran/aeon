package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WebReadTool struct {
	client *http.Client
}

func NewWebRead() *WebReadTool {
	return &WebReadTool{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *WebReadTool) Name() string        { return "web_read" }
func (t *WebReadTool) Description() string  { return "Fetch a web page and return its content as markdown using Jina Reader API." }
func (t *WebReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {
				"type": "string",
				"description": "The URL to fetch"
			}
		},
		"required": ["url"]
	}`)
}

type webReadParams struct {
	URL string `json:"url"`
}

func (t *WebReadTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p webReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.URL == "" {
		return ToolResult{ForLLM: "Error: url is required"}, nil
	}

	// Use Jina Reader API for clean markdown extraction
	jinaURL := "https://r.jina.ai/" + p.URL

	req, err := http.NewRequestWithContext(ctx, "GET", jinaURL, nil)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error: %v", err)}, nil
	}
	req.Header.Set("Accept", "text/markdown")

	resp, err := t.client.Do(req)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error fetching URL: %v", err)}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ToolResult{ForLLM: fmt.Sprintf("HTTP %d from %s", resp.StatusCode, p.URL)}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024)) // 50KB limit
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error reading response: %v", err)}, nil
	}

	content := string(body)
	// Truncate for LLM context
	if len(content) > 8000 {
		content = content[:8000] + "\n\n... [content truncated at 8000 chars]"
	}

	return ToolResult{ForLLM: content}, nil
}
