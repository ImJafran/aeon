package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Provider  ProviderConfig  `yaml:"provider"`
	Routing   RoutingConfig   `yaml:"routing"`
	Channels  ChannelsConfig  `yaml:"channels"`
	Security  SecurityConfig  `yaml:"security"`
	Skills    SkillsConfig    `yaml:"skills"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
	Memory    MemoryConfig    `yaml:"memory"`
	Log       LogConfig       `yaml:"log"`
}

type ProviderConfig struct {
	ClaudeCLI    *ClaudeCLIConfig    `yaml:"claude_cli"`
	Anthropic    *AnthropicConfig    `yaml:"anthropic"`
	Gemini       *GeminiConfig       `yaml:"gemini"`
	OpenAICompat *OpenAICompatConfig `yaml:"openai_compat"`
}

type ClaudeCLIConfig struct {
	Enabled bool     `yaml:"enabled"`
	Binary  string   `yaml:"binary"`
	Timeout string   `yaml:"timeout"`
	Flags   []string `yaml:"flags"`
}

type AnthropicConfig struct {
	Enabled      bool   `yaml:"enabled"`
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
	FastModel    string `yaml:"fast_model"`
}

type GeminiConfig struct {
	Enabled      bool   `yaml:"enabled"`
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
}

type OpenAICompatConfig struct {
	Enabled      bool   `yaml:"enabled"`
	BaseURL      string `yaml:"base_url"`
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
}

type RoutingConfig struct {
	Primary    string `yaml:"primary"`
	Fast       string `yaml:"fast"`
	Multimodal string `yaml:"multimodal"`
	Fallback   string `yaml:"fallback"`
}

type ChannelsConfig struct {
	Telegram *TelegramConfig `yaml:"telegram"`
}

type TelegramConfig struct {
	Enabled      bool    `yaml:"enabled"`
	BotToken     string  `yaml:"bot_token"`
	AllowedUsers []int64 `yaml:"allowed_users"`
}

type SecurityConfig struct {
	ApprovalTimeout string   `yaml:"approval_timeout"`
	DenyPatterns    []string `yaml:"deny_patterns"`
	AllowedPaths    []string `yaml:"allowed_paths"`
}

type SkillsConfig struct {
	BasePackages []string `yaml:"base_packages"`
	WarmPoolSize int      `yaml:"warm_pool_size"`
	MaxRetries   int      `yaml:"max_retries"`
}

type SchedulerConfig struct {
	MaxConcurrent          int `yaml:"max_concurrent"`
	AutoPauseAfterFailures int `yaml:"auto_pause_after_failures"`
}

type MemoryConfig struct {
	AutoSave              bool `yaml:"auto_save"`
	CompactionThreshold   int  `yaml:"compaction_threshold"`
}

type LogConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
}

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		return match
	})
}

func expandEnvInBytes(data []byte) []byte {
	return []byte(expandEnvVars(string(data)))
}

func AeonHome() string {
	if h := os.Getenv("AEON_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".aeon")
	}
	return filepath.Join(home, ".aeon")
}

func DefaultConfigPath() string {
	return filepath.Join(AeonHome(), "config.yaml")
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	data = expandEnvInBytes(data)

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	applyDefaults(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Security.ApprovalTimeout == "" {
		cfg.Security.ApprovalTimeout = "60s"
	}
	if cfg.Skills.WarmPoolSize == 0 {
		cfg.Skills.WarmPoolSize = 3
	}
	if cfg.Skills.MaxRetries == 0 {
		cfg.Skills.MaxRetries = 3
	}
	if cfg.Scheduler.MaxConcurrent == 0 {
		cfg.Scheduler.MaxConcurrent = 3
	}
	if cfg.Scheduler.AutoPauseAfterFailures == 0 {
		cfg.Scheduler.AutoPauseAfterFailures = 5
	}
	if cfg.Memory.CompactionThreshold == 0 {
		cfg.Memory.CompactionThreshold = 10
	}
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.File == "" {
		cfg.Log.File = filepath.Join(AeonHome(), "logs", "aeon.log")
	}
	if len(cfg.Skills.BasePackages) == 0 {
		cfg.Skills.BasePackages = []string{"requests", "httpx", "beautifulsoup4", "pyyaml"}
	}
}

func validate(cfg *Config) error {
	if !hasAnyProvider(cfg) {
		return fmt.Errorf("at least one LLM provider must be configured. Run 'aeon init' for setup")
	}
	return nil
}

func hasAnyProvider(cfg *Config) bool {
	if cfg.Provider.ClaudeCLI != nil && cfg.Provider.ClaudeCLI.Enabled {
		return true
	}
	if cfg.Provider.Anthropic != nil && cfg.Provider.Anthropic.Enabled {
		key := cfg.Provider.Anthropic.APIKey
		return key != "" && !strings.HasPrefix(key, "${")
	}
	if cfg.Provider.Gemini != nil && cfg.Provider.Gemini.Enabled {
		key := cfg.Provider.Gemini.APIKey
		return key != "" && !strings.HasPrefix(key, "${")
	}
	if cfg.Provider.OpenAICompat != nil && cfg.Provider.OpenAICompat.Enabled {
		return cfg.Provider.OpenAICompat.BaseURL != ""
	}
	return false
}

func EnabledProviderCount(cfg *Config) int {
	count := 0
	if cfg.Provider.ClaudeCLI != nil && cfg.Provider.ClaudeCLI.Enabled {
		count++
	}
	if cfg.Provider.Anthropic != nil && cfg.Provider.Anthropic.Enabled {
		count++
	}
	if cfg.Provider.Gemini != nil && cfg.Provider.Gemini.Enabled {
		count++
	}
	if cfg.Provider.OpenAICompat != nil && cfg.Provider.OpenAICompat.Enabled {
		count++
	}
	return count
}
