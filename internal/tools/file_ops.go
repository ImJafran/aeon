package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const maxFileReadSize = 100 * 1024 // 100KB

// ---- file_read ----

type FileReadTool struct{}

func NewFileRead() *FileReadTool { return &FileReadTool{} }

func (t *FileReadTool) Name() string        { return "file_read" }
func (t *FileReadTool) Description() string  { return "Read the contents of a file." }
func (t *FileReadTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative path to the file"
			},
			"offset": {
				"type": "integer",
				"description": "Line number to start reading from (1-based, optional)"
			},
			"limit": {
				"type": "integer",
				"description": "Maximum number of lines to read (optional)"
			}
		},
		"required": ["path"]
	}`)
}

type fileReadParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (t *FileReadTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p fileReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	info, err := os.Stat(p.Path)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error: %v", err)}, nil
	}
	if info.IsDir() {
		return ToolResult{ForLLM: "Error: path is a directory, not a file"}, nil
	}
	if info.Size() > maxFileReadSize {
		return ToolResult{ForLLM: fmt.Sprintf("Error: file too large (%d bytes, max %d). Use offset/limit.", info.Size(), maxFileReadSize)}, nil
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error: %v", err)}, nil
	}

	content := string(data)

	// Apply offset and limit
	if p.Offset > 0 || p.Limit > 0 {
		lines := strings.Split(content, "\n")
		start := 0
		if p.Offset > 0 {
			start = p.Offset - 1 // convert to 0-based
		}
		if start >= len(lines) {
			return ToolResult{ForLLM: "Error: offset beyond end of file"}, nil
		}
		end := len(lines)
		if p.Limit > 0 && start+p.Limit < end {
			end = start + p.Limit
		}
		lines = lines[start:end]

		// Add line numbers
		var b strings.Builder
		for i, line := range lines {
			b.WriteString(fmt.Sprintf("%4d | %s\n", start+i+1, line))
		}
		content = b.String()
	}

	return ToolResult{ForLLM: content}, nil
}

// ---- file_write ----

type FileWriteTool struct{}

func NewFileWrite() *FileWriteTool { return &FileWriteTool{} }

func (t *FileWriteTool) Name() string        { return "file_write" }
func (t *FileWriteTool) Description() string  { return "Create or overwrite a file with the given content." }
func (t *FileWriteTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Absolute or relative path to the file"
			},
			"content": {
				"type": "string",
				"description": "Content to write to the file"
			}
		},
		"required": ["path", "content"]
	}`)
}

type fileWriteParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *FileWriteTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p fileWriteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if err := os.WriteFile(p.Path, []byte(p.Content), 0644); err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error: %v", err)}, nil
	}

	return ToolResult{ForLLM: fmt.Sprintf("File written: %s (%d bytes)", p.Path, len(p.Content))}, nil
}

// ---- file_edit ----

type FileEditTool struct{}

func NewFileEdit() *FileEditTool { return &FileEditTool{} }

func (t *FileEditTool) Name() string        { return "file_edit" }
func (t *FileEditTool) Description() string  { return "Edit a file by replacing an exact string match with new content." }
func (t *FileEditTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {
				"type": "string",
				"description": "Path to the file"
			},
			"old_string": {
				"type": "string",
				"description": "The exact string to find and replace"
			},
			"new_string": {
				"type": "string",
				"description": "The replacement string"
			},
			"replace_all": {
				"type": "boolean",
				"description": "Replace all occurrences (default: false)"
			}
		},
		"required": ["path", "old_string", "new_string"]
	}`)
}

type fileEditParams struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (t *FileEditTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p fileEditParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	data, err := os.ReadFile(p.Path)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error reading file: %v", err)}, nil
	}

	content := string(data)

	if !strings.Contains(content, p.OldString) {
		return ToolResult{ForLLM: "Error: old_string not found in file"}, nil
	}

	var newContent string
	var count int
	if p.ReplaceAll {
		count = strings.Count(content, p.OldString)
		newContent = strings.ReplaceAll(content, p.OldString, p.NewString)
	} else {
		count = 1
		newContent = strings.Replace(content, p.OldString, p.NewString, 1)
	}

	if err := os.WriteFile(p.Path, []byte(newContent), 0644); err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error writing file: %v", err)}, nil
	}

	return ToolResult{ForLLM: fmt.Sprintf("Replaced %d occurrence(s) in %s", count, p.Path)}, nil
}
