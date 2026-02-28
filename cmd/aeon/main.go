package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jafran/aeon/internal/bootstrap"
	"github.com/jafran/aeon/internal/channels"
	"github.com/jafran/aeon/internal/config"
)

const shutdownTimeout = 10 * time.Second

var version = "0.1.0"

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
		default:
			// fall through to interactive mode
		}
	}

	runInteractive()
}

func runInit() {
	fmt.Printf("\n🌱 Aeon v%s — First-Time Setup\n", version)
	fmt.Println(strings.Repeat("=", 40))

	// Detect system
	fmt.Println("\n[1/3] Checking system...")
	info := bootstrap.DetectSystem()
	fmt.Printf("  ✓ OS: %s (%s)\n", info.OS, info.Arch)
	fmt.Println("  ✓ SQLite: compiled into binary")

	if info.PythonPath != "" {
		fmt.Printf("  ✓ Python: %s at %s\n", info.PythonVer, info.PythonPath)
	} else {
		fmt.Println("  ✗ Python: not found (evolved skills will be disabled)")
	}

	// Provider detection
	fmt.Println("\n[2/3] Detecting LLM providers...")
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
	if providerCount == 0 {
		fmt.Println("  ✗ No providers detected.")
		fmt.Println("    You'll need to configure at least one in ~/.aeon/config.yaml")
	}

	// Create workspace
	fmt.Println("\n[3/3] Setting up workspace...")
	if err := bootstrap.EnsureWorkspace(); err != nil {
		fmt.Fprintf(os.Stderr, "  ✗ Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✓ Workspace created at", config.AeonHome())

	// Generate config
	cfgPath := config.DefaultConfigPath()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfgContent := bootstrap.GenerateDefaultConfig(info)
		if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
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
			fmt.Println("    Skills will be disabled. Fix and run: aeon init --python")
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

	// Setup logging
	logLevel := parseLogLevel(cfg.Log.Level)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

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

	// Start CLI channel
	cli := channels.NewCLI()
	if err := cli.Start(ctx, deps.Bus); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting CLI channel: %v\n", err)
		os.Exit(1)
	}

	// Start Telegram channel if configured
	var tg *channels.TelegramChannel
	if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.BotToken != "" {
		tg = channels.NewTelegram(cfg.Channels.Telegram.BotToken, cfg.Channels.Telegram.AllowedUsers, logger)
		if cfg.Provider.Gemini != nil && cfg.Provider.Gemini.APIKey != "" {
			tg.SetTranscriber(channels.NewGeminiTranscriber(cfg.Provider.Gemini.APIKey, cfg.Provider.Gemini.DefaultModel))
		}
		if err := tg.Start(ctx, deps.Bus); err != nil {
			logger.Error("failed to start telegram channel", "error", err)
		} else {
			logger.Info("telegram channel started")
		}
	}

	// Run agent loop (blocks until context cancelled)
	deps.Loop.Run(ctx)

	// Cleanup
	cli.Stop()
	if tg != nil {
		tg.Stop()
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

	if cfg.Channels.Telegram == nil || !cfg.Channels.Telegram.Enabled || cfg.Channels.Telegram.BotToken == "" {
		fmt.Println("Telegram not configured. Add telegram settings to config.yaml and run 'aeon serve'.")
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

	logLevel := parseLogLevel(cfg.Log.Level)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

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

	// Start Telegram channel
	tg := channels.NewTelegram(cfg.Channels.Telegram.BotToken, cfg.Channels.Telegram.AllowedUsers, logger)
	if cfg.Provider.Gemini != nil && cfg.Provider.Gemini.APIKey != "" {
		tg.SetTranscriber(channels.NewGeminiTranscriber(cfg.Provider.Gemini.APIKey, cfg.Provider.Gemini.DefaultModel))
	}
	if err := tg.Start(ctx, deps.Bus); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting Telegram: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🌱 Aeon v%s — Daemon Mode (Telegram)\n", version)
	fmt.Printf("   Tools: %d | Skills: %d | Listening...\n\n", deps.Registry.Count(), deps.SkillLoader.Count())

	deps.Loop.Run(ctx)

	tg.Stop()
}

func printUsage() {
	fmt.Printf("aeon v%s — The Self-Evolving Agentic Kernel\n\n", version)
	fmt.Println("Usage:")
	fmt.Println("  aeon              Start interactive CLI mode")
	fmt.Println("  aeon serve        Start daemon mode (Telegram)")
	fmt.Println("  aeon init         First-time setup wizard")
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
