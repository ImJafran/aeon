package providers

import "testing"

func TestCooldownTracker(t *testing.T) {
	ct := NewCooldownTracker()

	// Initially not in cooldown
	if ct.InCooldown("test") {
		t.Error("should not be in cooldown initially")
	}

	// Mark failed → should be in cooldown
	ct.MarkFailed("test")
	if !ct.InCooldown("test") {
		t.Error("should be in cooldown after failure")
	}

	// Mark success → should clear cooldown
	ct.MarkSuccess("test")
	if ct.InCooldown("test") {
		t.Error("should not be in cooldown after success")
	}

	// Different providers are independent
	ct.MarkFailed("provider_a")
	if ct.InCooldown("provider_b") {
		t.Error("provider_b should not be in cooldown")
	}
	if !ct.InCooldown("provider_a") {
		t.Error("provider_a should be in cooldown")
	}
}
