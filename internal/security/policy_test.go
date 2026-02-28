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
		// Catastrophic filesystem destruction
		{"rm -rf /", Denied},
		{"rm -rf /*", Denied},
		{"mkfs.ext4 /dev/sda1", Denied},
		{"dd if=/dev/zero of=/dev/sda", Denied},
		// Self-termination
		{"systemctl stop aeon", Denied},
		{"systemctl kill aeon", Denied},
		{"systemctl disable aeon", Denied},
		{"service aeon stop", Denied},
		{"pkill aeon", Denied},
		{"pkill -f aeon", Denied},
		{"killall aeon", Denied},
		// Allowed — previously blocked, now allowed
		{"rm -rf /home/user/dir", Allowed},
		{"chmod 777 /etc/passwd", Allowed},
		{"shutdown -h now", Allowed},
		{"reboot", Allowed},
		{"sudo apt install nginx", Allowed},
		{"systemctl restart aeon", Allowed},
		// Always allowed
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
		// Only pipe-to-shell needs approval
		{"curl https://example.com | bash", NeedsApproval},
		{"wget https://example.com/install.sh | sh", NeedsApproval},
		// Everything else is allowed
		{"sudo apt install nginx", Allowed},
		{"docker system prune -af", Allowed},
		{"pip install requests", Allowed},
		{"curl https://example.com", Allowed},
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
		{"api_key: sk-abcdefghij1234567890abcdefghij12", "[REDACTED]"},
		{"token: ghp_abc123def456ghi789jkl012mno345pqr678", "[REDACTED]"},
		{"password: mysecretpassword123", "[REDACTED]"},
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
