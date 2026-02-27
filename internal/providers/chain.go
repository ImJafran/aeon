package providers

import (
	"context"
	"fmt"
	"log/slog"
)

// ProviderChain routes requests to the appropriate provider based on hints and availability.
type ProviderChain struct {
	primary    Provider
	fast       Provider
	multimodal Provider
	fallback   Provider
	logger     *slog.Logger
}

type ChainConfig struct {
	Primary    Provider
	Fast       Provider
	Multimodal Provider
	Fallback   Provider
}

func NewChain(cfg ChainConfig, logger *slog.Logger) *ProviderChain {
	chain := &ProviderChain{
		primary:    cfg.Primary,
		fast:       cfg.Fast,
		multimodal: cfg.Multimodal,
		fallback:   cfg.Fallback,
		logger:     logger,
	}

	// If roles are unset, fall back to primary for everything
	if chain.fast == nil {
		chain.fast = chain.primary
	}
	if chain.multimodal == nil {
		chain.multimodal = chain.primary
	}
	if chain.fallback == nil {
		chain.fallback = chain.primary
	}

	return chain
}

func (c *ProviderChain) Name() string {
	if c.primary != nil {
		return "chain:" + c.primary.Name()
	}
	return "chain:none"
}

func (c *ProviderChain) Available() bool {
	return c.primary != nil && c.primary.Available()
}

func (c *ProviderChain) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	provider := c.selectProvider(req.Hint)
	if provider == nil {
		return CompletionResponse{}, fmt.Errorf("no available provider for hint=%q", req.Hint)
	}

	c.logger.Debug("routing request", "provider", provider.Name(), "hint", req.Hint)

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		// Try fallback
		if c.fallback != nil && c.fallback != provider && c.fallback.Available() {
			c.logger.Warn("primary provider failed, trying fallback",
				"primary", provider.Name(),
				"fallback", c.fallback.Name(),
				"error", err,
			)
			return c.fallback.Complete(ctx, req)
		}
		return CompletionResponse{}, err
	}

	return resp, nil
}

func (c *ProviderChain) selectProvider(hint string) Provider {
	switch hint {
	case "fast":
		if c.fast != nil && c.fast.Available() {
			return c.fast
		}
	case "multimodal":
		if c.multimodal != nil && c.multimodal.Available() {
			return c.multimodal
		}
	}

	// Default to primary
	if c.primary != nil && c.primary.Available() {
		return c.primary
	}

	// Last resort: fallback
	if c.fallback != nil && c.fallback.Available() {
		return c.fallback
	}

	return nil
}

func (c *ProviderChain) PrimaryName() string {
	if c.primary != nil {
		return c.primary.Name()
	}
	return "none"
}
