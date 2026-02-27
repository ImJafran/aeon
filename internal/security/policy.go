package security

import (
	"fmt"
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

	// Compile deny patterns
	defaultDeny := []string{
		`rm\s+(-[a-zA-Z]*f[a-zA-Z]*\s+)?/`,
		`rm\s+-rf\s`,
		`mkfs`,
		`dd\s+if=`,
		`:\(\)\s*\{\s*:\|:&\s*\}\s*;`,
		`chmod\s+777`,
		`shutdown`,
		`reboot`,
		`>\s*/dev/sd`,
		`/etc/passwd`,
		`/etc/shadow`,
	}

	// Patterns that need approval (not outright denied)
	defaultApprove := []string{
		`sudo\s`,
		`apt\s+install`,
		`apt-get\s+install`,
		`yum\s+install`,
		`dnf\s+install`,
		`pip\s+install`,
		`npm\s+install\s+-g`,
		`curl\s.*\|\s*sh`,
		`curl\s.*\|\s*bash`,
		`wget\s.*\|\s*sh`,
		`wget\s.*\|\s*bash`,
		`eval\s`,
		`docker\s+rm`,
		`docker\s+system\s+prune`,
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

	// Credential patterns for scrubbing
	credentialPatterns := []string{
		`(?i)(api[_-]?key|apikey|api_secret)\s*[:=]\s*['"]?([a-zA-Z0-9_\-]{20,})`,
		`(?i)(token|bearer|auth)\s*[:=]\s*['"]?([a-zA-Z0-9_\-\.]{20,})`,
		`(?i)(password|passwd|pwd)\s*[:=]\s*['"]?(\S{6,})`,
		`sk-[a-zA-Z0-9]{20,}`,
		`ghp_[a-zA-Z0-9]{36}`,
		`glpat-[a-zA-Z0-9\-]{20,}`,
		`AIza[a-zA-Z0-9_\-]{35}`,
		`xox[bps]-[a-zA-Z0-9\-]+`,
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
			return Denied, fmt.Sprintf("Command blocked by security policy (matched: %s)", re.String())
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

	for _, allowed := range p.allowedPaths {
		expandedAllowed := expandHome(allowed)
		absAllowed, err := filepath.Abs(expandedAllowed)
		if err != nil {
			continue
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
			if len(match) > 8 {
				return match[:4] + "****[REDACTED]"
			}
			return "****[REDACTED]"
		})
	}
	return result
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := filepath.Abs(filepath.Join("$HOME")); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
