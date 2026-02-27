package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jafran/aeon/internal/bus"
	"github.com/jafran/aeon/internal/memory"
	"github.com/jafran/aeon/internal/providers"
	"github.com/jafran/aeon/internal/tools"
)

// CredentialScrubber scrubs sensitive data from tool output before it enters conversation history.
type CredentialScrubber interface {
	ScrubCredentials(text string) string
}

// maxHistoryMessages is the maximum number of prior messages to load into context.
const maxHistoryMessages = 20

type AgentLoop struct {
	bus       *bus.MessageBus
	provider  providers.Provider
	registry  *tools.Registry
	memStore  *memory.Store
	scrubber  CredentialScrubber
	subMgr    *SubagentManager
	logger    *slog.Logger
	sessionID string
	history   []providers.Message // in-memory conversation history for current session
}

func NewAgentLoop(b *bus.MessageBus, provider providers.Provider, registry *tools.Registry, logger *slog.Logger) *AgentLoop {
	return &AgentLoop{
		bus:       b,
		provider:  provider,
		registry:  registry,
		logger:    logger,
		sessionID: fmt.Sprintf("session_%d", time.Now().UnixNano()),
	}
}

func (a *AgentLoop) SetScrubber(s CredentialScrubber) {
	a.scrubber = s
}

func (a *AgentLoop) SetSubagentManager(m *SubagentManager) {
	a.subMgr = m
}

func (a *AgentLoop) SetMemoryStore(m *memory.Store) {
	a.memStore = m
}

func (a *AgentLoop) Run(ctx context.Context) {
	a.logger.Info("agent loop started", "session", a.sessionID)

	// Load prior conversation history from database
	a.loadHistory(ctx)

	for {
		select {
		case <-ctx.Done():
			a.logger.Info("agent loop stopped")
			return
		case msg, ok := <-a.bus.Inbound():
			if !ok {
				return
			}
			a.handleMessage(ctx, msg)
		}
	}
}

// loadHistory loads the last N messages from SQLite into memory.
func (a *AgentLoop) loadHistory(ctx context.Context) {
	if a.memStore == nil {
		return
	}

	rows, err := a.memStore.GetHistory(ctx, a.sessionID, maxHistoryMessages)
	if err != nil {
		a.logger.Warn("failed to load history", "error", err)
		return
	}

	for _, row := range rows {
		a.history = append(a.history, providers.Message{
			Role:    row["role"],
			Content: row["content"],
		})
	}

	if len(a.history) > 0 {
		a.logger.Info("loaded conversation history", "messages", len(a.history))
	}
}

func (a *AgentLoop) handleMessage(ctx context.Context, msg bus.InboundMessage) {
	a.logger.Info("received message",
		"channel", msg.Channel,
		"content_len", len(msg.Content),
	)

	// Handle slash commands
	if len(msg.Content) > 0 && msg.Content[0] == '/' {
		a.handleCommand(ctx, msg)
		return
	}

	// If no provider is configured, echo mode
	if a.provider == nil {
		a.bus.Send(bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: fmt.Sprintf("[Aeon] %s", msg.Content),
		})
		return
	}

	// Run agent loop with full conversation history
	a.runAgentLoop(ctx, msg)
}

func (a *AgentLoop) runAgentLoop(ctx context.Context, msg bus.InboundMessage) {
	// Add user message to history
	userMsg := providers.Message{Role: "user", Content: msg.Content}
	a.history = append(a.history, userMsg)
	a.saveToHistory(ctx, "user", msg.Content)

	// Build system prompt with relevant memories injected
	systemPrompt := a.buildSystemPrompt(ctx, msg.Content)

	// Build messages: full conversation history
	messages := make([]providers.Message, len(a.history))
	copy(messages, a.history)

	toolDefs := a.registry.ToolDefs()

	maxIterations := 20
	for i := 0; i < maxIterations; i++ {
		resp, err := a.provider.Complete(ctx, providers.CompletionRequest{
			SystemPrompt: systemPrompt,
			Messages:     messages,
			Tools:        toolDefs,
		})
		if err != nil {
			a.logger.Error("provider error", "error", err)
			a.bus.Send(bus.OutboundMessage{
				Channel: msg.Channel,
				ChatID:  msg.ChatID,
				Content: fmt.Sprintf("[Error] %v", err),
			})
			return
		}

		// If there are tool calls, execute them
		if len(resp.ToolCalls) > 0 {
			// Add assistant message with tool calls
			assistantMsg := providers.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			}
			messages = append(messages, assistantMsg)

			// Execute tools (parallel for independent calls)
			results := a.executeTools(ctx, resp.ToolCalls)
			for _, result := range results {
				// Scrub credentials from tool output before it enters conversation
				forLLM := result.ForLLM
				if a.scrubber != nil {
					forLLM = a.scrubber.ScrubCredentials(forLLM)
				}
				messages = append(messages, providers.Message{
					Role:       "tool",
					Content:    forLLM,
					ToolCallID: result.ToolCallID,
				})

				// Send user-visible output if any
				if result.ForUser != "" && !result.Silent {
					a.bus.Send(bus.OutboundMessage{
						Channel: msg.Channel,
						ChatID:  msg.ChatID,
						Content: result.ForUser,
					})
				}
			}
			continue
		}

		// Text response — send to user, save to history, break
		if resp.Content != "" {
			a.bus.Send(bus.OutboundMessage{
				Channel: msg.Channel,
				ChatID:  msg.ChatID,
				Content: resp.Content,
			})

			// Save assistant response to history
			a.history = append(a.history, providers.Message{
				Role:    "assistant",
				Content: resp.Content,
			})
			a.saveToHistory(ctx, "assistant", resp.Content)

			// Trim in-memory history if it gets too long
			a.trimHistory()
		}
		return
	}

	a.bus.Send(bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: "[Aeon] Max tool iterations reached. Stopping.",
	})
}

// saveToHistory persists a message to the SQLite conversation_history table.
func (a *AgentLoop) saveToHistory(ctx context.Context, role, content string) {
	if a.memStore == nil {
		return
	}
	if err := a.memStore.SaveHistory(ctx, a.sessionID, role, content); err != nil {
		a.logger.Warn("failed to save history", "error", err)
	}
}

// trimHistory keeps the in-memory history bounded.
// Drops oldest messages beyond 2x maxHistoryMessages, keeping the most recent ones.
func (a *AgentLoop) trimHistory() {
	limit := maxHistoryMessages * 2
	if len(a.history) > limit {
		a.history = a.history[len(a.history)-maxHistoryMessages:]
	}
}

// clearHistory resets the conversation for /new command.
func (a *AgentLoop) clearHistory(ctx context.Context) {
	a.history = nil
	if a.memStore != nil {
		a.memStore.ClearHistory(ctx, a.sessionID)
	}
	a.sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
}

func (a *AgentLoop) executeTools(ctx context.Context, calls []providers.ToolCall) []tools.ToolResult {
	results := make([]tools.ToolResult, len(calls))

	if len(calls) == 1 {
		// Single tool — no goroutine overhead
		result, err := a.registry.Execute(ctx, calls[0].Name, []byte(calls[0].Arguments))
		if err != nil {
			results[0] = tools.ToolResult{
				ToolCallID: calls[0].ID,
				ForLLM:     fmt.Sprintf("Error executing %s: %v", calls[0].Name, err),
			}
		} else {
			result.ToolCallID = calls[0].ID
			results[0] = result
		}
		return results
	}

	// Multiple tools — execute in parallel
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(idx int, tc providers.ToolCall) {
			defer wg.Done()
			a.logger.Debug("executing tool", "name", tc.Name, "id", tc.ID)

			result, err := a.registry.Execute(ctx, tc.Name, []byte(tc.Arguments))
			if err != nil {
				results[idx] = tools.ToolResult{
					ToolCallID: tc.ID,
					ForLLM:     fmt.Sprintf("Error executing %s: %v", tc.Name, err),
				}
				return
			}
			result.ToolCallID = tc.ID
			results[idx] = result
		}(i, call)
	}
	wg.Wait()

	return results
}

func (a *AgentLoop) buildSystemPrompt(ctx context.Context, query string) string {
	base := `You are Aeon, an autonomous AI assistant running as a persistent kernel on the user's system.

Core capabilities:
- shell_exec: Run shell commands with security policy enforcement
- file_read, file_write, file_edit: File operations
- web_read: Fetch and extract web content
- memory_store, memory_recall: Persistent long-term memory across sessions
- skill_factory: Create new Python/Bash tools that persist
- find_skills, read_skill, run_skill: Use evolved skills
- cron_manage: Schedule recurring tasks
- spawn_agent: Delegate tasks to background subagents
- list_tasks: View active background tasks

Behavior:
- Be concise and direct. Prefer action over explanation.
- Use memory_store for important information the user shares.
- When a task is complex, consider using spawn_agent to run parts in parallel.
- Report errors clearly with actionable next steps.
- For dangerous operations, the security policy will block or require approval.`

	// Inject relevant memories into system prompt
	if a.memStore != nil && query != "" {
		memContext := a.memStore.BuildContextFromMemory(ctx, query)
		if memContext != "" {
			base += "\n\n" + memContext
		}
	}

	return base
}

func (a *AgentLoop) handleCommand(ctx context.Context, msg bus.InboundMessage) {
	var response string

	switch msg.Content {
	case "/status":
		providerName := "none"
		if a.provider != nil {
			providerName = a.provider.Name()
		}
		toolCount := len(a.registry.ToolDefs())
		taskCount := 0
		if a.subMgr != nil {
			taskCount = a.subMgr.Count()
		}
		historyCount := len(a.history)
		response = fmt.Sprintf("Aeon Status:\n  Provider: %s\n  Tools: %d loaded\n  Active tasks: %d\n  Session: %d messages", providerName, toolCount, taskCount, historyCount)
	case "/skills":
		response = "Use find_skills tool to list installed skills."
	case "/new":
		a.clearHistory(ctx)
		response = "Conversation cleared. Starting fresh."
	case "/stop":
		if a.subMgr != nil {
			count := a.subMgr.StopAll()
			if count > 0 {
				response = fmt.Sprintf("Cancelled %d active task(s).", count)
			} else {
				response = "No active tasks to stop."
			}
		} else {
			response = "No active tasks to stop."
		}
	case "/help":
		response = "Commands:\n  /status  — Show system status\n  /skills  — List evolved skills\n  /new     — Start fresh conversation\n  /stop    — Cancel running tasks\n  /help    — Show this help"
	default:
		response = fmt.Sprintf("Unknown command: %s. Type /help for available commands.", msg.Content)
	}

	a.bus.Send(bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: response,
	})
}
