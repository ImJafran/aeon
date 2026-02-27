package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const maxOutputLen = 10000
const defaultShellTimeout = 30 * time.Second

type SecurityChecker interface {
	CheckCommand(command string) (int, string) // 0=allowed, 1=denied, 2=needs_approval
	ScrubCredentials(text string) string
}

type ShellExecTool struct {
	timeout  time.Duration
	security SecurityChecker
}

func NewShellExec() *ShellExecTool {
	return &ShellExecTool{timeout: defaultShellTimeout}
}

func (t *ShellExecTool) SetSecurity(s SecurityChecker) {
	t.security = s
}

func (t *ShellExecTool) Name() string        { return "shell_exec" }
func (t *ShellExecTool) Description() string  { return "Execute a shell command and return the output." }
func (t *ShellExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to execute"
			},
			"timeout_seconds": {
				"type": "integer",
				"description": "Optional timeout in seconds (default: 30)"
			}
		},
		"required": ["command"]
	}`)
}

type shellExecParams struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

func (t *ShellExecTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p shellExecParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.Command == "" {
		return ToolResult{ForLLM: "Error: command is required"}, nil
	}

	// Security check
	if t.security != nil {
		decision, reason := t.security.CheckCommand(p.Command)
		switch decision {
		case 1: // Denied
			return ToolResult{ForLLM: fmt.Sprintf("BLOCKED: %s", reason)}, nil
		case 2: // NeedsApproval
			return ToolResult{
				ForLLM:  fmt.Sprintf("REQUIRES APPROVAL: %s\nCommand: %s", reason, p.Command),
				ForUser: fmt.Sprintf("⚠️ Command requires approval: %s\nReason: %s", p.Command, reason),
			}, nil
		}
	}

	timeout := t.timeout
	if p.TimeoutSeconds > 0 {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.Command)

	// Set process group so we can kill the entire tree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// Kill process group on timeout
	if ctx.Err() == context.DeadlineExceeded && cmd.Process != nil {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	output := stdout.String()
	errOutput := stderr.String()

	// Truncate if too long
	if len(output) > maxOutputLen {
		output = output[:maxOutputLen] + "\n... [output truncated]"
	}
	if len(errOutput) > maxOutputLen {
		errOutput = errOutput[:maxOutputLen] + "\n... [stderr truncated]"
	}

	var result strings.Builder
	if output != "" {
		result.WriteString(output)
	}
	if errOutput != "" {
		if result.Len() > 0 {
			result.WriteString("\n")
		}
		result.WriteString("[stderr] " + errOutput)
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			return ToolResult{
				ForLLM: fmt.Sprintf("Command timed out after %v\n%s", timeout, result.String()),
			}, nil
		} else {
			return ToolResult{ForLLM: fmt.Sprintf("Error: %v\n%s", err, result.String())}, nil
		}
	}

	resultStr := result.String()
	if resultStr == "" {
		resultStr = fmt.Sprintf("Command completed (exit code %d)", exitCode)
	} else if exitCode != 0 {
		resultStr = fmt.Sprintf("Exit code %d:\n%s", exitCode, resultStr)
	}

	return ToolResult{ForLLM: resultStr}, nil
}
