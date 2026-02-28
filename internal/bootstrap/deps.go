package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/jafran/aeon/internal/agent"
	"github.com/jafran/aeon/internal/bus"
	"github.com/jafran/aeon/internal/config"
	"github.com/jafran/aeon/internal/memory"
	"github.com/jafran/aeon/internal/providers"
	"github.com/jafran/aeon/internal/scheduler"
	"github.com/jafran/aeon/internal/security"
	"github.com/jafran/aeon/internal/skills"
	"github.com/jafran/aeon/internal/tools"
)

// Deps holds all shared dependencies for an Aeon instance.
type Deps struct {
	Bus         *bus.MessageBus
	MemStore    *memory.Store
	Registry    *tools.Registry
	Provider    providers.Provider
	SubMgr      *agent.SubagentManager
	Loop        *agent.AgentLoop
	Scheduler   *scheduler.Scheduler
	SkillLoader *skills.Loader
	SecAdapter  *security.PolicyAdapter
	Logger      *slog.Logger
	Cfg         *config.Config

	MemCount  int
	CronCount int
}

// BuildDeps creates all shared dependencies from config.
// The caller is responsible for calling Close() on the returned Deps.
func BuildDeps(cfg *config.Config, logger *slog.Logger) (*Deps, error) {
	home := config.AeonHome()
	d := &Deps{Cfg: cfg, Logger: logger}

	// Initialize message bus
	d.Bus = bus.New(64)

	// Initialize security policy
	secPolicy := security.NewPolicy(cfg.Security.DenyPatterns, cfg.Security.AllowedPaths)
	d.SecAdapter = security.NewAdapter(secPolicy)

	// Initialize memory store
	dbPath := filepath.Join(home, "aeon.db")
	memStore, err := memory.NewStore(dbPath)
	if err != nil {
		d.Close()
		return nil, fmt.Errorf("opening database: %w", err)
	}
	d.MemStore = memStore
	d.MemCount, _ = memStore.Count(context.Background())
	logger.Info("memory store ready", "path", dbPath, "entries", d.MemCount)

	// Initialize tool registry with DNA tools
	d.Registry = tools.NewRegistry()
	d.Registry.SetLogger(logger)
	dnaTools := tools.RegisterDNATools(d.Registry)
	dnaTools.ShellExec.SetSecurity(d.SecAdapter)
	dnaTools.FileRead.SetSecurity(d.SecAdapter)
	dnaTools.FileWrite.SetSecurity(d.SecAdapter)
	dnaTools.FileEdit.SetSecurity(d.SecAdapter)

	// Register memory tools
	d.Registry.Register(tools.NewMemoryStore(memStore))
	d.Registry.Register(tools.NewMemoryRecall(memStore))

	// Initialize skill system
	skillsDir := filepath.Join(home, "skills")
	venvPath := filepath.Join(home, "base_venv")
	d.SkillLoader = skills.NewLoader(skillsDir, venvPath)
	if err := d.SkillLoader.LoadAll(); err != nil {
		logger.Warn("failed to load skills", "error", err)
	}
	logger.Info("skills loaded", "count", d.SkillLoader.Count())

	// Register skill tools
	d.Registry.Register(tools.NewSkillFactory(d.SkillLoader))
	d.Registry.Register(tools.NewFindSkills(d.SkillLoader))
	d.Registry.Register(tools.NewReadSkill(d.SkillLoader))
	d.Registry.Register(tools.NewRunSkill(d.SkillLoader))

	// Initialize scheduler
	sched, err := scheduler.New(memStore.DB(), logger)
	if err != nil {
		logger.Warn("failed to initialize scheduler", "error", err)
	} else {
		d.Scheduler = sched
		d.Registry.Register(tools.NewCronManage(sched))
	}

	logger.Info("tools registered", "count", d.Registry.Count())

	// Initialize provider chain
	d.Provider, err = providers.FromConfig(cfg, logger)
	if err != nil {
		logger.Warn("no provider available, running in echo mode", "error", err)
	}

	// Initialize subagent manager
	d.SubMgr = agent.NewSubagentManager(d.Provider, d.Registry, d.Bus, logger)
	d.SubMgr.SetScrubber(d.SecAdapter)
	d.Registry.Register(tools.NewSpawnAgent(d.SubMgr))
	d.Registry.Register(tools.NewListTasks(d.SubMgr))

	// Apply configurable timeouts
	if cfg.Agent.ToolTimeout != "" {
		if dur, err := time.ParseDuration(cfg.Agent.ToolTimeout); err == nil {
			d.Registry.SetDefaultTimeout(dur)
		}
	}

	// Initialize agent loop
	d.Loop = agent.NewAgentLoop(d.Bus, d.Provider, d.Registry, logger)
	d.Loop.SetScrubber(d.SecAdapter)
	d.Loop.SetSubagentManager(d.SubMgr)
	d.Loop.SetMemoryStore(memStore)
	d.Loop.SetSystemPrompt(cfg.Agent.SystemPrompt)
	d.Loop.SetMaxHistoryMessages(cfg.Agent.MaxHistoryMessages)
	d.Loop.SetMaxIterations(cfg.Agent.MaxIterations)

	return d, nil
}

// SetupSchedulerTrigger configures the scheduler's trigger callback.
func (d *Deps) SetupSchedulerTrigger() {
	if d.Scheduler == nil {
		return
	}
	d.Scheduler.OnTrigger(func(job scheduler.Job) {
		if scheduler.IsOneShot(job.Schedule) {
			if d.Cfg.Channels.Telegram != nil {
				for _, uid := range d.Cfg.Channels.Telegram.AllowedUsers {
					d.Bus.Send(bus.OutboundMessage{
						Channel: "telegram",
						ChatID:  fmt.Sprintf("%d", uid),
						Content: fmt.Sprintf("Reminder: %s", job.Command),
					})
				}
			}
			return
		}
		d.Bus.Publish(bus.InboundMessage{
			Channel: "system",
			Content: fmt.Sprintf("[cron:%s] %s", job.Name, job.Command),
		})
	})
}

// StartScheduler starts the scheduler if available.
func (d *Deps) StartScheduler(ctx context.Context) {
	if d.Scheduler != nil {
		d.CronCount, _ = d.Scheduler.Count()
		d.Scheduler.Start(ctx)
	}
}

// Close cleans up all shared dependencies.
func (d *Deps) Close() {
	if d.MemStore != nil {
		d.MemStore.Close()
	}
	if d.Bus != nil {
		d.Bus.Close()
	}
}
