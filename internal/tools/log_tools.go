package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ImJafran/aeon/internal/config"
)

// ---- log_read ----

type LogReadTool struct{}

func NewLogRead() *LogReadTool { return &LogReadTool{} }

func (t *LogReadTool) Name() string        { return "log_read" }
func (t *LogReadTool) Description() string { return "Read your own log file. Use to diagnose errors, check provider latency, or review recent activity." }
func (t *LogReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"lines": {
				"type": "integer",
				"description": "Number of lines to read from the end of the log (default: 50, max: 200)"
			},
			"filter": {
				"type": "string",
				"description": "Filter log lines containing this string (e.g. 'ERROR', 'provider', 'tool')"
			}
		}
	}`)
}

type logReadParams struct {
	Lines  int    `json:"lines"`
	Filter string `json:"filter"`
}

func (t *LogReadTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p logReadParams
	json.Unmarshal(params, &p)

	if p.Lines <= 0 {
		p.Lines = 50
	}
	if p.Lines > 200 {
		p.Lines = 200
	}

	logPath := filepath.Join(config.AeonHome(), "logs", "aeon.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error reading log: %v", err)}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Apply filter if specified
	if p.Filter != "" {
		filterLower := strings.ToLower(p.Filter)
		var filtered []string
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), filterLower) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}

	// Take last N lines
	if len(lines) > p.Lines {
		lines = lines[len(lines)-p.Lines:]
	}

	if len(lines) == 0 {
		return ToolResult{ForLLM: "No matching log entries found.", Silent: true}, nil
	}

	output := strings.Join(lines, "\n")
	if len(output) > 8000 {
		output = output[len(output)-8000:]
	}

	return ToolResult{ForLLM: output, Silent: true}, nil
}
