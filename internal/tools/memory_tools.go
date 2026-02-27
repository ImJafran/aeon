package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jafran/aeon/internal/memory"
)

// ---- memory_store ----

type MemoryStoreTool struct {
	store *memory.Store
}

func NewMemoryStore(store *memory.Store) *MemoryStoreTool {
	return &MemoryStoreTool{store: store}
}

func (t *MemoryStoreTool) Name() string        { return "memory_store" }
func (t *MemoryStoreTool) Description() string  { return "Store information in long-term memory for future recall." }
func (t *MemoryStoreTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"content": {
				"type": "string",
				"description": "The information to store"
			},
			"category": {
				"type": "string",
				"enum": ["core", "daily", "conversation", "custom"],
				"description": "Memory category (default: custom)"
			},
			"tags": {
				"type": "string",
				"description": "Comma-separated tags for searchability"
			}
		},
		"required": ["content"]
	}`)
}

type memoryStoreParams struct {
	Content  string `json:"content"`
	Category string `json:"category"`
	Tags     string `json:"tags"`
}

func (t *MemoryStoreTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p memoryStoreParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.Content == "" {
		return ToolResult{ForLLM: "Error: content is required"}, nil
	}

	category := memory.CategoryCustom
	switch p.Category {
	case "core":
		category = memory.CategoryCore
	case "daily":
		category = memory.CategoryDaily
	case "conversation":
		category = memory.CategoryConversation
	}

	id, err := t.store.MemStore(ctx, category, p.Content, p.Tags)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error storing memory: %v", err)}, nil
	}

	return ToolResult{
		ForLLM: fmt.Sprintf("Memory stored (id=%d, category=%s)", id, category),
		Silent: true,
	}, nil
}

// ---- memory_recall ----

type MemoryRecallTool struct {
	store *memory.Store
}

func NewMemoryRecall(store *memory.Store) *MemoryRecallTool {
	return &MemoryRecallTool{store: store}
}

func (t *MemoryRecallTool) Name() string        { return "memory_recall" }
func (t *MemoryRecallTool) Description() string  { return "Search long-term memory for previously stored information." }
func (t *MemoryRecallTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Search query (keywords)"
			},
			"category": {
				"type": "string",
				"enum": ["core", "daily", "conversation", "custom", ""],
				"description": "Filter by category (optional)"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum results to return (default: 5)"
			}
		},
		"required": ["query"]
	}`)
}

type memoryRecallParams struct {
	Query    string `json:"query"`
	Category string `json:"category"`
	Limit    int    `json:"limit"`
}

func (t *MemoryRecallTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p memoryRecallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.Query == "" {
		return ToolResult{ForLLM: "Error: query is required"}, nil
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}

	entries, err := t.store.Recall(ctx, p.Query, limit)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error searching memory: %v", err)}, nil
	}

	if len(entries) == 0 {
		return ToolResult{ForLLM: "No memories found matching the query."}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d memories:\n", len(entries)))
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("\n[%d] (%s) %s", e.ID, e.Category, e.Content))
		if e.Tags != "" {
			b.WriteString(fmt.Sprintf(" [tags: %s]", e.Tags))
		}
	}

	return ToolResult{ForLLM: b.String(), Silent: true}, nil
}
