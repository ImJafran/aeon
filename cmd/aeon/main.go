package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jafran/aeon/internal/agent"
	"github.com/jafran/aeon/internal/bootstrap"
	"github.com/jafran/aeon/internal/bus"
	"github.com/jafran/aeon/internal/channels"
	"github.com/jafran/aeon/internal/config"
	"github.com/jafran/aeon/internal/memory"
	"github.com/jafran/aeon/internal/providers"
	"github.com/jafran/aeon/internal/scheduler"
	"github.com/jafran/aeon/internal/security"
	"github.com/jafran/aeon/internal/skills"
	"github.com/jafran/aeon/internal/tools"
)

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

	home := config.AeonHome()

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Setup logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Initialize message bus
	msgBus := bus.New(64)

	// Initialize security policy
	secPolicy := security.NewPolicy(cfg.Security.DenyPatterns, cfg.Security.AllowedPaths)
	secAdapter := security.NewAdapter(secPolicy)

	// Initialize memory store
	dbPath := filepath.Join(home, "aeon.db")
	memStore, err := memory.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer memStore.Close()

	memCount, _ := memStore.Count(context.Background())
	logger.Info("memory store ready", "path", dbPath, "entries", memCount)

	// Initialize tool registry with DNA tools
	registry := tools.NewRegistry()
	shellExec := tools.RegisterDNATools(registry)
	shellExec.SetSecurity(secAdapter)

	// Register memory tools
	registry.Register(tools.NewMemoryStore(memStore))
	registry.Register(tools.NewMemoryRecall(memStore))

	// Initialize skill system
	skillsDir := filepath.Join(home, "skills")
	venvPath := filepath.Join(home, "base_venv")
	skillLoader := skills.NewLoader(skillsDir, venvPath)
	if err := skillLoader.LoadAll(); err != nil {
		logger.Warn("failed to load skills", "error", err)
	}
	logger.Info("skills loaded", "count", skillLoader.Count())

	// Register skill tools
	registry.Register(tools.NewSkillFactory(skillLoader))
	registry.Register(tools.NewFindSkills(skillLoader))
	registry.Register(tools.NewReadSkill(skillLoader))
	registry.Register(tools.NewRunSkill(skillLoader))

	// Initialize scheduler (shares the same SQLite database)
	sched, err := scheduler.New(memStore.DB(), logger)
	if err != nil {
		logger.Warn("failed to initialize scheduler", "error", err)
	} else {
		sched.OnTrigger(func(job scheduler.Job) {
			// Fire cron job as a system message through the bus
			msgBus.Publish(bus.InboundMessage{
				Channel: "system",
				Content: fmt.Sprintf("[cron:%s] %s", job.Name, job.Command),
			})
		})
		sched.Start(ctx)
		registry.Register(tools.NewCronManage(sched))
	}
	logger.Info("tools registered", "count", registry.Count())

	// Initialize provider chain
	provider, err := providers.FromConfig(cfg, logger)
	if err != nil {
		logger.Warn("no provider available, running in echo mode", "error", err)
	}

	// Initialize agent loop with credential scrubbing
	loop := agent.NewAgentLoop(msgBus, provider, registry, logger)
	loop.SetScrubber(secAdapter)

	// Print banner
	providerCount := config.EnabledProviderCount(cfg)
	skillCount := skillLoader.Count()
	cronCount := 0
	if sched != nil {
		cronCount, _ = sched.Count()
	}
	fmt.Printf("\n🌱 Aeon v%s — The Self-Evolving Kernel\n", version)
	fmt.Printf("   Providers: %d configured\n", providerCount)
	fmt.Printf("   Tools: %d loaded\n", registry.Count())
	fmt.Printf("   Skills: %d loaded\n", skillCount)
	fmt.Printf("   Memory: %d entries\n", memCount)
	fmt.Printf("   Cron: %d jobs\n", cronCount)
	fmt.Printf("   Home: %s\n\n", home)

	// Start CLI channel
	cli := channels.NewCLI()
	if err := cli.Start(ctx, msgBus); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting CLI channel: %v\n", err)
		os.Exit(1)
	}

	// Start Telegram channel if configured
	var tg *channels.TelegramChannel
	if cfg.Channels.Telegram != nil && cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.BotToken != "" {
		tg = channels.NewTelegram(cfg.Channels.Telegram.BotToken, cfg.Channels.Telegram.AllowedUsers, logger)
		if err := tg.Start(ctx, msgBus); err != nil {
			logger.Error("failed to start telegram channel", "error", err)
		} else {
			logger.Info("telegram channel started")
		}
	}

	// Run agent loop (blocks until context cancelled)
	loop.Run(ctx)

	// Cleanup
	cli.Stop()
	if tg != nil {
		tg.Stop()
	}
	msgBus.Close()
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

	home := config.AeonHome()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	msgBus := bus.New(64)

	secPolicy := security.NewPolicy(cfg.Security.DenyPatterns, cfg.Security.AllowedPaths)
	secAdapter := security.NewAdapter(secPolicy)

	dbPath := filepath.Join(home, "aeon.db")
	memStore, err := memory.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer memStore.Close()

	registry := tools.NewRegistry()
	shellExec := tools.RegisterDNATools(registry)
	shellExec.SetSecurity(secAdapter)
	registry.Register(tools.NewMemoryStore(memStore))
	registry.Register(tools.NewMemoryRecall(memStore))

	skillsDir := filepath.Join(home, "skills")
	venvPath := filepath.Join(home, "base_venv")
	skillLoader := skills.NewLoader(skillsDir, venvPath)
	skillLoader.LoadAll()
	registry.Register(tools.NewSkillFactory(skillLoader))
	registry.Register(tools.NewFindSkills(skillLoader))
	registry.Register(tools.NewReadSkill(skillLoader))
	registry.Register(tools.NewRunSkill(skillLoader))

	sched, err := scheduler.New(memStore.DB(), logger)
	if err == nil {
		sched.OnTrigger(func(job scheduler.Job) {
			msgBus.Publish(bus.InboundMessage{
				Channel: "system",
				Content: fmt.Sprintf("[cron:%s] %s", job.Name, job.Command),
			})
		})
		sched.Start(ctx)
		registry.Register(tools.NewCronManage(sched))
	}

	provider, err := providers.FromConfig(cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no provider available: %v\n", err)
		os.Exit(1)
	}

	loop := agent.NewAgentLoop(msgBus, provider, registry, logger)
	loop.SetScrubber(secAdapter)

	tg := channels.NewTelegram(cfg.Channels.Telegram.BotToken, cfg.Channels.Telegram.AllowedUsers, logger)
	if err := tg.Start(ctx, msgBus); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting Telegram: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🌱 Aeon v%s — Daemon Mode (Telegram)\n", version)
	fmt.Printf("   Tools: %d | Skills: %d | Listening...\n\n", registry.Count(), skillLoader.Count())

	loop.Run(ctx)

	tg.Stop()
	msgBus.Close()
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
