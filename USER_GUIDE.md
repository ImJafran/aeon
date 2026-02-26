# Aeon User Guide

## Prerequisites

**Just one thing:** Linux (Ubuntu 22.04+) or macOS. Everything else is auto-handled.

Aeon is a **single static binary** with zero runtime dependencies. On first run, it detects your system and bootstraps everything it needs.

---

## Installation

### One-Line Install

```bash
curl -fsSL https://get.aeon.dev | sh
```

This downloads the binary, puts it in your PATH, and runs `aeon init`.

### Or Manual Download

```bash
curl -fsSL https://github.com/youruser/aeon/releases/latest/download/aeon-linux-amd64 -o aeon
chmod +x aeon
sudo mv aeon /usr/local/bin/
aeon init
```

### Or Build from Source (needs Go 1.22+)

```bash
git clone https://github.com/youruser/aeon.git
cd aeon
make build && sudo make install
aeon init
```

---

## What `aeon init` Does

Interactive first-run wizard. Auto-detects, auto-installs, asks only what it must.

```
$ aeon init

🌱 Aeon First-Time Setup
========================

[1/4] Checking system...
  ✓ OS: Ubuntu 24.04 (x86_64)
  ✓ SQLite: compiled into binary (no install needed)

[2/4] Setting up Python (needed for evolved skills)...
  ✓ Python 3.12 found at /usr/bin/python3
  ✓ Creating base environment... done (requests, httpx, bs4, pyyaml)
  ── OR ──
  ✗ Python not found.
    Install now? [Y/n] y
    → Installing python3 via apt... done
    → Creating base environment... done
  ── OR ──
  ✗ Python not found.
    Install now? [Y/n] n
    → Skipping. Aeon will work but cannot create evolved skills.
      You can install Python later and run: aeon init --python

[3/4] Configuring LLM provider (need at least one)...
  Detected:
    ✓ claude binary found (Claude CLI)
    ✓ ANTHROPIC_API_KEY env var set
    ✗ GEMINI_API_KEY not set

  Which provider should be primary?
  [1] Claude CLI (free — uses your subscription)  ← detected
  [2] Anthropic API (pay-per-token)               ← detected
  [3] Gemini API (enter API key)
  [4] Ollama / Local model (enter URL)
  > 1

  ✓ Primary: Claude CLI
  ✓ Fallback: Anthropic API (auto-configured)

[4/4] Optional integrations...
  Set up Telegram bot? [y/N] y
    Enter bot token: <paste>
    Enter your Telegram user ID: 123456789
  ✓ Telegram configured.

✓ Setup complete!

  Config:     ~/.aeon/config.yaml
  Database:   ~/.aeon/aeon.db
  Skills:     ~/.aeon/skills/
  Logs:       ~/.aeon/logs/

  Run `aeon` to start in CLI mode.
  Run `aeon serve` to start in daemon mode (Telegram only).
```

### What Gets Created

```
~/.aeon/
├── config.yaml          # main config (auto-generated, editable)
├── aeon.db              # SQLite database (auto-created at runtime)
├── base_venv/           # shared Python env (auto-created if Python available)
│   └── (requests, httpx, bs4, pyyaml pre-installed)
├── skills/              # your evolved tools live here
├── workspace/
│   ├── AGENT.md         # system prompt / personality
│   ├── SOUL.md          # identity and boundaries
│   └── memory/
│       └── MEMORY.md    # persistent notes
└── logs/
    └── aeon.log
```

### Auto-Dependency Resolution

| Dependency | How Aeon Handles It |
|---|---|
| SQLite | Compiled into the Go binary (pure Go driver). Zero install. |
| Python 3 | Auto-detected. If missing: offers to install via system package manager. If declined: Aeon works but skills are disabled. |
| pip / venv | Bundled with Python. If missing: `apt install python3-venv` (auto). |
| Base Python packages | Auto-installed into `base_venv/` on first run. |
| Claude CLI | Auto-detected. Not installed automatically (user's subscription choice). |
| Ollama | Not installed. User points Aeon at existing URL if they have it. |
| Telegram | No binary dependency. Just needs a bot token in config. |

**Graceful degradation:** If a dependency is missing, the feature is disabled — not the whole system.

| Missing | Impact | How to Fix Later |
|---|---|---|
| Python | No evolved skills. DNA tools + LLM still work. | `aeon init --python` |
| All LLM providers | Aeon refuses to start (tells you how to set one up). | Add any provider to config. |
| Telegram token | No remote access. CLI still works. | Add token to config, `aeon restart`. |
| Gemini API key | No audio/image analysis. Text still works. | Add key to config. |

---

## Configuration

After `aeon init`, config is at `~/.aeon/config.yaml` (auto-generated, but editable):

```yaml
# === Provider (at least ONE required) ===
# Uncomment the provider(s) you want to use.
# If multiple are configured, Aeon routes intelligently between them.
# If only one is configured, ALL requests go through it.

provider:
  # Option 1: Claude CLI (free — uses your Anthropic subscription)
  claude_cli:
    enabled: true
    binary: claude                   # path to claude binary
    timeout: 120s
    flags:
      - --dangerously-skip-permissions
      - --no-chrome

  # Option 2: Anthropic API (pay-per-token, no subscription needed)
  # anthropic:
  #   enabled: true
  #   api_key: ${ANTHROPIC_API_KEY}
  #   default_model: claude-sonnet-4-6
  #   fast_model: claude-haiku-4-5-20251001  # used for lightweight ops

  # Option 3: Gemini API (free tier available, good multimodal)
  # gemini:
  #   enabled: true
  #   api_key: ${GEMINI_API_KEY}
  #   default_model: gemini-2.0-flash

  # Option 4: OpenAI-compatible (Ollama, OpenRouter, LM Studio, etc.)
  # openai_compat:
  #   enabled: true
  #   base_url: http://localhost:11434/v1  # Ollama example
  #   api_key: ollama                       # some need a dummy key
  #   default_model: llama3.1

# === Routing (only matters if 2+ providers configured) ===
routing:
  # Which provider handles what (auto-detected if only one provider)
  primary: claude_cli       # normal conversation + complex reasoning
  fast: anthropic           # memory summaries, cron checks (cheap + fast)
  multimodal: gemini        # audio/image analysis
  fallback: anthropic       # when primary is unavailable

# === Telegram (optional) ===
channels:
  telegram:
    enabled: true
    bot_token: ${TELEGRAM_BOT_TOKEN}
    allowed_users:
      - 123456789                    # your Telegram user ID

# === Security ===
security:
  approval_timeout: 60s
  deny_patterns:                     # add custom patterns
    - "rm -rf /"
    - "mkfs"
  allowed_paths:
    - ~/.aeon
    - ~/projects

# === Skill Runtime ===
skills:
  base_packages:                     # pre-installed in base_venv
    - requests
    - httpx
    - beautifulsoup4
    - pyyaml
  warm_pool_size: 3                  # keep N skill processes alive
  max_retries: 3                     # auto-disable after N failures

# === Scheduler ===
scheduler:
  max_concurrent: 3
  auto_pause_after_failures: 5

# === Memory ===
memory:
  auto_save: true
  compaction_threshold: 10           # summarize after N messages
```

---

## Running Aeon

### Interactive (CLI Mode)

```bash
aeon
```

```
🌱 Aeon v0.1.0 — The Self-Evolving Kernel
   Provider: claude-cli (persistent)
   Skills: 3 loaded, 2 warm
   Cron: 1 active job
   Memory: 47 entries

> hello, what can you do?

[Aeon] I'm your autonomous assistant running on this server.
I can run commands, read/write files, search the web, and
create my own tools. I currently have 3 custom skills:
- uptime_check: monitors website availability
- log_analyzer: parses nginx logs for errors
- weather_fetch: gets weather for a location

What would you like me to do?

> check if my blog is up

[Aeon] ✓ blog.example.com is up (200 OK, 342ms response time)
```

### Daemon Mode (VPS — Headless)

```bash
# Run in background (Telegram-only, no CLI)
aeon serve

# Or via systemd (recommended for VPS)
sudo systemctl enable aeon
sudo systemctl start aeon
```

Systemd service file (auto-generated by `aeon init --systemd`):

```ini
[Unit]
Description=Aeon Agentic Kernel
After=network.target

[Service]
Type=simple
User=aeon
ExecStart=/usr/local/bin/aeon serve
Restart=always
RestartSec=5
Environment=ANTHROPIC_API_KEY=sk-...
Environment=TELEGRAM_BOT_TOKEN=...

[Install]
WantedBy=multi-user.target
```

### Desktop Mode

Same as CLI mode. Run `aeon` in a terminal. Optionally enable Telegram for mobile access while away from desk.

---

## Daily Usage

### Via CLI

```
> [any natural language request]     # ask anything
> /status                            # show skills, cron jobs, memory stats
> /skills                            # list all evolved skills
> /cron                              # list scheduled jobs
> /new                               # start fresh conversation (memory persists)
> /stop                              # cancel all running tasks
> /approve                           # approve pending dangerous command
> /deny                              # deny pending dangerous command
```

### Via Telegram

Same as CLI — just send messages to your bot. Plus:

- **Voice notes** → auto-transcribed (via Gemini) → processed as text
- **Images** → auto-analyzed (via Gemini) → description in context
- **Approval buttons** → inline "Allow / Deny" for dangerous commands
- **Progress updates** → "thinking..." indicator for long tasks

### Example Workflows

**Create a new tool:**
```
> create a tool that monitors my nginx error log and alerts me
  if there are more than 10 errors in the last 5 minutes

[Aeon] I'll create that. Let me write the skill...
[Aeon] ✓ Created skill: nginx_error_monitor
       - Reads /var/log/nginx/error.log
       - Counts errors in 5min window
       - Returns alert if threshold exceeded
       Tested successfully. Want me to schedule it?

> yes, run it every 5 minutes

[Aeon] ✓ Scheduled: nginx_error_monitor every 5 minutes
```

**Ask about your system:**
```
> how much disk space do I have left?

[Aeon] Running `df -h`...
       /dev/sda1: 14G used / 25G total (56%)
       You have about 11G free.
```

**Dangerous command approval:**
```
> clean up old docker images to free space

[Aeon] I'll run: docker system prune -af
       ⚠️ This will remove all unused images, containers, and networks.
       [Allow] [Deny]

(you tap Allow on Telegram)

[Aeon] ✓ Reclaimed 3.2 GB of disk space.
```

**Memory:**
```
> remember that the production database is at db.internal:5432
  and the password is in /etc/secrets/db_pass

[Aeon] ✓ Stored to memory (category: core)

(next day, new conversation)

> how do I connect to the production database?

[Aeon] From my memory: the production DB is at db.internal:5432.
       The password is stored at /etc/secrets/db_pass.
       Want me to test the connection?
```

---

## Skill Management

### List Skills
```
> /skills

Evolved Skills (3):
  ✓ uptime_check      — monitors website availability    (used 142 times)
  ✓ log_analyzer       — parses nginx error logs          (used 38 times)
  ✗ weather_fetch      — gets weather data [DISABLED: 3 failures]

Cron Jobs (1):
  ✓ uptime_check       — every 1h   (last: 5 min ago, next: 55 min)
```

### Manually Manage Skills
```
> re-enable the weather_fetch skill

[Aeon] ✓ Re-enabled weather_fetch. I'll monitor it — if it fails
       3 more times it will be disabled again.

> delete the weather_fetch skill

[Aeon] ⚠️ This will permanently remove the skill and its files.
       [Confirm] [Cancel]
```

### Skills on Disk
```bash
ls ~/.aeon/skills/
# uptime_check/
# log_analyzer/
# weather_fetch/

cat ~/.aeon/skills/uptime_check/SKILL.md
# ---
# name: uptime_check
# description: Check if a website is responding
# params:
#   url: {type: string, required: true}
# deps: []
# created: 2026-02-27
# uses: 142
# ---
# Sends HTTP GET to the given URL, returns status code and response time.
```

You can also manually edit skill files — Aeon picks up changes on next load.

---

## Monitoring & Troubleshooting

### Health Check
```
> /status

Aeon v0.1.0 — uptime: 4d 12h 33m
Provider:  claude-cli (persistent, pid 1234, uptime 2h)
           haiku (api, healthy)
           gemini (api, healthy)
Skills:    3 loaded, 2 enabled, 1 disabled
Warm Pool: 2/3 processes active
Cron:      1 job active, 0 paused
Memory:    47 entries (12 core, 35 conversation)
Database:  aeon.db — 2.3 MB
Last Error: weather_fetch failed 3h ago (HTTP timeout)
```

### Logs
```bash
# Live logs
tail -f ~/.aeon/logs/aeon.log

# Filter for errors
grep "level=error" ~/.aeon/logs/aeon.log

# Provider latency
grep "provider_latency" ~/.aeon/logs/aeon.log
```

### Common Issues

| Problem | Cause | Fix |
|---|---|---|
| "claude binary not found" | CLI not installed or not in PATH | Install Claude CLI, check `which claude` |
| "provider timeout" | Claude CLI hanging | Check internet, restart: `aeon restart` |
| Skill keeps failing | Bad generated code | `/skills` → check error, ask Aeon to fix it, or edit manually |
| Telegram not responding | Bot token wrong or polling failed | Check config, check `aeon logs` |
| High memory usage | Too many warm pool processes | Lower `warm_pool_size` in config |
| Slow responses | CLI subprocess restarting frequently | Check logs for crash loops, consider API fallback |

### Recovery
```bash
# Restart cleanly
sudo systemctl restart aeon

# Reset conversation (keeps skills, memory, cron)
aeon reset --conversation

# Full reset (nuclear option — keeps config only)
aeon reset --all

# Backup everything
tar -czf aeon-backup.tar.gz ~/.aeon/
```

---

## Security Notes

- **Single-user only.** Don't share your Telegram bot token or expose the CLI to other users.
- **Telegram `allowed_users`** is critical. Without it, anyone who finds your bot can control your server.
- **Review skills.** Aeon generates Python code. Periodically check `~/.aeon/skills/` for anything unexpected.
- **Approval gate.** Never auto-approve in config. Always review dangerous commands.
- **Backups.** The `aeon.db` file is your brain. Back it up.
- **API keys.** Use environment variables, not plaintext in config. The config file supports `${ENV_VAR}` syntax.
