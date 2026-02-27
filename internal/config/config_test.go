package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
provider:
  anthropic:
    enabled: true
    api_key: "sk-test-key-1234567890"
    default_model: "claude-sonnet-4-20250514"
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("failed to load valid config: %v", err)
	}
	if cfg.Provider.Anthropic == nil || !cfg.Provider.Anthropic.Enabled {
		t.Error("expected anthropic provider to be enabled")
	}
}

func TestLoadConfigNoProvider(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
log:
  level: info
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Error("expected validation error for config with no provider")
	}
}

func TestEnvVarExpansion(t *testing.T) {
	os.Setenv("TEST_AEON_KEY", "my-secret-key")
	defer os.Unsetenv("TEST_AEON_KEY")

	result := expandEnvVars("key: ${TEST_AEON_KEY}")
	if result != "key: my-secret-key" {
		t.Errorf("expected expansion, got: %s", result)
	}
}

func TestEnvVarNoExpansion(t *testing.T) {
	result := expandEnvVars("key: ${NONEXISTENT_VAR}")
	if result != "key: ${NONEXISTENT_VAR}" {
		t.Errorf("expected no expansion, got: %s", result)
	}
}

func TestDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
provider:
  claude_cli:
    enabled: true
`
	os.WriteFile(cfgPath, []byte(content), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if cfg.Security.ApprovalTimeout != "60s" {
		t.Errorf("expected default approval timeout 60s, got %s", cfg.Security.ApprovalTimeout)
	}
	if cfg.Scheduler.MaxConcurrent != 3 {
		t.Errorf("expected default max concurrent 3, got %d", cfg.Scheduler.MaxConcurrent)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("expected default log level info, got %s", cfg.Log.Level)
	}
}

func TestEnabledProviderCount(t *testing.T) {
	cfg := &Config{
		Provider: ProviderConfig{
			Anthropic: &AnthropicConfig{Enabled: true, APIKey: "key"},
			Gemini:    &GeminiConfig{Enabled: true, APIKey: "key"},
		},
	}

	count := EnabledProviderCount(cfg)
	if count != 2 {
		t.Errorf("expected 2 providers, got %d", count)
	}
}

func TestAeonHome(t *testing.T) {
	// Test with AEON_HOME set
	os.Setenv("AEON_HOME", "/tmp/test-aeon")
	defer os.Unsetenv("AEON_HOME")

	home := AeonHome()
	if home != "/tmp/test-aeon" {
		t.Errorf("expected /tmp/test-aeon, got %s", home)
	}
}
