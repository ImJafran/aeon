package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jafran/aeon/internal/skills"
)

// ---- skill_factory ----

type SkillFactoryTool struct {
	loader *skills.Loader
}

func NewSkillFactory(loader *skills.Loader) *SkillFactoryTool {
	return &SkillFactoryTool{loader: loader}
}

func (t *SkillFactoryTool) Name() string        { return "skill_factory" }
func (t *SkillFactoryTool) Description() string {
	return "Create a new skill (Python or Bash tool) that persists across sessions. The skill receives JSON params via stdin and should output JSON to stdout."
}
func (t *SkillFactoryTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Skill name (lowercase, underscores allowed)"
			},
			"description": {
				"type": "string",
				"description": "What the skill does"
			},
			"code": {
				"type": "string",
				"description": "Python or Bash source code. Must read JSON from stdin and write JSON to stdout."
			},
			"parameters": {
				"type": "object",
				"description": "Map of parameter names to {type, description} objects"
			},
			"required": {
				"type": "array",
				"items": {"type": "string"},
				"description": "List of required parameter names"
			},
			"dependencies": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Python packages to install (pip)"
			}
		},
		"required": ["name", "description", "code"]
	}`)
}

type skillFactoryParams struct {
	Name         string                      `json:"name"`
	Description  string                      `json:"description"`
	Code         string                      `json:"code"`
	Parameters   map[string]skills.Param     `json:"parameters"`
	Required     []string                    `json:"required"`
	Dependencies []string                    `json:"dependencies"`
}

func (t *SkillFactoryTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p skillFactoryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.Name == "" || p.Code == "" {
		return ToolResult{ForLLM: "Error: name and code are required"}, nil
	}

	// Create the skill
	skill, err := t.loader.CreateSkill(p.Name, p.Description, p.Code, p.Dependencies, p.Parameters, p.Required)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Error creating skill: %v", err)}, nil
	}

	// Test it
	testErr := t.loader.TestSkill(ctx, p.Name)
	if testErr != nil {
		return ToolResult{
			ForLLM: fmt.Sprintf("Skill created but test failed: %v. The skill is registered but may need fixes.", testErr),
		}, nil
	}

	return ToolResult{
		ForLLM:  fmt.Sprintf("Skill '%s' created and tested successfully at %s", skill.Meta.Name, skill.Dir),
		ForUser: fmt.Sprintf("New skill created: %s — %s", skill.Meta.Name, skill.Meta.Description),
	}, nil
}

// ---- find_skills ----

type FindSkillsTool struct {
	loader *skills.Loader
}

func NewFindSkills(loader *skills.Loader) *FindSkillsTool {
	return &FindSkillsTool{loader: loader}
}

func (t *FindSkillsTool) Name() string        { return "find_skills" }
func (t *FindSkillsTool) Description() string  { return "List all available skills (evolved tools)." }
func (t *FindSkillsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Optional search term to filter skills by name or description"
			}
		}
	}`)
}

type findSkillsParams struct {
	Query string `json:"query"`
}

func (t *FindSkillsTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p findSkillsParams
	json.Unmarshal(params, &p)

	skillList := t.loader.List()

	if len(skillList) == 0 {
		return ToolResult{ForLLM: "No skills installed. Use skill_factory to create one."}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d skills:\n", len(skillList)))

	for _, s := range skillList {
		if p.Query != "" && !containsI(s.Name, p.Query) && !containsI(s.Description, p.Query) {
			continue
		}
		status := "healthy"
		if !s.Healthy {
			status = "DISABLED"
		}
		b.WriteString(fmt.Sprintf("\n- %s: %s [%s]", s.Name, s.Description, status))
	}

	return ToolResult{ForLLM: b.String(), Silent: true}, nil
}

// ---- read_skill ----

type ReadSkillTool struct {
	loader *skills.Loader
}

func NewReadSkill(loader *skills.Loader) *ReadSkillTool {
	return &ReadSkillTool{loader: loader}
}

func (t *ReadSkillTool) Name() string        { return "read_skill" }
func (t *ReadSkillTool) Description() string  { return "Read the full details and source code of a skill." }
func (t *ReadSkillTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the skill to read"
			}
		},
		"required": ["name"]
	}`)
}

type readSkillParams struct {
	Name string `json:"name"`
}

func (t *ReadSkillTool) Execute(_ context.Context, params json.RawMessage) (ToolResult, error) {
	var p readSkillParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	skill, ok := t.loader.Get(p.Name)
	if !ok {
		return ToolResult{ForLLM: fmt.Sprintf("Skill '%s' not found.", p.Name)}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Skill: %s\n", skill.Meta.Name))
	b.WriteString(fmt.Sprintf("Description: %s\n", skill.Meta.Description))
	b.WriteString(fmt.Sprintf("Directory: %s\n", skill.Dir))
	b.WriteString(fmt.Sprintf("Entrypoint: %s\n", skill.Meta.Entrypoint))
	b.WriteString(fmt.Sprintf("Timeout: %ds\n", skill.Meta.Timeout))
	b.WriteString(fmt.Sprintf("Healthy: %v (failures: %d)\n", skill.Healthy, skill.Fails))

	if len(skill.Meta.Parameters) > 0 {
		b.WriteString("\nParameters:\n")
		for name, param := range skill.Meta.Parameters {
			req := ""
			for _, r := range skill.Meta.Required {
				if r == name {
					req = " (required)"
					break
				}
			}
			b.WriteString(fmt.Sprintf("  - %s (%s): %s%s\n", name, param.Type, param.Description, req))
		}
	}

	if len(skill.Meta.Deps) > 0 {
		b.WriteString(fmt.Sprintf("\nDependencies: %s\n", strings.Join(skill.Meta.Deps, ", ")))
	}

	// Read source code
	schema := skills.BuildJSONSchema(skill.Meta)
	b.WriteString(fmt.Sprintf("\nJSON Schema: %s\n", string(schema)))

	return ToolResult{ForLLM: b.String(), Silent: true}, nil
}

// ---- run_skill ----

type RunSkillTool struct {
	loader *skills.Loader
}

func NewRunSkill(loader *skills.Loader) *RunSkillTool {
	return &RunSkillTool{loader: loader}
}

func (t *RunSkillTool) Name() string        { return "run_skill" }
func (t *RunSkillTool) Description() string  { return "Execute an installed skill with given parameters." }
func (t *RunSkillTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the skill to run"
			},
			"params": {
				"type": "object",
				"description": "Parameters to pass to the skill (as JSON)"
			}
		},
		"required": ["name"]
	}`)
}

type runSkillParams struct {
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params"`
}

func (t *RunSkillTool) Execute(ctx context.Context, params json.RawMessage) (ToolResult, error) {
	var p runSkillParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ToolResult{}, fmt.Errorf("parsing params: %w", err)
	}

	if p.Name == "" {
		return ToolResult{ForLLM: "Error: skill name is required"}, nil
	}

	if p.Params == nil {
		p.Params = json.RawMessage(`{}`)
	}

	result, err := t.loader.Execute(ctx, p.Name, p.Params)
	if err != nil {
		return ToolResult{ForLLM: fmt.Sprintf("Skill execution error: %v", err)}, nil
	}

	// Truncate for LLM if needed
	display := result
	if len(display) > 8000 {
		display = display[:8000] + "\n... (output truncated)"
	}

	return ToolResult{ForLLM: display}, nil
}

// containsI does a case-insensitive substring check.
func containsI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
