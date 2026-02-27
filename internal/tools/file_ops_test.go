package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileReadWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	// Write
	writeTool := NewFileWrite()
	writeParams, _ := json.Marshal(fileWriteParams{Path: path, Content: "hello world\nline 2\nline 3"})
	result, err := writeTool.Execute(context.Background(), writeParams)
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "File written") {
		t.Errorf("expected success message, got: %s", result.ForLLM)
	}

	// Read
	readTool := NewFileRead()
	readParams, _ := json.Marshal(fileReadParams{Path: path})
	result, err = readTool.Execute(context.Background(), readParams)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "hello world") {
		t.Errorf("expected file content, got: %s", result.ForLLM)
	}
}

func TestFileReadWithOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\nline4\nline5"), 0644)

	readTool := NewFileRead()
	params, _ := json.Marshal(fileReadParams{Path: path, Offset: 2, Limit: 2})
	result, err := readTool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "line2") || !strings.Contains(result.ForLLM, "line3") {
		t.Errorf("expected lines 2-3, got: %s", result.ForLLM)
	}
	if strings.Contains(result.ForLLM, "line4") {
		t.Errorf("should not contain line4, got: %s", result.ForLLM)
	}
}

func TestFileEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	editTool := NewFileEdit()
	params, _ := json.Marshal(fileEditParams{
		Path:      path,
		OldString: "world",
		NewString: "aeon",
	})
	result, err := editTool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("edit error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "Replaced 1") {
		t.Errorf("expected replace confirmation, got: %s", result.ForLLM)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello aeon" {
		t.Errorf("expected 'hello aeon', got: %s", string(data))
	}
}

func TestFileEditNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	editTool := NewFileEdit()
	params, _ := json.Marshal(fileEditParams{
		Path:      path,
		OldString: "nonexistent",
		NewString: "replacement",
	})
	result, err := editTool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("edit error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "not found") {
		t.Errorf("expected not found error, got: %s", result.ForLLM)
	}
}

func TestFileReadDirectory(t *testing.T) {
	dir := t.TempDir()
	readTool := NewFileRead()
	params, _ := json.Marshal(fileReadParams{Path: dir})
	result, err := readTool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if !strings.Contains(result.ForLLM, "directory") {
		t.Errorf("expected directory error, got: %s", result.ForLLM)
	}
}
