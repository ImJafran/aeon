package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/bus"
	"github.com/ImJafran/aeon/internal/providers"
	"github.com/ImJafran/aeon/internal/tools"
)

// SubagentTask represents a running background task.
type SubagentTask struct {
	ID          string
	Description string
	StartedAt   time.Time
	Cancel      context.CancelFunc
	Done        chan struct{}
	mu          sync.Mutex
	result      string
	err         error
}

// SetResult safely stores the result and error for a completed task.
func (t *SubagentTask) SetResult(result string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.result = result
	t.err = err
}

// GetResult safely retrieves the result and error from a task.
func (t *SubagentTask) GetResult() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result, t.err
}

// SubagentManager manages background subagent tasks.
// It implements tools.SubagentSpawner interface.
type SubagentManager struct {
	mu       sync.Mutex
	tasks    map[string]*SubagentTask
	nextID   int
	maxConc  int
	provider providers.Provider
	registry *tools.Registry
	scrubber CredentialScrubber
	msgBus   *bus.MessageBus
	logger   *slog.Logger
}

// NewSubagentManager creates a new subagent manager.
func NewSubagentManager(provider providers.Provider, registry *tools.Registry, msgBus *bus.MessageBus, logger *slog.Logger) *SubagentManager {
	return &SubagentManager{
		tasks:    make(map[string]*SubagentTask),
		maxConc:  3,
		provider: provider,
		registry: registry,
		msgBus:   msgBus,
		logger:   logger,
	}
}

// SetScrubber sets the credential scrubber for subagent outputs.
func (m *SubagentManager) SetScrubber(s CredentialScrubber) {
	m.scrubber = s
}

// Spawn creates a new background task.
func (m *SubagentManager) Spawn(ctx context.Context, description, channel, chatID string) (string, error) {
	m.mu.Lock()
	if len(m.tasks) >= m.maxConc {
		m.mu.Unlock()
		return "", fmt.Errorf("max concurrent subagents reached (%d)", m.maxConc)
	}

	m.nextID++
	taskID := fmt.Sprintf("task_%d", m.nextID)

	taskCtx, cancel := context.WithCancel(ctx)
	task := &SubagentTask{
		ID:          taskID,
		Description: description,
		StartedAt:   time.Now(),
		Cancel:      cancel,
		Done:        make(chan struct{}),
	}
	m.tasks[taskID] = task
	m.mu.Unlock()

	go func() {
		defer close(task.Done)
		defer func() {
			m.mu.Lock()
			delete(m.tasks, taskID)
			m.mu.Unlock()
		}()

		result, err := m.runSubagent(taskCtx, description)
		task.SetResult(result, err)

		var content string
		if err != nil {
			content = fmt.Sprintf("[Task %s completed with error]\nTask: %s\nError: %v", taskID, description, err)
		} else {
			content = fmt.Sprintf("[Task %s completed]\nTask: %s\nResult: %s", taskID, description, result)
		}

		// Scrub credentials before sending to user
		if m.scrubber != nil {
			content = m.scrubber.ScrubCredentials(content)
		}

		m.msgBus.Send(bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: content,
		})
	}()

	return taskID, nil
}

// runSubagent runs a simplified agent loop for the background task.
func (m *SubagentManager) runSubagent(ctx context.Context, task string) (string, error) {
	if m.provider == nil {
		return "", fmt.Errorf("no provider available")
	}

	messages := []providers.Message{
		{Role: "user", Content: fmt.Sprintf("Background task: %s\n\nComplete this task and provide a concise summary of results.", task)},
	}

	systemPrompt := `You are Aeon, running a background subtask.
Complete the task efficiently and return a concise result.
You have access to tools for file operations, shell commands, and more.`

	toolDefs := m.registry.ToolDefs()

	maxIterations := 15
	for i := 0; i < maxIterations; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		resp, err := m.provider.Complete(ctx, providers.CompletionRequest{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        toolDefs,
			Hint:         "fast",
		})
		if err != nil {
			return "", fmt.Errorf("provider error: %w", err)
		}

		if len(resp.ToolCalls) > 0 {
			messages = append(messages, providers.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

			for _, tc := range resp.ToolCalls {
				result, err := m.registry.Execute(ctx, tc.Name, []byte(tc.Arguments))
				forLLM := result.ForLLM
				if err != nil {
					forLLM = fmt.Sprintf("Error: %v", err)
				}
				if m.scrubber != nil {
					forLLM = m.scrubber.ScrubCredentials(forLLM)
				}
				messages = append(messages, providers.Message{
					Role:       "tool",
					Content:    forLLM,
					ToolCallID: tc.ID,
				})
			}
			continue
		}

		return resp.Content, nil
	}

	return "", fmt.Errorf("max iterations reached")
}

// StopAll cancels all running subagent tasks.
func (m *SubagentManager) StopAll() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := len(m.tasks)
	for _, task := range m.tasks {
		task.Cancel()
	}
	return count
}

// Stop cancels a specific task by ID.
func (m *SubagentManager) Stop(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %s not found", taskID)
	}

	task.Cancel()
	return nil
}

// List returns all active subagent tasks.
func (m *SubagentManager) List() []tools.TaskInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var list []tools.TaskInfo
	for _, t := range m.tasks {
		list = append(list, tools.TaskInfo{
			ID:          t.ID,
			Description: t.Description,
			StartedAt:   t.StartedAt,
			Running:     true,
		})
	}
	return list
}

// Count returns the number of active tasks.
func (m *SubagentManager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.tasks)
}
