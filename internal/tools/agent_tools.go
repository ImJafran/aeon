package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SubagentSpawner is the interface for spawning background tasks.
// This avoids a circular dependency with the agent package.
type SubagentSpawner interface {
	Spawn(ctx context.Context, description, channel, chatID string) (string, error)
	StopAll() int
	Stop(taskID string) error
	List() []TaskInfo
	Count() int
}

// TaskInfo is a summary of a background task.
type TaskInfo struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	StartedAt   time.Time `json:"started_at"`
	Running     bool      `json:"running"`
}

// ---- spawn_agent ----

type SpawnAgentTool struct {
	spawner SubagentSpawner
}

func NewSpawnAgent(spawner SubagentSpawner) *SpawnAgentTool {
	return &SpawnAgentTool{spawner: spawner}
}

func (t *SpawnAgentTool) Name() string { return "spawn_agent" }
func (t *SpawnAgentTool) Description() string {
	return "Spawn a background subagent to handle a task independently. The result will be posted when complete."
}
func (t *SpawnAgentTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {
				"type": "string",
				"description": "Description of the task for the subagent to complete"
			}
		},
		"required": ["task"]
	}`)
}

type spawnAgentParams struct {
	Task string `json:"task"`
}

func (t *SpawnAgentTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p spawnAgentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.Task == "" {
		return ToolResult{ForLLM: "Error: task description is required"}, nil
	}

	taskID, err := t.spawner.Spawn(ctx, p.Task, "cli", "")
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error spawning agent: %v", err)}, nil
	}

	return ToolResult{
		ForLLM:  fmt.Sprintf("Background task spawned (id=%s). Results will be posted when complete.", taskID),
		ForUser: fmt.Sprintf("Started background task: %s", p.Task),
	}, nil
}

// ---- list_tasks ----

type ListTasksTool struct {
	spawner SubagentSpawner
}

func NewListTasks(spawner SubagentSpawner) *ListTasksTool {
	return &ListTasksTool{spawner: spawner}
}

func (t *ListTasksTool) Name() string        { return "list_tasks" }
func (t *ListTasksTool) Description() string  { return "List all active background tasks." }
func (t *ListTasksTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ListTasksTool) Execute(_ context.Context, _ json.RawMessage) (ToolResult, error) {
	tasks := t.spawner.List()

	if len(tasks) == 0 {
		return ToolResult{ForLLM: "No active background tasks."}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Active tasks (%d):\n", len(tasks)))
	for _, task := range tasks {
		b.WriteString(fmt.Sprintf("\n[%s] %s (started: %s)",
			task.ID, task.Description, task.StartedAt.Format("15:04:05")))
	}

	return ToolResult{ForLLM: b.String()}, nil
}
