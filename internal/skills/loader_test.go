package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestSkillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	os.MkdirAll(skillsDir, 0755)
	return skillsDir
}

func createTestSkill(t *testing.T, skillsDir, name, code string) {
	t.Helper()
	skillDir := filepath.Join(skillsDir, name)
	os.MkdirAll(skillDir, 0755)

	md := `---
name: ` + name + `
description: A test skill
parameters:
  message:
    type: string
    description: A message
required:
  - message
timeout: 10
---

# ` + name + `

A test skill.
`
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0644)
	os.WriteFile(filepath.Join(skillDir, "main.py"), []byte(code), 0755)
}

func TestLoadAll(t *testing.T) {
	skillsDir := setupTestSkillDir(t)

	// Create a valid skill
	createTestSkill(t, skillsDir, "test_skill", `
import sys, json
params = json.load(sys.stdin)
print(json.dumps({"result": "hello " + params.get("message", "world")}))
`)

	loader := NewLoader(skillsDir, "")
	if err := loader.LoadAll(); err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if loader.Count() != 1 {
		t.Errorf("expected 1 skill, got %d", loader.Count())
	}

	skill, ok := loader.Get("test_skill")
	if !ok {
		t.Fatal("skill not found")
	}
	if skill.Meta.Description != "A test skill" {
		t.Errorf("unexpected description: %s", skill.Meta.Description)
	}
}

func TestExecuteSkill(t *testing.T) {
	skillsDir := setupTestSkillDir(t)

	createTestSkill(t, skillsDir, "echo_skill", `
import sys, json
params = json.load(sys.stdin)
result = {"echo": params.get("message", "default")}
print(json.dumps(result))
`)

	loader := NewLoader(skillsDir, "")
	loader.LoadAll()

	ctx := context.Background()
	params := json.RawMessage(`{"message": "hello"}`)
	result, err := loader.Execute(ctx, "echo_skill", params)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var output map[string]string
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("failed to parse output: %v (raw: %s)", err, result)
	}
	if output["echo"] != "hello" {
		t.Errorf("unexpected output: %v", output)
	}
}

func TestCircuitBreaker(t *testing.T) {
	skillsDir := setupTestSkillDir(t)

	// Create a skill that always fails
	createTestSkill(t, skillsDir, "broken_skill", `
import sys
sys.exit(1)
`)

	loader := NewLoader(skillsDir, "")
	loader.LoadAll()

	ctx := context.Background()
	params := json.RawMessage(`{}`)

	// Execute until circuit breaker trips
	for i := 0; i < maxFailures; i++ {
		loader.Execute(ctx, "broken_skill", params)
	}

	// Next call should be blocked by circuit breaker
	_, err := loader.Execute(ctx, "broken_skill", params)
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
	if !contains(err.Error(), "circuit breaker") {
		t.Errorf("expected circuit breaker error, got: %v", err)
	}
}

func TestList(t *testing.T) {
	skillsDir := setupTestSkillDir(t)

	createTestSkill(t, skillsDir, "skill_a", `print("a")`)
	createTestSkill(t, skillsDir, "skill_b", `print("b")`)

	loader := NewLoader(skillsDir, "")
	loader.LoadAll()

	list := loader.List()
	if len(list) != 2 {
		t.Errorf("expected 2 skills, got %d", len(list))
	}
}

func TestCreateSkill(t *testing.T) {
	skillsDir := setupTestSkillDir(t)
	loader := NewLoader(skillsDir, "")

	code := `
import sys, json
params = json.load(sys.stdin)
print(json.dumps({"status": "ok"}))
`

	params := map[string]Param{
		"url": {Type: "string", Description: "URL to check"},
	}

	skill, err := loader.CreateSkill("site_checker", "Checks if a site is up", code, nil, params, []string{"url"})
	if err != nil {
		t.Fatalf("CreateSkill failed: %v", err)
	}

	if skill.Meta.Name != "site_checker" {
		t.Errorf("unexpected name: %s", skill.Meta.Name)
	}

	// Verify files exist
	if _, err := os.Stat(filepath.Join(skillsDir, "site_checker", "SKILL.md")); err != nil {
		t.Error("SKILL.md not created")
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "site_checker", "main.py")); err != nil {
		t.Error("main.py not created")
	}

	// Verify it was registered
	if loader.Count() != 1 {
		t.Errorf("expected 1 skill after create, got %d", loader.Count())
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantName string
	}{
		{
			name: "valid frontmatter",
			input: `---
name: test
description: A test
---
# Test`,
			wantName: "test",
		},
		{
			name:    "no frontmatter",
			input:   "# Just a heading",
			wantErr: true,
		},
		{
			name:    "unclosed frontmatter",
			input:   "---\nname: test\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := parseFrontmatter([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if meta.Name != tt.wantName {
				t.Errorf("got name %q, want %q", meta.Name, tt.wantName)
			}
		})
	}
}

func TestBuildJSONSchema(t *testing.T) {
	meta := SkillMeta{
		Parameters: map[string]Param{
			"url":     {Type: "string", Description: "The URL"},
			"timeout": {Type: "integer", Description: "Timeout in seconds"},
		},
		Required: []string{"url"},
	}

	schema := BuildJSONSchema(meta)
	var parsed map[string]interface{}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("invalid schema JSON: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("expected type object, got %v", parsed["type"])
	}

	props := parsed["properties"].(map[string]interface{})
	if len(props) != 2 {
		t.Errorf("expected 2 properties, got %d", len(props))
	}
}

func TestExecuteShellSkill(t *testing.T) {
	skillsDir := setupTestSkillDir(t)
	skillDir := filepath.Join(skillsDir, "shell_skill")
	os.MkdirAll(skillDir, 0755)

	md := `---
name: shell_skill
description: A shell skill
entrypoint: main.sh
timeout: 10
---

# shell_skill
`
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0644)
	os.WriteFile(filepath.Join(skillDir, "main.sh"), []byte(`#!/bin/sh
echo '{"result": "shell works"}'
`), 0755)

	loader := NewLoader(skillsDir, "")
	loader.LoadAll()

	ctx := context.Background()
	result, err := loader.Execute(ctx, "shell_skill", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var output map[string]string
	if err := json.Unmarshal([]byte(result), &output); err != nil {
		t.Fatalf("failed to parse output: %v (raw: %s)", err, result)
	}
	if output["result"] != "shell works" {
		t.Errorf("unexpected output: %v", output)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
