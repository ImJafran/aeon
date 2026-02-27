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
	"github.com/jafran/aeon/internal/providers"
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
	providerCount := config.EnabledProviderCount(cfg)

	fmt.Printf("\n🌱 Aeon v%s — The Self-Evolving Kernel\n", version)
	fmt.Printf("   Providers: %d configured\n", providerCount)

	// Check if skills dir has anything
	skillCount := 0
	skillsDir := filepath.Join(home, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				skillCount++
			}
		}
	}
	fmt.Printf("   Skills: %d loaded\n", skillCount)
	fmt.Printf("   Home: %s\n\n", home)

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

	// Initialize tool registry with DNA tools
	registry := tools.NewRegistry()
	tools.RegisterDNATools(registry)
	logger.Info("DNA tools registered", "count", registry.Count())

	// Initialize provider chain
	provider, err := providers.FromConfig(cfg, logger)
	if err != nil {
		logger.Warn("no provider available, running in echo mode", "error", err)
	}

	// Initialize agent loop
	loop := agent.NewAgentLoop(msgBus, provider, registry, logger)

	// Start CLI channel
	cli := channels.NewCLI()
	if err := cli.Start(ctx, msgBus); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting CLI channel: %v\n", err)
		os.Exit(1)
	}

	// Run agent loop (blocks until context cancelled)
	loop.Run(ctx)

	// Cleanup
	cli.Stop()
	msgBus.Close()

	_ = cfg // will be used for provider setup in Phase 2
}

func runServe() {
	fmt.Printf("🌱 Aeon v%s — Daemon Mode\n", version)
	fmt.Println("Daemon mode not yet implemented. Coming in Phase 8.")
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
