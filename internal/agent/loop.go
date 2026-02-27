package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jafran/aeon/internal/bus"
	"github.com/jafran/aeon/internal/providers"
	"github.com/jafran/aeon/internal/tools"
)

type AgentLoop struct {
	bus      *bus.MessageBus
	provider providers.Provider
	registry *tools.Registry
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
				messages = append(messages, providers.Message{
					Role:       "tool",
					Content:    result.ForLLM,
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

	// Execute tools — for now sequential, parallel execution in Phase 3
	for i, call := range calls {
		result, err := a.registry.Execute(ctx, call.Name, []byte(call.Arguments))
		if err != nil {
			results[i] = tools.ToolResult{
				ToolCallID: call.ID,
				ForLLM:     fmt.Sprintf("Error executing %s: %v", call.Name, err),
			}
			continue
		}
		result.ToolCallID = call.ID
		results[i] = result
	}
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
		response = fmt.Sprintf("Aeon Status:\n  Provider: %s\n  Tools: %d loaded", providerName, toolCount)
	case "/skills":
		response = "Skills: none loaded (Phase 6)"
	case "/new":
		response = "Conversation cleared."
	case "/stop":
		response = "No active tasks to stop."
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
