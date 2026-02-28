package security

// PolicyAdapter wraps Policy to implement tool-level security interfaces.
type PolicyAdapter struct {
	policy *Policy
}

func NewAdapter(p *Policy) *PolicyAdapter {
	return &PolicyAdapter{policy: p}
}

// CheckCommand returns (0=allowed, 1=denied, 2=needs_approval) and a reason string.
func (a *PolicyAdapter) CheckCommand(command string) (int, string) {
	decision, reason := a.policy.CheckCommand(command)
	return int(decision), reason
}

// CheckPath returns (0=allowed, 1=denied) and a reason string.
func (a *PolicyAdapter) CheckPath(path string) (int, string) {
	decision, reason := a.policy.CheckPath(path)
	return int(decision), reason
}

// ScrubCredentials removes sensitive data from text.
func (a *PolicyAdapter) ScrubCredentials(text string) string {
	return a.policy.ScrubCredentials(text)
}
