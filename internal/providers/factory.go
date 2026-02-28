package providers

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/ImJafran/aeon/internal/config"
)

// FromConfig creates a ProviderChain from configuration.
func FromConfig(cfg *config.Config, logger *slog.Logger) (Provider, error) {
	available := make(map[string]Provider)

	// Build all enabled providers
	if c := cfg.Provider.ClaudeCLI; c != nil && c.Enabled {
		timeout := 120 * time.Second
		if c.Timeout != "" {
			if d, err := time.ParseDuration(c.Timeout); err == nil {
				timeout = d
			}
		}
		p := NewClaudeCLI(c.Binary, c.Flags, timeout)
		if p.Available() {
			available["claude_cli"] = p
			logger.Info("provider enabled", "name", "claude_cli")
		} else {
			logger.Warn("claude_cli configured but binary not found", "binary", c.Binary)
		}
	}

	if c := cfg.Provider.Anthropic; c != nil && c.Enabled && c.APIKey != "" {
		p := NewAnthropic(c.APIKey, c.DefaultModel)
		available["anthropic"] = p
		logger.Info("provider enabled", "name", "anthropic", "model", c.DefaultModel)

		// Also create a fast variant if fast_model specified
		if c.FastModel != "" {
			fast := NewAnthropic(c.APIKey, c.FastModel)
			available["anthropic_fast"] = fast
			logger.Info("provider enabled", "name", "anthropic_fast", "model", c.FastModel)
		}
	}

	if c := cfg.Provider.Gemini; c != nil && c.Enabled && c.APIKey != "" {
		p := NewOpenAICompat(
			"https://generativelanguage.googleapis.com/v1beta/openai",
			c.APIKey,
			c.DefaultModel,
		)
		available["gemini"] = p
		logger.Info("provider enabled", "name", "gemini", "model", c.DefaultModel)
	}

	if c := cfg.Provider.ZAI; c != nil && c.Enabled && c.APIKey != "" {
		p := NewOpenAICompat(
			"https://api.z.ai/api/coding/paas/v4",
			c.APIKey,
			c.DefaultModel,
		)
		available["zai"] = p
		logger.Info("provider enabled", "name", "zai", "model", c.DefaultModel)
	}

	if c := cfg.Provider.OpenAICompat; c != nil && c.Enabled && c.BaseURL != "" {
		p := NewOpenAICompat(c.BaseURL, c.APIKey, c.DefaultModel)
		available["openai_compat"] = p
		logger.Info("provider enabled", "name", "openai_compat", "model", c.DefaultModel)
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("no providers available. Configure at least one in config.yaml")
	}

	// If only one provider, use it for everything
	if len(available) == 1 {
		for _, p := range available {
			logger.Info("single provider mode", "provider", p.Name())
			return p, nil
		}
	}

	// Build chain from routing config
	chainCfg := ChainConfig{}

	resolve := func(name string) Provider {
		if p, ok := available[name]; ok {
			return p
		}
		return nil
	}

	chainCfg.Primary = resolve(cfg.Routing.Primary)
	chainCfg.Fast = resolve(cfg.Routing.Fast)
	if chainCfg.Fast == nil {
		chainCfg.Fast = resolve("anthropic_fast")
	}
	chainCfg.Multimodal = resolve(cfg.Routing.Multimodal)
	chainCfg.Fallback = resolve(cfg.Routing.Fallback)

	// If no explicit primary, pick the first available
	if chainCfg.Primary == nil {
		for _, name := range []string{"zai", "claude_cli", "anthropic", "gemini", "openai_compat"} {
			if p, ok := available[name]; ok {
				chainCfg.Primary = p
				break
			}
		}
	}

	chain := NewChain(chainCfg, logger)
	chain.SetAll(available)
	logger.Info("provider chain configured",
		"primary", chain.PrimaryName(),
		"total_providers", len(available),
	)

	return chain, nil
}
