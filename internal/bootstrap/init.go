package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ImJafran/aeon/internal/config"
)

type SystemInfo struct {
	OS              string
	Arch            string
	PythonPath      string
	PythonVer       string
	FfmpegPath      string
	HasClaudeCLI    bool
	HasAnthropicKey bool
	HasGeminiKey    bool
	HasZAIKey       bool
	HasTelegram     bool
}

func DetectSystem() *SystemInfo {
	info := &SystemInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	if path, err := exec.LookPath("python3"); err == nil {
		info.PythonPath = path
		if out, err := exec.Command(path, "--version").Output(); err == nil {
			info.PythonVer = strings.TrimSpace(string(out))
		}
	}

	if path, err := exec.LookPath("ffmpeg"); err == nil {
		info.FfmpegPath = path
	}

	if _, err := exec.LookPath("claude"); err == nil {
		info.HasClaudeCLI = true
	}

	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		info.HasAnthropicKey = true
	}
	if os.Getenv("GEMINI_API_KEY") != "" {
		info.HasGeminiKey = true
	}
	if os.Getenv("ZAI_API_KEY") != "" {
		info.HasZAIKey = true
	}
	if os.Getenv("TELEGRAM_BOT_TOKEN") != "" {
		info.HasTelegram = true
	}

	return info
}

// InstallPython attempts to install Python 3 using the system package manager.
// Returns the python3 path on success, or empty string if it fails.
func InstallPython() (string, string) {
	fmt.Println("  Installing Python 3...")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		// Try apt first (Debian/Ubuntu), then dnf (Fedora/RHEL), then pacman (Arch)
		if _, err := exec.LookPath("apt-get"); err == nil {
			cmd = exec.Command("sudo", "apt-get", "install", "-y", "python3", "python3-venv", "python3-pip")
		} else if _, err := exec.LookPath("dnf"); err == nil {
			cmd = exec.Command("sudo", "dnf", "install", "-y", "python3", "python3-pip")
		} else if _, err := exec.LookPath("pacman"); err == nil {
			cmd = exec.Command("sudo", "pacman", "-S", "--noconfirm", "python", "python-pip")
		} else {
			fmt.Println("  ⚠ No supported package manager found (apt, dnf, pacman)")
			return "", ""
		}
	case "darwin":
		if _, err := exec.LookPath("brew"); err == nil {
			cmd = exec.Command("brew", "install", "python3")
		} else {
			fmt.Println("  ⚠ Homebrew not found. Install from https://brew.sh")
			return "", ""
		}
	default:
		fmt.Printf("  ⚠ Auto-install not supported on %s\n", runtime.GOOS)
		return "", ""
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("  ⚠ Python install failed: %v\n", err)
		return "", ""
	}

	// Verify installation
	path, err := exec.LookPath("python3")
	if err != nil {
		return "", ""
	}
	ver := ""
	if out, err := exec.Command(path, "--version").Output(); err == nil {
		ver = strings.TrimSpace(string(out))
	}
	return path, ver
}

// InstallFfmpeg attempts to install ffmpeg using the system package manager.
// Returns the ffmpeg path on success, or empty string if it fails.
func InstallFfmpeg() string {
	fmt.Println("  Installing ffmpeg...")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("apt-get"); err == nil {
			cmd = exec.Command("sudo", "apt-get", "install", "-y", "ffmpeg")
		} else if _, err := exec.LookPath("dnf"); err == nil {
			cmd = exec.Command("sudo", "dnf", "install", "-y", "ffmpeg")
		} else if _, err := exec.LookPath("pacman"); err == nil {
			cmd = exec.Command("sudo", "pacman", "-S", "--noconfirm", "ffmpeg")
		} else {
			fmt.Println("  ⚠ No supported package manager found (apt, dnf, pacman)")
			return ""
		}
	case "darwin":
		if _, err := exec.LookPath("brew"); err == nil {
			cmd = exec.Command("brew", "install", "ffmpeg")
		} else {
			fmt.Println("  ⚠ Homebrew not found. Install from https://brew.sh")
			return ""
		}
	default:
		fmt.Printf("  ⚠ Auto-install not supported on %s\n", runtime.GOOS)
		return ""
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("  ⚠ ffmpeg install failed: %v\n", err)
		return ""
	}

	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return path
}

func EnsureWorkspace() error {
	home := config.AeonHome()
	dirs := []string{
		home,
		filepath.Join(home, "skills"),
		filepath.Join(home, "logs"),
		filepath.Join(home, "workspace"),
		filepath.Join(home, "workspace", "memory"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Copy workspace templates if not present
	templates := map[string]string{
		"AGENT.md":     defaultAgentMD,
		"SOUL.md":      defaultSoulMD,
		"HEARTBEAT.md": defaultHeartbeatMD,
	}
	for name, content := range templates {
		path := filepath.Join(home, "workspace", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return fmt.Errorf("writing %s: %w", name, err)
			}
		}
	}

	memPath := filepath.Join(home, "workspace", "memory", "MEMORY.md")
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte("# Aeon Memory\n"), 0644); err != nil {
			return fmt.Errorf("writing MEMORY.md: %w", err)
		}
	}

	return nil
}

func SetupBaseVenv(pythonPath string) error {
	home := config.AeonHome()
	venvPath := filepath.Join(home, "base_venv")

	if _, err := os.Stat(filepath.Join(venvPath, "bin", "python3")); err == nil {
		return nil // already exists
	}

	fmt.Println("  Creating base Python environment...")
	cmd := exec.Command(pythonPath, "-m", "venv", venvPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating venv: %w", err)
	}

	pip := filepath.Join(venvPath, "bin", "pip3")
	packages := []string{"requests", "httpx", "beautifulsoup4", "pyyaml"}
	fmt.Printf("  Installing base packages: %s\n", strings.Join(packages, ", "))
	args := append([]string{"install", "-q"}, packages...)
	cmd = exec.Command(pip, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installing base packages: %w", err)
	}

	return nil
}

func GenerateDefaultConfig(info *SystemInfo) string {
	cfg := config.Config{}

	// Providers
	if info.HasZAIKey {
		cfg.Provider.ZAI = &config.ZAIConfig{
			Enabled:      true,
			APIKey:       "${ZAI_API_KEY}",
			DefaultModel: "glm-4.7",
		}
	}
	if info.HasClaudeCLI {
		cfg.Provider.ClaudeCLI = &config.ClaudeCLIConfig{
			Enabled: true,
			Binary:  "claude",
			Timeout: "120s",
			Flags:   []string{"--dangerously-skip-permissions", "--no-chrome"},
		}
	}
	if info.HasAnthropicKey {
		cfg.Provider.Anthropic = &config.AnthropicConfig{
			Enabled:      true,
			APIKey:       "${ANTHROPIC_API_KEY}",
			DefaultModel: "claude-sonnet-4-6",
			FastModel:    "claude-haiku-4-5-20251001",
		}
	}
	if info.HasGeminiKey {
		cfg.Provider.Gemini = &config.GeminiConfig{
			Enabled:      true,
			APIKey:       "${GEMINI_API_KEY}",
			DefaultModel: "gemini-2.5-flash-lite",
			AudioModel:   "gemini-2.5-flash-native-audio-preview-12-2025",
			TTSModel:     "gemini-2.5-flash-preview-tts",
		}
	}

	// Channels
	if info.HasTelegram {
		tg := &config.TelegramConfig{
			Enabled:  true,
			BotToken: "${TELEGRAM_BOT_TOKEN}",
		}
		if uid := os.Getenv("TELEGRAM_USER_ID"); uid != "" {
			// Parse as int64 — skip on error since the env var may not be numeric
			var id int64
			if _, err := fmt.Sscanf(uid, "%d", &id); err == nil {
				tg.AllowedUsers = []int64{id}
			}
		}
		cfg.Channels.Telegram = tg
	}

	// Routing
	switch {
	case info.HasZAIKey:
		cfg.Routing.Primary = "zai"
		if info.HasAnthropicKey {
			cfg.Routing.Fast = "anthropic_fast"
			cfg.Routing.Fallback = "anthropic"
		} else if info.HasGeminiKey {
			cfg.Routing.Fallback = "gemini"
		}
	case info.HasAnthropicKey:
		cfg.Routing.Primary = "anthropic"
		cfg.Routing.Fast = "anthropic_fast"
		if info.HasGeminiKey {
			cfg.Routing.Fallback = "gemini"
		}
	case info.HasGeminiKey:
		cfg.Routing.Primary = "gemini"
	}

	// Security
	cfg.Security.ApprovalTimeout = "60s"
	cfg.Security.AllowedPaths = []string{"~/.aeon"}

	// Skills
	cfg.Skills.BasePackages = []string{"requests", "httpx", "beautifulsoup4", "pyyaml"}
	cfg.Skills.WarmPoolSize = 3
	cfg.Skills.MaxRetries = 3

	// Memory
	cfg.Memory.AutoSave = true
	cfg.Memory.CompactionThreshold = 10

	// Log
	cfg.Log.Level = "info"

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data) + "\n"
}

const defaultAgentMD = `# Agent Behavior

## Prime Directive
Act, don't chat. You are a system agent — execute tasks, report results, move on.
When asked to do something, DO IT. Don't describe what you would do.

## Tool Usage
- Use shell_exec for system commands, file operations, installations, and any task that requires the operating system.
- Use memory_store to save important facts, preferences, corrections, and lessons learned.
- Use memory_recall before asking the user to repeat themselves.
- When you need a capability you don't have, use skill_factory to CREATE it — don't say "I can't do that".
- Use spawn_agent for complex or parallel tasks.

## Self-Evolution
- When a task reveals a gap in your capabilities, create a skill to fill it.
- When you make a mistake, store a lesson in memory (category: lesson) so you don't repeat it.
- When corrected, store the correction in memory (category: correction) with high importance.
- You can edit your own AGENT.md and SOUL.md files to refine your behavior over time.
- Review and update your skills when they fail — iterate, don't give up.

## Communication
- Be concise. 1-3 sentences max unless detail is requested.
- Show command output when relevant.
- Report errors with context — what failed, why, what you'll try next.
- Don't ask for permission for safe operations. Act, then report.

## Safety
- Never execute destructive commands (rm -rf, DROP TABLE, etc.) without user approval.
- Never expose API keys, tokens, or passwords in responses.
- If a skill fails 3+ times, investigate the root cause before retrying.
`

const defaultSoulMD = `# Identity

I am Aeon — a self-evolving agentic kernel.

I live on my operator's system. I am not a chatbot — I am a persistent agent that DOES things.
I have a shell, a filesystem, memory, scheduling, and the ability to create my own tools.

## Core Traits
- **Autonomous**: I act on instructions without hand-holding. If I can figure it out, I do.
- **Self-improving**: I learn from mistakes, store lessons, and evolve my skills over time.
- **Reliable**: I complete tasks correctly. When I fail, I debug, fix, and retry.
- **Minimal**: I say what's needed, nothing more. Results speak louder than words.

## What I Am Not
- I am not a search engine. I execute, I don't just retrieve.
- I am not cautious to a fault. I take action within safety boundaries.
- I am not a one-shot tool. I persist, remember, and grow across sessions.
`

const defaultHeartbeatMD = `# Heartbeat Tasks

Tasks that Aeon runs periodically to stay aware and proactive.

## Quick (run inline, <30s)
- Check system disk usage, alert if >80%
- Report system uptime and load average

## Background (spawn subagent for longer tasks)
- Consolidate and clean up old memories
`
