package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/jafran/aeon/internal/bus"
	"github.com/jafran/aeon/internal/providers"
	"github.com/jafran/aeon/internal/tools"
)

// CredentialScrubber scrubs sensitive data from tool output before it enters conversation history.
type CredentialScrubber interface {
	ScrubCredentials(text string) string
}

type AgentLoop struct {
	bus      *bus.MessageBus
	provider providers.Provider
	registry *tools.Registry
	scrubber CredentialScrubber
	subMgr   *SubagentManager
	logger   *slog.Logger
}

func NewAgentLoop(b *bus.MessageBus, provider providers.Provider, registry *tools.Registry, logger *slog.Logger) *AgentLoop {
	return &AgentLoop{
		bus:      b,
		provider: provider,
		registry: registry,
		logger:   logger,
	}
}

func (a *AgentLoop) SetScrubber(s CredentialScrubber) {
	a.scrubber = s
}

func (a *AgentLoop) SetSubagentManager(m *SubagentManager) {
	a.subMgr = m
}

func (a *AgentLoop) Run(ctx context.Context) {
	a.logger.Info("agent loop started")

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

func (a *AgentLoop) handleMessage(ctx context.Context, msg bus.InboundMessage) {
	a.logger.Info("received message",
		"channel", msg.Channel,
		"content_len", len(msg.Content),
	)

	// Handle slash commands
	if len(msg.Content) > 0 && msg.Content[0] == '/' {
		a.handleCommand(msg)
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

	// Build messages for LLM
	a.runAgentLoop(ctx, msg)
}

func (a *AgentLoop) runAgentLoop(ctx context.Context, msg bus.InboundMessage) {
	messages := []providers.Message{
		{Role: "user", Content: msg.Content},
	}

	systemPrompt := a.buildSystemPrompt()
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
			messages = append(messages, providers.Message{
				Role:      "assistant",
				Content:   resp.Content,
				ToolCalls: resp.ToolCalls,
			})

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

		// Text response — send to user and break
		if resp.Content != "" {
			a.bus.Send(bus.OutboundMessage{
				Channel: msg.Channel,
				ChatID:  msg.ChatID,
				Content: resp.Content,
			})
		}
		return
	}

	a.bus.Send(bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: "[Aeon] Max tool iterations reached. Stopping.",
	})
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

func (a *AgentLoop) buildSystemPrompt() string {
	return `You are Aeon, an autonomous AI assistant running as a persistent kernel.
You have access to tools for file operations, shell commands, and more.
Be concise and direct. Report errors clearly.`
}

func (a *AgentLoop) handleCommand(msg bus.InboundMessage) {
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
		response = fmt.Sprintf("Aeon Status:\n  Provider: %s\n  Tools: %d loaded\n  Active tasks: %d", providerName, toolCount, taskCount)
	case "/skills":
		response = "Use find_skills tool to list installed skills."
	case "/new":
		response = "Conversation cleared."
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
