package providers

import (
	"sync"
	"time"
)

const (
	cooldownBase = 1 * time.Minute
	cooldownMax  = 1 * time.Hour
	cooldownMult = 5
)

// CooldownTracker tracks per-provider cooldown periods with exponential backoff.
type CooldownTracker struct {
	mu       sync.Mutex
	cooldown map[string]cooldownEntry
}

type cooldownEntry struct {
	until    time.Time
	duration time.Duration // last applied cooldown duration for backoff
}

func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{
		cooldown: make(map[string]cooldownEntry),
	}
}

// MarkFailed puts a provider into cooldown with exponential backoff.
func (ct *CooldownTracker) MarkFailed(name string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	entry, exists := ct.cooldown[name]
	var dur time.Duration
	if exists && entry.duration > 0 {
		dur = entry.duration * cooldownMult
		if dur > cooldownMax {
			dur = cooldownMax
		}
	} else {
		dur = cooldownBase
	}

	ct.cooldown[name] = cooldownEntry{
		until:    time.Now().Add(dur),
		duration: dur,
	}
}

// MarkSuccess resets the cooldown for a provider.
func (ct *CooldownTracker) MarkSuccess(name string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	delete(ct.cooldown, name)
}

// InCooldown returns true if a provider is currently in cooldown.
func (ct *CooldownTracker) InCooldown(name string) bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	entry, exists := ct.cooldown[name]
	if !exists {
		return false
	}
	if time.Now().After(entry.until) {
		delete(ct.cooldown, name)
		return false
	}
	return true
}
