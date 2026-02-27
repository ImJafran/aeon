package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// SkillMeta is the YAML frontmatter from SKILL.md.
type SkillMeta struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Parameters  map[string]Param  `yaml:"parameters"`
	Required    []string          `yaml:"required"`
	Deps        []string          `yaml:"deps"`
	Timeout     int               `yaml:"timeout"` // seconds, default 30
	Entrypoint  string            `yaml:"entrypoint"` // default: main.py
}

// Param describes a single parameter.
type Param struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

// Skill is a loaded, executable skill.
type Skill struct {
	Meta    SkillMeta
	Dir     string // absolute path to skill directory
	Healthy bool
	Fails   int // consecutive failure count
}

// CircuitState tracks failure counts per skill.
const maxFailures = 3

// Loader scans, loads, and executes skills.
type Loader struct {
	mu        sync.RWMutex
	skills    map[string]*Skill
	skillsDir string
	venvPath  string // path to base_venv
}

// NewLoader creates a skill loader.
func NewLoader(skillsDir, venvPath string) *Loader {
	return &Loader{
		skills:    make(map[string]*Skill),
		skillsDir: skillsDir,
		venvPath:  venvPath,
	}
}

// LoadAll scans the skills directory and loads all valid skills.
func (l *Loader) LoadAll() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entries, err := os.ReadDir(l.skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no skills directory yet
		}
		return fmt.Errorf("reading skills dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(l.skillsDir, entry.Name())
		skill, err := loadSkill(skillDir)
		if err != nil {
			continue // skip invalid skills silently
		}
		l.skills[skill.Meta.Name] = skill
	}

	return nil
}

// loadSkill parses SKILL.md frontmatter and validates the skill directory.
func loadSkill(dir string) (*Skill, error) {
	mdPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		return nil, fmt.Errorf("reading SKILL.md: %w", err)
	}

	meta, err := parseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}

	if meta.Name == "" {
		meta.Name = filepath.Base(dir)
	}
	if meta.Entrypoint == "" {
		meta.Entrypoint = "main.py"
	}
	if meta.Timeout <= 0 {
		meta.Timeout = 30
	}

	// Verify entrypoint exists
	entryPath := filepath.Join(dir, meta.Entrypoint)
	if _, err := os.Stat(entryPath); err != nil {
		return nil, fmt.Errorf("entrypoint not found: %s", entryPath)
	}

	return &Skill{
		Meta:    meta,
		Dir:     dir,
		Healthy: true,
	}, nil
}

// parseFrontmatter extracts YAML frontmatter from --- delimited blocks.
func parseFrontmatter(data []byte) (SkillMeta, error) {
	content := string(data)
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		return SkillMeta{}, fmt.Errorf("no frontmatter found")
	}

	// Find second ---
	trimmed := strings.TrimSpace(content)
	rest := trimmed[3:] // skip first ---
	end := strings.Index(rest, "---")
	if end < 0 {
		return SkillMeta{}, fmt.Errorf("unclosed frontmatter")
	}

	yamlBlock := rest[:end]
	var meta SkillMeta
	if err := yaml.Unmarshal([]byte(yamlBlock), &meta); err != nil {
		return SkillMeta{}, fmt.Errorf("parsing yaml: %w", err)
	}

	return meta, nil
}

// Get returns a skill by name.
func (l *Loader) Get(name string) (*Skill, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	s, ok := l.skills[name]
	return s, ok
}

// List returns all loaded skill names and descriptions.
func (l *Loader) List() []SkillSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var result []SkillSummary
	for _, s := range l.skills {
		result = append(result, SkillSummary{
			Name:        s.Meta.Name,
			Description: s.Meta.Description,
			Healthy:     s.Healthy,
		})
	}
	return result
}

// SkillSummary is a brief view of a skill.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Healthy     bool   `json:"healthy"`
}

// Count returns the number of loaded skills.
func (l *Loader) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.skills)
}

// Execute runs a skill as a subprocess and returns its output.
func (l *Loader) Execute(ctx context.Context, name string, params json.RawMessage) (string, error) {
	l.mu.RLock()
	skill, ok := l.skills[name]
	l.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}

	if !skill.Healthy {
		return "", fmt.Errorf("skill %s is disabled (circuit breaker: %d consecutive failures)", name, skill.Fails)
	}

	timeout := time.Duration(skill.Meta.Timeout) * time.Second
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := l.executeSkill(execCtx, skill, params)
	if err != nil {
		l.recordFailure(name)
		return "", err
	}

	l.recordSuccess(name)
	return result, nil
}

// executeSkill runs the skill subprocess.
func (l *Loader) executeSkill(ctx context.Context, skill *Skill, params json.RawMessage) (string, error) {
	entrypoint := filepath.Join(skill.Dir, skill.Meta.Entrypoint)

	var cmd *exec.Cmd

	// Determine interpreter based on entrypoint extension
	ext := filepath.Ext(entrypoint)
	switch ext {
	case ".py":
		pythonPath := l.findPython(skill)
		cmd = exec.CommandContext(ctx, pythonPath, entrypoint)
	case ".sh":
		cmd = exec.CommandContext(ctx, "sh", entrypoint)
	default:
		// Try to execute directly
		cmd = exec.CommandContext(ctx, entrypoint)
	}

	// Set environment
	cmd.Env = append(os.Environ(),
		"SKILL_DIR="+skill.Dir,
		"SKILL_NAME="+skill.Meta.Name,
	)

	// Add overlay lib to PYTHONPATH if it exists
	libDir := filepath.Join(skill.Dir, "lib")
	if _, err := os.Stat(libDir); err == nil {
		cmd.Env = append(cmd.Env, "PYTHONPATH="+libDir)
	}

	cmd.Dir = skill.Dir

	// Pass params via stdin
	cmd.Stdin = bytes.NewReader(params)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("skill execution failed: %s", truncateStr(errMsg, 500))
	}

	return stdout.String(), nil
}

// findPython returns the best Python path for a skill.
func (l *Loader) findPython(skill *Skill) string {
	// Check for base_venv python
	if l.venvPath != "" {
		venvPython := filepath.Join(l.venvPath, "bin", "python3")
		if _, err := os.Stat(venvPython); err == nil {
			return venvPython
		}
	}
	// Fallback to system python
	return "python3"
}

// recordFailure increments the failure count and disables the skill if threshold reached.
func (l *Loader) recordFailure(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	skill, ok := l.skills[name]
	if !ok {
		return
	}

	skill.Fails++
	if skill.Fails >= maxFailures {
		skill.Healthy = false
	}
}

// recordSuccess resets the failure count.
func (l *Loader) recordSuccess(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	skill, ok := l.skills[name]
	if !ok {
		return
	}

	skill.Fails = 0
}

// Register adds a skill programmatically (used by skill_factory).
func (l *Loader) Register(skill *Skill) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.skills[skill.Meta.Name] = skill
}

// CreateSkill creates a new skill from provided code and metadata.
func (l *Loader) CreateSkill(name, description, code string, deps []string, params map[string]Param, required []string) (*Skill, error) {
	skillDir := filepath.Join(l.skillsDir, name)

	// Create directory
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return nil, fmt.Errorf("creating skill dir: %w", err)
	}

	// Write SKILL.md
	meta := SkillMeta{
		Name:        name,
		Description: description,
		Parameters:  params,
		Required:    required,
		Deps:        deps,
		Timeout:     30,
		Entrypoint:  "main.py",
	}

	mdContent := buildSkillMD(meta)
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(mdContent), 0644); err != nil {
		return nil, fmt.Errorf("writing SKILL.md: %w", err)
	}

	// Write main.py
	if err := os.WriteFile(filepath.Join(skillDir, "main.py"), []byte(code), 0755); err != nil {
		return nil, fmt.Errorf("writing main.py: %w", err)
	}

	// Write requirements.txt if deps provided
	if len(deps) > 0 {
		reqContent := strings.Join(deps, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "requirements.txt"), []byte(reqContent), 0644); err != nil {
			return nil, fmt.Errorf("writing requirements.txt: %w", err)
		}

		// Install deps to overlay lib
		if err := l.installDeps(skillDir, deps); err != nil {
			// Non-fatal: skill might still work with base_venv
			_ = err
		}
	}

	skill := &Skill{
		Meta:    meta,
		Dir:     skillDir,
		Healthy: true,
	}

	l.Register(skill)
	return skill, nil
}

// installDeps installs dependencies to the skill's lib directory.
func (l *Loader) installDeps(skillDir string, deps []string) error {
	libDir := filepath.Join(skillDir, "lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return err
	}

	pythonPath := "python3"
	if l.venvPath != "" {
		vp := filepath.Join(l.venvPath, "bin", "python3")
		if _, err := os.Stat(vp); err == nil {
			pythonPath = vp
		}
	}

	args := append([]string{"-m", "pip", "install", "--target", libDir, "--quiet"}, deps...)
	cmd := exec.Command(pythonPath, args...)
	cmd.Dir = skillDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pip install failed: %s: %s", err, string(output))
	}

	return nil
}

// TestSkill runs a quick test of the skill with empty or minimal params.
func (l *Loader) TestSkill(ctx context.Context, name string) error {
	// Try executing with empty params
	_, err := l.Execute(ctx, name, json.RawMessage(`{}`))
	return err
}

// buildSkillMD generates SKILL.md content from metadata.
func buildSkillMD(meta SkillMeta) string {
	var b strings.Builder
	b.WriteString("---\n")

	yamlBytes, _ := yaml.Marshal(meta)
	b.Write(yamlBytes)

	b.WriteString("---\n\n")
	b.WriteString(fmt.Sprintf("# %s\n\n", meta.Name))
	b.WriteString(meta.Description + "\n")

	return b.String()
}

// BuildJSONSchema generates a JSON Schema for a skill's parameters.
func BuildJSONSchema(meta SkillMeta) json.RawMessage {
	props := make(map[string]interface{})
	for name, param := range meta.Parameters {
		props[name] = map[string]string{
			"type":        param.Type,
			"description": param.Description,
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(meta.Required) > 0 {
		schema["required"] = meta.Required
	}

	data, _ := json.Marshal(schema)
	return json.RawMessage(data)
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
