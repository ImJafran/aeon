package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type ClaudeCLIProvider struct {
	binary  string
	flags   []string
	timeout time.Duration
	mu      sync.Mutex
}

func NewClaudeCLI(binary string, flags []string, timeout time.Duration) *ClaudeCLIProvider {
	if binary == "" {
		binary = "claude"
	}
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	return &ClaudeCLIProvider{
		binary:  binary,
		flags:   flags,
		timeout: timeout,
	}
}

func (p *ClaudeCLIProvider) Name() string { return "claude_cli" }

func (p *ClaudeCLIProvider) Available() bool {
	_, err := exec.LookPath(p.binary)
	return err == nil
}

func (p *ClaudeCLIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	prompt := p.buildPrompt(req)

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	args := []string{"-p", "--output-format", "json"}
	args = append(args, p.flags...)

	cmd := exec.CommandContext(ctx, p.binary, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(prompt)

	if err := cmd.Run(); err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			return CompletionResponse{}, fmt.Errorf("claude cli error: %s", stderrStr)
		}
		return CompletionResponse{}, fmt.Errorf("claude cli error: %w", err)
	}

	return p.parseOutput(stdout.Bytes())
}

func (p *ClaudeCLIProvider) buildPrompt(req CompletionRequest) string {
	var b strings.Builder

	if req.SystemPrompt != "" {
		b.WriteString("[System]\n")
		b.WriteString(req.SystemPrompt)
		b.WriteString("\n\n")
	}

	if len(req.Tools) > 0 {
		b.WriteString("[Available Tools]\n")
		for _, t := range req.Tools {
			paramsJSON, _ := json.Marshal(t.Parameters)
			b.WriteString(fmt.Sprintf("- %s: %s\n  Parameters: %s\n", t.Name, t.Description, string(paramsJSON)))
		}
		b.WriteString("\nTo use a tool, respond with JSON: {\"tool\": \"name\", \"arguments\": {...}}\n\n")
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			b.WriteString("[User]\n")
			b.WriteString(m.Content)
			b.WriteString("\n\n")
		case "assistant":
			b.WriteString("[Assistant]\n")
			b.WriteString(m.Content)
			b.WriteString("\n\n")
		case "tool":
			b.WriteString("[Tool Result]\n")
			b.WriteString(m.Content)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

// claudeCLIResponse represents the JSON output from `claude -p --output-format json`
type claudeCLIResponse struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Result  string `json:"result"`
	// Sometimes the output is an array of content blocks
	ContentBlocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content_blocks"`
}

func (p *ClaudeCLIProvider) parseOutput(data []byte) (CompletionResponse, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return CompletionResponse{}, fmt.Errorf("empty response from claude cli")
	}

	// Try parsing as JSON
	var resp claudeCLIResponse
	if err := json.Unmarshal(data, &resp); err == nil {
		content := resp.Result
		if content == "" {
			content = resp.Content
		}
		if content == "" && len(resp.ContentBlocks) > 0 {
			var parts []string
			for _, block := range resp.ContentBlocks {
				if block.Type == "text" {
					parts = append(parts, block.Text)
				}
			}
			content = strings.Join(parts, "")
		}

		if content != "" {
			return CompletionResponse{
				Content:  content,
				Provider: p.Name(),
			}, nil
		}
	}

	// Fallback: treat entire output as text response
	return CompletionResponse{
		Content:  string(data),
		Provider: p.Name(),
	}, nil
}
