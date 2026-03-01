package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Provider  ProviderConfig  `json:"provider"`
	Routing   RoutingConfig   `json:"routing"`
	Channels  ChannelsConfig  `json:"channels"`
	Security  SecurityConfig  `json:"security"`
	Skills    SkillsConfig    `json:"skills"`
	Scheduler SchedulerConfig `json:"scheduler"`
	Memory    MemoryConfig    `json:"memory"`
	Agent     AgentConfig     `json:"agent"`
	Log       LogConfig       `json:"log"`
}

type ProviderConfig struct {
	ClaudeCLI    *ClaudeCLIConfig    `json:"claude_cli,omitempty"`
	Anthropic    *AnthropicConfig    `json:"anthropic,omitempty"`
	Gemini       *GeminiConfig       `json:"gemini,omitempty"`
	ZAI          *ZAIConfig          `json:"zai,omitempty"`
	OpenAICompat *OpenAICompatConfig `json:"openai_compat,omitempty"`
}

type ClaudeCLIConfig struct {
	Enabled bool     `json:"enabled"`
	Binary  string   `json:"binary"`
	Timeout string   `json:"timeout"`
	Flags   []string `json:"flags"`
}

type AnthropicConfig struct {
	Enabled      bool   `json:"enabled"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
	FastModel    string `json:"fast_model"`
}

type GeminiConfig struct {
	Enabled      bool   `json:"enabled"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
	AudioModel   string `json:"audio_model"` // native audio (transcription/live)
	TTSModel     string `json:"tts_model"`   // text-to-speech
}

type ZAIConfig struct {
	Enabled      bool   `json:"enabled"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
}

type OpenAICompatConfig struct {
	Enabled      bool   `json:"enabled"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	DefaultModel string `json:"default_model"`
}

type RoutingConfig struct {
	Primary    string `json:"primary,omitempty"`
	Fast       string `json:"fast,omitempty"`
	Multimodal string `json:"multimodal,omitempty"`
	Fallback   string `json:"fallback,omitempty"`
}

type ChannelsConfig struct {
	Telegram  *TelegramConfig  `json:"telegram,omitempty"`
	Webhook   *WebhookConfig   `json:"webhook,omitempty"`
	WebSocket *WebSocketConfig `json:"websocket,omitempty"`
	Discord   *DiscordConfig   `json:"discord,omitempty"`
	Slack     *SlackConfig     `json:"slack,omitempty"`
	Email     *EmailConfig     `json:"email,omitempty"`
	WhatsApp  *WhatsAppConfig  `json:"whatsapp,omitempty"`
}

type TelegramConfig struct {
	Enabled      bool    `json:"enabled"`
	BotToken     string  `json:"bot_token"`
	AllowedUsers []int64 `json:"allowed_users"`
}

type WebhookConfig struct {
	Enabled    bool   `json:"enabled"`
	ListenAddr string `json:"listen_addr"`
	AuthToken  string `json:"auth_token,omitempty"`
}

type WebSocketConfig struct {
	Enabled    bool   `json:"enabled"`
	ListenAddr string `json:"listen_addr"`
	AuthToken  string `json:"auth_token,omitempty"`
}

type DiscordConfig struct {
	Enabled      bool     `json:"enabled"`
	BotToken     string   `json:"bot_token"`
	AllowedUsers []string `json:"allowed_users,omitempty"`
	MentionOnly  bool     `json:"mention_only,omitempty"`
}

type SlackConfig struct {
	Enabled      bool     `json:"enabled"`
	BotToken     string   `json:"bot_token"`
	AppToken     string   `json:"app_token"`
	AllowedUsers []string `json:"allowed_users,omitempty"`
}

type EmailConfig struct {
	Enabled      bool     `json:"enabled"`
	IMAPServer   string   `json:"imap_server"`
	SMTPServer   string   `json:"smtp_server"`
	Username     string   `json:"username"`
	Password     string   `json:"password"`
	PollInterval string   `json:"poll_interval,omitempty"`
	AllowedFrom  []string `json:"allowed_from,omitempty"`
}

type WhatsAppConfig struct {
	Enabled       bool   `json:"enabled"`
	PhoneNumberID string `json:"phone_number_id"`
	AccessToken   string `json:"access_token"`
	VerifyToken   string `json:"verify_token"`
	ListenAddr    string `json:"listen_addr,omitempty"`
}

type SecurityConfig struct {
	ApprovalTimeout string   `json:"approval_timeout,omitempty"`
	DenyPatterns    []string `json:"deny_patterns,omitempty"`
	AllowedPaths    []string `json:"allowed_paths,omitempty"`
}

type SkillsConfig struct {
	BasePackages []string `json:"base_packages,omitempty"`
	WarmPoolSize int      `json:"warm_pool_size,omitempty"`
	MaxRetries   int      `json:"max_retries,omitempty"`
}

type SchedulerConfig struct {
	MaxConcurrent          int `json:"max_concurrent,omitempty"`
	AutoPauseAfterFailures int `json:"auto_pause_after_failures,omitempty"`
}

type MemoryConfig struct {
	AutoSave            bool `json:"auto_save,omitempty"`
	CompactionThreshold int  `json:"compaction_threshold,omitempty"`
}

type AgentConfig struct {
	SystemPrompt       string `json:"system_prompt,omitempty"`
	MaxHistoryMessages int    `json:"max_history_messages,omitempty"` // max messages to load into context (default: 20)
	MaxIterations      int    `json:"max_iterations,omitempty"`       // max tool iterations per turn (default: 20)
	MaxOutputLen       int    `json:"max_output_len,omitempty"`       // max shell output chars (default: 10000)
	ShellTimeout       string `json:"shell_timeout,omitempty"`        // default shell_exec timeout (default: "30s")
	ProviderTimeout    string `json:"provider_timeout,omitempty"`     // HTTP timeout for providers (default: "120s")
	MaxTokens          int    `json:"max_tokens,omitempty"`           // max tokens for LLM response (default: 4096)
	DailyTokenLimit    int    `json:"daily_token_limit,omitempty"`    // daily token limit, 0=unlimited
	ToolTimeout        string `json:"tool_timeout,omitempty"`         // default tool execution timeout (default: "60s")
	HeartbeatInterval  string `json:"heartbeat_interval,omitempty"`   // heartbeat interval (default: "30m", empty to disable)
}

type LogConfig struct {
	Level string `json:"level,omitempty"`
	File  string `json:"file,omitempty"`
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
	return filepath.Join(AeonHome(), "config.json")
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	data = expandEnvInBytes(data)

	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
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
	if cfg.Agent.MaxHistoryMessages == 0 {
		cfg.Agent.MaxHistoryMessages = 20
	}
	if cfg.Agent.MaxIterations == 0 {
		cfg.Agent.MaxIterations = 20
	}
	if cfg.Agent.MaxOutputLen == 0 {
		cfg.Agent.MaxOutputLen = 10000
	}
	if cfg.Agent.ShellTimeout == "" {
		cfg.Agent.ShellTimeout = "30s"
	}
	if cfg.Agent.ProviderTimeout == "" {
		cfg.Agent.ProviderTimeout = "120s"
	}
	if cfg.Agent.MaxTokens == 0 {
		cfg.Agent.MaxTokens = 4096
	}
	if cfg.Agent.ToolTimeout == "" {
		cfg.Agent.ToolTimeout = "60s"
	}
	if cfg.Agent.HeartbeatInterval == "" {
		cfg.Agent.HeartbeatInterval = "30m"
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
	// Validate log level
	switch strings.ToLower(cfg.Log.Level) {
	case "debug", "info", "warn", "warning", "error":
		// valid
	default:
		return fmt.Errorf("invalid log level %q (must be debug/info/warn/error)", cfg.Log.Level)
	}

	// Validate duration strings
	durations := map[string]string{
		"security.approval_timeout":  cfg.Security.ApprovalTimeout,
		"agent.shell_timeout":        cfg.Agent.ShellTimeout,
		"agent.provider_timeout":     cfg.Agent.ProviderTimeout,
		"agent.tool_timeout":         cfg.Agent.ToolTimeout,
		"agent.heartbeat_interval":   cfg.Agent.HeartbeatInterval,
	}
	for name, val := range durations {
		if val != "" {
			if _, err := time.ParseDuration(val); err != nil {
				return fmt.Errorf("invalid duration for %s: %q (%v)", name, val, err)
			}
		}
	}

	// Validate allowed_paths are resolvable
	for _, p := range cfg.Security.AllowedPaths {
		expanded := expandHome(p)
		if _, err := filepath.Abs(expanded); err != nil {
			return fmt.Errorf("cannot resolve allowed_path %q: %v", p, err)
		}
	}

	// Warn on unexpanded env vars in API keys (starts with ${)
	if c := cfg.Provider.Anthropic; c != nil && c.Enabled && strings.HasPrefix(c.APIKey, "${") {
		return fmt.Errorf("anthropic api_key contains unexpanded env var: %s", c.APIKey)
	}
	if c := cfg.Provider.Gemini; c != nil && c.Enabled && strings.HasPrefix(c.APIKey, "${") {
		return fmt.Errorf("gemini api_key contains unexpanded env var: %s", c.APIKey)
	}
	if c := cfg.Provider.ZAI; c != nil && c.Enabled && strings.HasPrefix(c.APIKey, "${") {
		return fmt.Errorf("zai api_key contains unexpanded env var: %s", c.APIKey)
	}

	return nil
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
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
	if cfg.Provider.ZAI != nil && cfg.Provider.ZAI.Enabled {
		key := cfg.Provider.ZAI.APIKey
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
	if cfg.Provider.ZAI != nil && cfg.Provider.ZAI.Enabled {
		count++
	}
	if cfg.Provider.OpenAICompat != nil && cfg.Provider.OpenAICompat.Enabled {
		count++
	}
	return count
}
