# Aeon

A self-evolving AI agent that lives on your server. Single Go binary, zero dependencies, talks to you through Telegram.

Aeon starts with a fixed set of DNA tools (shell, files, memory, scheduling) and grows by writing its own Python skills — tested, registered, and managed autonomously.

## How it works

```
You (Telegram/CLI) → Message Bus → Agent Loop → LLM Provider → Tool Execution
                                                                      ↓
                                                              Skills / Shell / Memory / Cron
```

- **Provider-agnostic** — Anthropic, Gemini, OpenAI-compatible (Ollama), or Claude CLI
- **Multi-provider routing** — primary, fast, fallback tiers with automatic failover
- **Persistent memory** — SQLite with FTS5 search, survives restarts
- **Self-evolving skills** — LLM writes Python/Bash scripts, validates them, registers as tools
- **Scheduling** — recurring cron jobs + one-shot reminders (`in 10m`, `at 4:50pm`)
- **Voice support** — Telegram voice messages transcribed via Gemini

## Quick Start

### Prerequisites

- Go 1.24+ (build only)
- Docker + Docker Compose (recommended for dev)

### Setup

```bash
git clone https://github.com/jafran/aeon.git
cd aeon

# Create config (see config section below)
cp config.example.yaml config.yaml
# Edit config.yaml with your API keys and Telegram token

# Run tests
docker compose run --rm test

# Start (Telegram daemon)
docker compose up telegram -d

# Check logs
docker compose logs telegram --tail 50
```

### Without Docker

```bash
make build
./bin/aeon init
./bin/aeon          # interactive CLI
./bin/aeon serve    # Telegram daemon
```

## Configuration

All config lives in `config.yaml` (YAML, gitignored). No `.env` files.

```yaml
provider:
  anthropic:
    enabled: true
    api_key: sk-ant-...
    default_model: claude-sonnet-4-6
    fast_model: claude-haiku-4-5-20251001
  gemini:
    enabled: true
    api_key: AIza...
    default_model: gemini-2.5-flash-lite
    audio_model: gemini-2.5-flash-native-audio-preview-12-2025
    tts_model: gemini-2.5-flash-preview-tts

channels:
  telegram:
    enabled: true
    bot_token: "123456:ABC..."
    allowed_users: [your_telegram_id]

routing:
  primary: gemini
  fast: anthropic_fast
  fallback: anthropic

agent:
  system_prompt: |
    You are Aeon, a persistent AI assistant...
```

## Commands

In Telegram or CLI:

| Command | Description |
|---------|-------------|
| `/status` | System info — provider, tools, memory, active tasks |
| `/model` | Switch LLM provider at runtime |
| `/model gemini` | Switch to Gemini |
| `/new` | Clear conversation history |
| `/stop` | Cancel running tasks |
| `/help` | List commands |

## Architecture

```
cmd/aeon/main.go          Entrypoint (interactive, serve, init)
internal/
  agent/loop.go            Core agent loop — message → LLM → tools → response
  agent/subagent.go        Parallel task delegation
  providers/               LLM providers (Anthropic, Gemini/OpenAI-compat, chain)
  channels/telegram.go     Telegram bot (long-polling, typing indicator, voice)
  channels/transcribe.go   Voice transcription via Gemini
  channels/cli.go          Local terminal interface
  tools/                   DNA tools (shell, files, memory, cron, skills)
  memory/store.go          SQLite FTS5 memory + conversation history
  scheduler/scheduler.go   Cron jobs + one-shot reminders
  skills/                  Skill loader and venv management
  security/                Command deny-list, path containment, credential scrubbing
  config/config.go         YAML config loading
  bus/                     Message bus (channels ↔ agent loop)
```

### DNA Tools (built-in)

| Tool | What it does |
|------|-------------|
| `shell_exec` | Run shell commands (with deny-pattern filter) |
| `file_read` / `file_write` / `file_edit` | File operations |
| `memory_store` / `memory_recall` | Long-term memory (FTS5 search) |
| `cron_manage` | Schedule recurring jobs or one-shot reminders |
| `skill_factory` | Create new Python/Bash skills |
| `find_skills` / `read_skill` / `run_skill` | Manage evolved skills |
| `spawn_agent` / `list_tasks` | Parallel subagent delegation |

### Evolved Skills (AI-generated)

Stored in `~/.aeon/skills/`. Each skill is a directory with:
- `SKILL.md` — metadata (name, description, params, deps)
- `main.py` or `main.sh` — entry point
- `lib/` — extra pip dependencies (overlay on shared base venv)

Skills receive JSON on stdin, return JSON on stdout. Auto-disabled after 3 consecutive failures.

## Development

```bash
make build          # build binary
make test           # run tests
make build-linux    # cross-compile for Linux amd64/arm64
make lint           # golangci-lint

# Docker
docker compose run --rm test       # tests in container
docker compose up telegram -d      # start bot
docker compose logs telegram -f    # follow logs
docker compose down                # stop
```

## Deployment

Build for your VPS target and deploy the binary:

```bash
make build-linux
scp bin/aeon-linux-amd64 yourserver:/usr/local/bin/aeon
ssh yourserver 'aeon init && aeon serve'
```

Or use the Dockerfile for container deployment.

## Docs

- [Architecture](ARCHITECTURE.md) — detailed system design
- [User Guide](USER_GUIDE.md) — setup, config, daily usage, troubleshooting

## License

MIT
