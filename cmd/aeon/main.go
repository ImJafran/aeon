package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ImJafran/aeon/internal/bootstrap"
	"github.com/ImJafran/aeon/internal/channels"
	"github.com/ImJafran/aeon/internal/config"
)

const shutdownTimeout = 10 * time.Second

var version = "0.0.1-beta"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			runInit()
			return
		case "version", "--version", "-v":
			fmt.Printf("aeon v%s\n", version)
			return
		case "help", "--help", "-h":
			printUsage()
			return
		case "serve":
			runServe()
			return
		case "uninstall":
			runUninstall()
			return
		default:
			// fall through to interactive mode
		}
	}

	runInteractive()
}

func runInit() {
	fmt.Printf("\n🌱 Aeon v%s — First-Time Setup\n", version)
	fmt.Println(strings.Repeat("=", 40))

	// Step 1: Detect system
	fmt.Println("\n[1/4] Checking system...")
	info := bootstrap.DetectSystem()
	fmt.Printf("  ✓ OS: %s (%s)\n", info.OS, info.Arch)
	fmt.Println("  ✓ SQLite: compiled into binary")

	if info.PythonPath != "" {
		fmt.Printf("  ✓ Python: %s at %s\n", info.PythonVer, info.PythonPath)
	} else {
		fmt.Println("  ✗ Python: not found")
	}

	if info.FfmpegPath != "" {
		fmt.Printf("  ✓ ffmpeg: %s\n", info.FfmpegPath)
	} else {
		fmt.Println("  ✗ ffmpeg: not found")
	}

	// Step 2: Install missing dependencies
	fmt.Println("\n[2/4] Installing dependencies...")
	if info.PythonPath == "" {
		path, ver := bootstrap.InstallPython()
		if path != "" {
			info.PythonPath = path
			info.PythonVer = ver
			fmt.Printf("  ✓ Python installed: %s\n", ver)
		} else {
			fmt.Println("  ⚠ Python not installed (evolved skills will be disabled)")
		}
	} else {
		fmt.Println("  ✓ Python already installed")
	}

	if info.FfmpegPath == "" {
		path := bootstrap.InstallFfmpeg()
		if path != "" {
			info.FfmpegPath = path
			fmt.Println("  ✓ ffmpeg installed")
		} else {
			fmt.Println("  ⚠ ffmpeg not installed (voice transcription will be disabled)")
		}
	} else {
		fmt.Println("  ✓ ffmpeg already installed")
	}

	// Step 3: Detect LLM providers
	fmt.Println("\n[3/4] Detecting LLM providers...")
	providerCount := 0
	if info.HasClaudeCLI {
		fmt.Println("  ✓ Claude CLI found")
		providerCount++
	}
	if info.HasAnthropicKey {
		fmt.Println("  ✓ ANTHROPIC_API_KEY set")
		providerCount++
	}
	if info.HasGeminiKey {
		fmt.Println("  ✓ GEMINI_API_KEY set")
		providerCount++
	}
	if info.HasZAIKey {
		fmt.Println("  ✓ ZAI_API_KEY set")
		providerCount++
	}
	if providerCount == 0 {
		fmt.Println("  ✗ No providers detected.")
		fmt.Println("    You'll need to configure at least one in ~/.aeon/config.yaml")
	}

	// Step 4: Setup workspace
	fmt.Println("\n[4/4] Setting up workspace...")
	if err := bootstrap.EnsureWorkspace(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✓ Workspace created at", config.AeonHome())

	// Generate config
	cfgPath := config.DefaultConfigPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfgContent := bootstrap.GenerateDefaultConfig(info)
		if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Error writing config: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("  ✓ Config written to", cfgPath)
	} else {
		fmt.Println("  ✓ Config already exists at", cfgPath)
	}

	// Setup Python venv
	if info.PythonPath != "" {
		if err := bootstrap.SetupBaseVenv(info.PythonPath); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Python venv setup failed: %v\n", err)
			fmt.Println("    Skills will be disabled. Fix and run: aeon init")
		} else {
			fmt.Println("  ✓ Base Python environment ready")
		}
	}

	fmt.Println("\n✓ Setup complete!")
	fmt.Println("\n  Run 'aeon' for interactive CLI mode.")
	fmt.Println("  Run 'aeon serve' for daemon mode.")
}

func runInteractive() {
	cfgPath := config.DefaultConfigPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Println("No config found. Run 'aeon init' first.")
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Setup context with graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down gracefully (10s timeout)...")
		cancel()
		// Start forced shutdown timer
		timer := time.NewTimer(shutdownTimeout)
		defer timer.Stop()
		select {
		case <-sigCh:
			fmt.Println("\nForced shutdown.")
			os.Exit(1)
		case <-timer.C:
			fmt.Println("\nShutdown timed out. Forcing exit.")
			os.Exit(1)
		}
	}()

	// Setup logging (stderr + file)
	logger, closeLog := setupLogger(cfg)
	defer closeLog()

	// Build all shared dependencies
	deps, err := bootstrap.BuildDeps(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer deps.Close()

	// Setup and start scheduler
	deps.SetupSchedulerTrigger()
	deps.StartScheduler(ctx)

	// Print banner
	home := config.AeonHome()
	providerCount := config.EnabledProviderCount(cfg)
	fmt.Printf("\n🌱 Aeon v%s — The Self-Evolving Kernel\n", version)
	fmt.Printf("   Providers: %d configured\n", providerCount)
	fmt.Printf("   Tools: %d loaded\n", deps.Registry.Count())
	fmt.Printf("   Skills: %d loaded\n", deps.SkillLoader.Count())
	fmt.Printf("   Memory: %d entries\n", deps.MemCount)
	fmt.Printf("   Cron: %d jobs\n", deps.CronCount)
	fmt.Printf("   Home: %s\n\n", home)

	// Start CLI channel (always active in interactive mode)
	type stoppable interface{ Stop() }
	var activeChannels []stoppable

	cli := channels.NewCLI()
	if err := cli.Start(ctx, deps.Bus); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting CLI channel: %v\n", err)
		os.Exit(1)
	}
	activeChannels = append(activeChannels, cli)

	// Start additional enabled channels
	if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.BotToken != "" {
		tg := channels.NewTelegram(cfg.Channels.Telegram.BotToken, cfg.Channels.Telegram.AllowedUsers, logger)
		if cfg.Provider.Gemini != nil && cfg.Provider.Gemini.APIKey != "" {
			tg.SetTranscriber(channels.NewGeminiTranscriber(cfg.Provider.Gemini.APIKey, cfg.Provider.Gemini.DefaultModel))
		}
		if err := tg.Start(ctx, deps.Bus); err != nil {
			logger.Error("failed to start telegram channel", "error", err)
		} else {
			logger.Info("telegram channel started")
			activeChannels = append(activeChannels, tg)
		}
	}

	// Run agent loop (blocks until context cancelled)
	deps.Loop.Run(ctx)

	// Cleanup
	for _, ch := range activeChannels {
		ch.Stop()
	}
}

func runServe() {
	cfgPath := config.DefaultConfigPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		fmt.Println("No config found. Run 'aeon init' first.")
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down gracefully (10s timeout)...")
		cancel()
		timer := time.NewTimer(shutdownTimeout)
		defer timer.Stop()
		select {
		case <-sigCh:
			fmt.Println("\nForced shutdown.")
			os.Exit(1)
		case <-timer.C:
			fmt.Println("\nShutdown timed out. Forcing exit.")
			os.Exit(1)
		}
	}()

	logger, closeLog := setupLogger(cfg)
	defer closeLog()

	// Build all shared dependencies
	deps, err := bootstrap.BuildDeps(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer deps.Close()

	if deps.Provider == nil {
		fmt.Fprintf(os.Stderr, "Error: no provider available for serve mode\n")
		os.Exit(1)
	}

	// Setup and start scheduler
	deps.SetupSchedulerTrigger()
	deps.StartScheduler(ctx)

	// Start all enabled channels
	type stoppable interface{ Stop() }
	var activeChannels []stoppable
	var channelNames []string

	if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.BotToken != "" {
		tg := channels.NewTelegram(cfg.Channels.Telegram.BotToken, cfg.Channels.Telegram.AllowedUsers, logger)
		if cfg.Provider.Gemini != nil && cfg.Provider.Gemini.APIKey != "" {
			tg.SetTranscriber(channels.NewGeminiTranscriber(cfg.Provider.Gemini.APIKey, cfg.Provider.Gemini.DefaultModel))
		}
		if err := tg.Start(ctx, deps.Bus); err != nil {
			logger.Error("failed to start telegram channel", "error", err)
		} else {
			activeChannels = append(activeChannels, tg)
			channelNames = append(channelNames, "telegram")
		}
	}

	if len(activeChannels) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no channels configured. Add at least one channel to config.yaml.\n")
		os.Exit(1)
	}

	fmt.Printf("🌱 Aeon v%s — Daemon Mode (%s)\n", version, strings.Join(channelNames, ", "))
	fmt.Printf("   Tools: %d | Skills: %d | Listening...\n\n", deps.Registry.Count(), deps.SkillLoader.Count())

	deps.Loop.Run(ctx)

	for _, ch := range activeChannels {
		ch.Stop()
	}
}

func runUninstall() {
	fmt.Println("\n⚠ Aeon Uninstall")
	fmt.Println(strings.Repeat("=", 40))

	home := config.AeonHome()
	binPath, _ := os.Executable()

	fmt.Println("\nThis will remove:")
	fmt.Printf("  • Aeon home directory: %s\n", home)
	fmt.Printf("    (config, database, skills, logs, workspace — everything)\n")
	fmt.Printf("  • Aeon binary: %s\n", binPath)

	// Check for systemd service
	servicePath := "/etc/systemd/system/aeon.service"
	hasService := false
	if _, err := os.Stat(servicePath); err == nil {
		hasService = true
		fmt.Printf("  • Systemd service: %s\n", servicePath)
	}

	fmt.Print("\nAre you sure? This cannot be undone. [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("Cancelled.")
		return
	}

	// Stop and disable systemd service if present
	if hasService {
		fmt.Println("\n[1/3] Stopping systemd service...")
		exec.Command("sudo", "systemctl", "stop", "aeon").Run()
		exec.Command("sudo", "systemctl", "disable", "aeon").Run()
		if err := exec.Command("sudo", "rm", "-f", servicePath).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Could not remove service file: %v\n", err)
		} else {
			exec.Command("sudo", "systemctl", "daemon-reload").Run()
			fmt.Println("  ✓ Service stopped and removed")
		}
	} else {
		fmt.Println("\n[1/3] No systemd service found, skipping")
	}

	// Remove home directory
	fmt.Println("[2/3] Removing Aeon home directory...")
	if _, err := os.Stat(home); err == nil {
		if err := os.RemoveAll(home); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ Error removing %s: %v\n", home, err)
			fmt.Println("    Try manually: rm -rf", home)
		} else {
			fmt.Println("  ✓ Removed", home)
		}
	} else {
		fmt.Println("  ✓ Already gone")
	}

	// Remove binary (self-delete)
	fmt.Println("[3/3] Removing Aeon binary...")
	if binPath != "" {
		// Try direct removal first, fall back to sudo
		if err := os.Remove(binPath); err != nil {
			if err := exec.Command("sudo", "rm", "-f", binPath).Run(); err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ Could not remove binary: %v\n", err)
				fmt.Println("    Try manually: sudo rm", binPath)
			} else {
				fmt.Println("  ✓ Removed", binPath)
			}
		} else {
			fmt.Println("  ✓ Removed", binPath)
		}
	}

	fmt.Println("\n✓ Aeon has been completely uninstalled.")
	fmt.Println("  If you cloned the source, you can remove it manually:")
	fmt.Println("    rm -rf /path/to/aeon")
}

func printUsage() {
	fmt.Printf("aeon v%s — The Self-Evolving Agentic Kernel\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  aeon              Start interactive CLI mode")
	fmt.Println("  aeon serve        Start daemon mode (all enabled channels)")
	fmt.Println("  aeon init         First-time setup wizard")
	fmt.Println("  aeon uninstall    Remove Aeon completely (binary, data, service)")
	fmt.Println("  aeon version      Show version")
	fmt.Println("  aeon help         Show this help")
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// setupLogger creates a logger that writes to both stderr and a log file.
// Returns the logger and a cleanup function to close the file.
func setupLogger(cfg *config.Config) (*slog.Logger, func()) {
	logLevel := parseLogLevel(cfg.Log.Level)

	logFile := cfg.Log.File
	if logFile == "" {
		logFile = filepath.Join(config.AeonHome(), "logs", "aeon.log")
	}

	// Ensure log directory exists
	os.MkdirAll(filepath.Dir(logFile), 0755)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fall back to stderr only
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
		logger.Warn("failed to open log file, logging to stderr only", "path", logFile, "error", err)
		return logger, func() {}
	}

	w := io.MultiWriter(os.Stderr, f)
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: logLevel}))
	return logger, func() { f.Close() }
}
