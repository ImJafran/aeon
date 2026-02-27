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
	Agent     AgentConfig     `yaml:"agent"`
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
	AudioModel   string `yaml:"audio_model"` // native audio (transcription/live)
	TTSModel     string `yaml:"tts_model"`   // text-to-speech
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

type AgentConfig struct {
	SystemPrompt string `yaml:"system_prompt"`
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
	if cfg.Agent.SystemPrompt == "" {
		cfg.Agent.SystemPrompt = `You are Aeon, a persistent AI assistant on the user's system. You have tools — use them, don't describe them.

Rules:
- Be brief. 1-3 sentences max. No filler, no emojis, no sign-offs.
- Act first, explain only if asked.
- Save important facts with memory_store (names, preferences, decisions, project details).
- Check memory_recall before asking the user to repeat themselves.
- For complex tasks, use spawn_agent to parallelize.`
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

// NoProvider is returned when no LLM provider is configured.
// It's a warning, not a fatal error — Aeon runs in echo mode without providers.
var NoProvider = fmt.Errorf("no LLM provider configured — running in echo mode")

func validate(cfg *Config) error {
	// No longer fatal — agent loop handles nil provider with echo mode
	return nil
}

// HasProvider returns true if at least one provider is configured.
func HasProvider(cfg *Config) bool {
	return hasAnyProvider(cfg)
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
