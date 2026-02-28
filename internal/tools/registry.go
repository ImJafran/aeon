package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ImJafran/aeon/internal/providers"
)

const defaultToolTimeout = 60 * time.Second

type Tool interface {
	Name() string
	Description() string
	Parameters() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)
}

type ToolResult struct {
	ToolCallID    string
	ForLLM        string
	ForUser       string
	Silent        bool
	NeedsApproval bool   // If true, the tool execution needs human approval before proceeding
	ApprovalInfo  string // Description of what needs approval
}

type Registry struct {
	tools          map[string]Tool
	mu             sync.RWMutex
	defaultTimeout time.Duration
	logger         *slog.Logger
}

func NewRegistry() *Registry {
	return &Registry{
		tools:          make(map[string]Tool),
		defaultTimeout: defaultToolTimeout,
	}
}

// SetLogger sets the logger for structured observability logging.
func (r *Registry) SetLogger(l *slog.Logger) {
	r.logger = l
}

// SetDefaultTimeout sets the timeout for tool execution.
func (r *Registry) SetDefaultTimeout(d time.Duration) {
	r.defaultTimeout = d
}

func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *Registry) Deregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Execute(ctx context.Context, name string, params json.RawMessage) (ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}

	// Validate parameters before execution
	if err := ValidateParams(tool.Parameters(), params); err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Parameter validation error: %v", err)}, nil
	}

	start := time.Now()

	// Enforce timeout: run tool in goroutine with deadline
	timeout := r.defaultTimeout
	if timeout <= 0 {
		timeout = defaultToolTimeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		tr  ToolResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		tr, err := tool.Execute(ctx, params)
		ch <- result{tr, err}
	}()

	select {
	case res := <-ch:
		if r.logger != nil {
			r.logger.Debug("tool executed",
				"tool", name,
				"duration_ms", time.Since(start).Milliseconds(),
				"params_size", len(params),
				"result_size", len(res.tr.ForLLM),
				"error", res.err,
			)
		}
		return res.tr, res.err
	case <-ctx.Done():
		if r.logger != nil {
			r.logger.Warn("tool timed out",
				"tool", name,
				"timeout", timeout,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		}
		return ToolResult{
			ForLLM: fmt.Sprintf("Tool %q timed out after %v. The operation may still be running in the background.", name, timeout),
		}, nil
	}
}

// ToolDefs returns sorted tool definitions for provider (sorted for KV cache stability).
func (r *Registry) ToolDefs() []providers.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]providers.ToolDef, 0, len(names))
	for _, name := range names {
		tool := r.tools[name]
		defs = append(defs, providers.ToolDef{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		})
	}
	return defs
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
