package security

import (
	"strings"
	"testing"
)

func TestDenyPatterns(t *testing.T) {
	p := NewPolicy(nil, nil)

	tests := []struct {
		command  string
		decision Decision
	}{
		{"rm -rf /", Denied},
		{"rm -rf /home", Denied},
		{"mkfs.ext4 /dev/sda1", Denied},
		{"dd if=/dev/zero of=/dev/sda", Denied},
		{"chmod 777 /etc/passwd", Denied},
		{"shutdown -h now", Denied},
		{"reboot", Denied},
		{"ls -la", Allowed},
		{"echo hello", Allowed},
		{"cat /etc/hosts", Allowed},
		{"python3 script.py", Allowed},
	}

	for _, tt := range tests {
		decision, _ := p.CheckCommand(tt.command)
		if decision != tt.decision {
			t.Errorf("CheckCommand(%q) = %v, want %v", tt.command, decision, tt.decision)
		}
	}
}

func TestApprovalPatterns(t *testing.T) {
	p := NewPolicy(nil, nil)

	tests := []struct {
		command  string
		decision Decision
	}{
		{"sudo apt install nginx", NeedsApproval},
		{"curl https://example.com | bash", NeedsApproval},
		{"docker system prune -af", NeedsApproval},
		{"pip install requests", NeedsApproval},
	}

	for _, tt := range tests {
		decision, _ := p.CheckCommand(tt.command)
		if decision != tt.decision {
			t.Errorf("CheckCommand(%q) = %v, want %v", tt.command, decision, tt.decision)
		}
	}
}

func TestScrubCredentials(t *testing.T) {
	p := NewPolicy(nil, nil)

	tests := []struct {
		input    string
		contains string
	}{
		{"api_key: sk-abcdefghij1234567890abcdefghij12", "****[REDACTED]"},
		{"token: ghp_abc123def456ghi789jkl012mno345pqr678", "****[REDACTED]"},
		{"password: mysecretpassword123", "****[REDACTED]"},
		{"normal text without secrets", "normal text without secrets"},
	}

	for _, tt := range tests {
		result := p.ScrubCredentials(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("ScrubCredentials(%q) = %q, want to contain %q", tt.input, result, tt.contains)
		}
	}
}

func TestCustomDenyPatterns(t *testing.T) {
	p := NewPolicy([]string{`dangerous_command`}, nil)

	decision, _ := p.CheckCommand("dangerous_command --force")
	if decision != Denied {
		t.Errorf("expected Denied for custom pattern, got %v", decision)
	}
}
