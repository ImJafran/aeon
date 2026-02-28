package security

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Decision int

const (
	Allowed Decision = iota
	Denied
	NeedsApproval
)

func (d Decision) String() string {
	switch d {
	case Allowed:
		return "allowed"
	case Denied:
		return "denied"
	case NeedsApproval:
		return "needs_approval"
	default:
		return "unknown"
	}
}

type Policy struct {
	denyPatterns    []*regexp.Regexp
	approvePatterns []*regexp.Regexp
	allowedPaths    []string
	credPatterns    []*regexp.Regexp
}

func NewPolicy(denyPatterns []string, allowedPaths []string) *Policy {
	p := &Policy{
		allowedPaths: allowedPaths,
	}

	// DENY — only truly catastrophic / self-destructive commands
	defaultDeny := []string{
		`rm\s+-rf\s+/\s*$`,                          // rm -rf / (root filesystem wipe)
		`rm\s+-rf\s+/\*`,                             // rm -rf /*
		`mkfs\.\w+\s+/dev/[sv]d`,                     // format disk
		`dd\s+if=.*of=/dev/[sv]d`,                    // overwrite disk
		`:\(\)\s*\{\s*:\|:&\s*\}\s*;`,                // fork bomb
		`>\s*/dev/[sv]d`,                              // redirect to disk device
		// Self-termination — agent must not kill itself
		`systemctl\s+(stop|kill|disable)\s+aeon`,
		`service\s+aeon\s+stop`,
		`pkill.*aeon`,
		`killall.*aeon`,
	}

	// APPROVE — things that benefit from user awareness (not blocking)
	defaultApprove := []string{
		`curl\s.*\|\s*(sh|bash)`,  // pipe-to-shell
		`wget\s.*\|\s*(sh|bash)`,  // pipe-to-shell
	}

	for _, pat := range append(defaultDeny, denyPatterns...) {
		if re, err := regexp.Compile(pat); err == nil {
			p.denyPatterns = append(p.denyPatterns, re)
		}
	}

	for _, pat := range defaultApprove {
		if re, err := regexp.Compile(pat); err == nil {
			p.approvePatterns = append(p.approvePatterns, re)
		}
	}

	// Credential patterns for scrubbing (output only — does not block execution)
	credentialPatterns := []string{
		// Generic key=value
		`(?i)(api[_-]?key|apikey|api_secret)\s*[:=]\s*['"]?([a-zA-Z0-9_\-]{20,})`,
		`(?i)(token|bearer|auth)\s*[:=]\s*['"]?([a-zA-Z0-9_\-\.]{20,})`,
		`(?i)(password|passwd|pwd)\s*[:=]\s*['"]?(\S{6,})`,
		// Provider-specific
		`sk-ant-[a-zA-Z0-9\-_]{20,}`,            // Anthropic
		`sk-[a-zA-Z0-9]{20,}`,                    // OpenAI
		`ghp_[a-zA-Z0-9]{36}`,                    // GitHub PAT
		`glpat-[a-zA-Z0-9\-]{20,}`,               // GitLab PAT
		`AIza[a-zA-Z0-9_\-]{35}`,                  // Google API key
		`xox[bps]-[a-zA-Z0-9\-]+`,                 // Slack
		`AKIA[0-9A-Z]{16}`,                        // AWS access key ID
		`[0-9]{8,10}:[a-zA-Z0-9_\-]{35}`,          // Telegram bot token
		`sk_live_[a-zA-Z0-9]{24,}`,                // Stripe live key
		// Structured secrets
		`eyJ[a-zA-Z0-9_\-]{10,}\.[a-zA-Z0-9_\-]{10,}\.[a-zA-Z0-9_\-]{10,}`, // JWT
		`-----BEGIN\s+(RSA\s+|EC\s+|OPENSSH\s+)?PRIVATE\s+KEY-----`,          // Private keys
		`(?i)(postgres|mysql|mongodb)://[^\s]+:[^\s]+@`,                       // DB connection strings
		`(?i)aws_secret_access_key\s*[:=]\s*['"]?([a-zA-Z0-9/+=]{40})`,       // AWS secret
	}

	for _, pat := range credentialPatterns {
		if re, err := regexp.Compile(pat); err == nil {
			p.credPatterns = append(p.credPatterns, re)
		}
	}

	return p
}

// CheckCommand evaluates a shell command against security policy.
func (p *Policy) CheckCommand(command string) (Decision, string) {
	cmd := strings.TrimSpace(command)

	// Check deny patterns first
	for _, re := range p.denyPatterns {
		if re.MatchString(cmd) {
			return Denied, fmt.Sprintf("Command blocked (matched: %s)", re.String())
		}
	}

	// Check approval-needed patterns
	for _, re := range p.approvePatterns {
		if re.MatchString(cmd) {
			return NeedsApproval, fmt.Sprintf("Command requires approval (matched: %s)", re.String())
		}
	}

	return Allowed, ""
}

// CheckPath validates that a file path is within allowed boundaries.
func (p *Policy) CheckPath(path string) (Decision, string) {
	if len(p.allowedPaths) == 0 {
		return Allowed, ""
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Denied, fmt.Sprintf("Cannot resolve path: %v", err)
	}

	// Resolve symlinks
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	for _, allowed := range p.allowedPaths {
		expandedAllowed := expandHome(allowed)
		absAllowed, err := filepath.Abs(expandedAllowed)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(absAllowed); err == nil {
			absAllowed = resolved
		}
		if strings.HasPrefix(absPath, absAllowed) {
			return Allowed, ""
		}
	}

	return Denied, fmt.Sprintf("Path %s is outside allowed directories", absPath)
}

// ScrubCredentials removes API keys, tokens, and passwords from text.
func (p *Policy) ScrubCredentials(text string) string {
	result := text
	for _, re := range p.credPatterns {
		result = re.ReplaceAllStringFunc(result, func(match string) string {
			return "[REDACTED]"
		})
	}
	return result
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
