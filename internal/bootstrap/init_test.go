package bootstrap

import (
	"encoding/json"
	"testing"

	"github.com/ImJafran/aeon/internal/config"
)

func TestGenerateDefaultConfigValidJSON(t *testing.T) {
	tests := []struct {
		name string
		info *SystemInfo
	}{
		{"all providers", &SystemInfo{OS: "linux", Arch: "amd64", HasAnthropicKey: true, HasGeminiKey: true, HasZAIKey: true, HasTelegram: true}},
		{"no providers", &SystemInfo{OS: "linux", Arch: "amd64"}},
		{"claude_cli only", &SystemInfo{OS: "darwin", Arch: "arm64", HasClaudeCLI: true}},
		{"anthropic only", &SystemInfo{OS: "linux", Arch: "amd64", HasAnthropicKey: true}},
		{"gemini only", &SystemInfo{OS: "linux", Arch: "amd64", HasGeminiKey: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := GenerateDefaultConfig(tt.info)

			// Must be valid JSON
			var raw json.RawMessage
			if err := json.Unmarshal([]byte(out), &raw); err != nil {
				t.Fatalf("invalid JSON: %v\noutput:\n%s", err, out)
			}

			// Must parse into Config
			var cfg config.Config
			if err := json.Unmarshal([]byte(out), &cfg); err != nil {
				t.Fatalf("cannot unmarshal into Config: %v", err)
			}
		})
	}
}
