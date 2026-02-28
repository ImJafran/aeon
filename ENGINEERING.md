# Engineering Deep Dive

This document covers Aeon's architecture, internals, security model, and how it compares to other agentic frameworks. For getting started, see the [README](README.md). For contributing, see [DEVELOPMENT.md](DEVELOPMENT.md).

---

## Table of Contents

- [Architecture](#architecture)
- [Agent Loop](#agent-loop)
- [Built-in Tools](#built-in-tools)
- [Evolved Skills](#evolved-skills)
- [Memory System](#memory-system)
- [Security Model](#security-model)
- [Scheduler](#scheduler)
- [Subagents](#subagents)
- [Provider Chain](#provider-chain)
- [Project Structure](#project-structure)
- [Key Patterns](#key-patterns)
- [Comparison with Other Agents](#comparison-with-other-agents)
- [Performance](#performance)
- [Monitoring & Troubleshooting](#monitoring--troubleshooting)

---

## Architecture

### High-Level Flow

```
You (Telegram / CLI)
        |
        v
   Message Bus ──────────── Channels (telegram, cli)
        |
        v
   Agent Loop ───────────── History + Memory (SQLite FTS5)
        |
        v
   Provider Chain ───────── Anthropic | Gemini | OpenAI-compat
        |
        v
   Tool Execution ───────── DNA Tools | Evolved Skills | Subagents
```

### Core Components

```
┌──────────────────────────────────────────────────────┐
│                    AEON KERNEL (Go)                   │
├──────────────────────────────────────────────────────┤
│                                                      │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────┐ │
│  │  Message Bus │──│  Agent Loop  │──│  Provider   │ │
│  │  (In/Out)    │  │  (Core)      │  │  Chain      │ │
│  └──────┬───────┘  └──────┬───────┘  └─────────────┘ │
│         │                 │                           │
│  ┌──────┴───────┐  ┌──────┴───────┐  ┌─────────────┐ │
│  │  Channels    │  │ Tool Registry│──│  Security   │ │
│  │  - Telegram  │  │  - DNA tools │  │  Policy     │ │
│  │  - CLI       │  │  - Evolved   │  └─────────────┘ │
│  └──────────────┘  └──────┬───────┘                   │
│                           │          ┌─────────────┐  │
│  ┌──────────────┐  ┌──────┴───────┐  │  Scheduler  │  │
│  │  Memory      │  │  Skill Mgr   │  │  (Cron)     │  │
│  │  (SQLite)    │  │  - Loader    │  └─────────────┘  │
│  └──────────────┘  │  - Factory   │                   │
│                    └──────────────┘                   │
└──────────────────────────────────────────────────────┘
```

All communication flows as `InboundMessage -> AgentLoop -> OutboundMessage`. Channels publish inbound messages; the agent loop publishes outbound. Routing is by `channel + chatID` pair.

---

## Agent Loop

The core request-response cycle (`internal/agent/loop.go`):

1. User message arrives on bus (Telegram or CLI)
2. Slash commands handled separately (`/status`, `/model`, `/new`, `/cost`, `/help`, `/skills`, `/stop`)
3. For normal messages:
   - Add to in-memory history
   - Build system prompt dynamically (see below)
   - Call LLM provider with history + tool definitions

4. **Tool execution loop** (max 20 iterations per turn):
   - If LLM returns tool calls → execute all tools (parallel when independent)
   - Emit status updates to user ("Running shell...", "Storing memory...")
   - For each tool: scrub credentials from output, handle approval flow if needed
   - Append results as `tool` role messages, loop back to LLM
   - If LLM returns text (no tool calls) → send to user, save to history, exit

### System Prompt Construction

Built dynamically at every turn, layered:

1. **SOUL.md** — agent identity/personality (`~/.aeon/workspace/SOUL.md`)
2. **AGENT.md** — behavior rules (`~/.aeon/workspace/AGENT.md`)
3. **config.yaml `system_prompt`** — user overrides
4. **Memory context** — relevant memories injected from FTS5 search on user message
5. **Runtime context** — current provider, timestamp, skill count, active tasks, recent errors
6. **Safety boundary** — warning about trusting tool output (prompt injection defense)

### History Management

- Resumed from DB on startup (session persistence across restarts)
- In-memory buffer trimmed to keep the most recent N messages
- All user/assistant messages persisted to SQLite
- Tool messages NOT persisted (avoids cross-provider tool ID conflicts)

---

## Built-in Tools

Aeon ships with 15 DNA tools compiled into the binary:

### Shell & System

| Tool | Description |
|---|---|
| `shell_exec` | Execute shell commands with full system access. Max 600s timeout. Subject to deny/approval patterns. |
| `log_read` | Read agent's own log file for diagnostics. Filter by keyword, max 200 lines. |

### File Operations

| Tool | Description |
|---|---|
| `file_read` | Read file contents (max 100KB). Supports offset/limit for large files. Path-checked. |
| `file_write` | Create or overwrite files. Path-checked, resolves symlinks. |
| `file_edit` | In-place text substitution. Same security model as file_read/write. |

### Web

| Tool | Description |
|---|---|
| `web_read` | Fetch and parse web pages via Jina Reader API. Returns markdown (max 8000 chars). Blocks private IPs. |

### Memory

| Tool | Description |
|---|---|
| `memory_store` | Persist information to SQLite FTS5. Categories: core, daily, lesson, correction, custom. Importance scoring (0.0-1.0). |
| `memory_recall` | Search long-term memory with keyword queries. FTS5 with OR semantics, ranked by importance. |

### Skills (Evolved Tools)

| Tool | Description |
|---|---|
| `skill_factory` | Create or update Python/Bash skills. Writes code, installs deps, auto-tests. Circuit breaker resets on update. |
| `find_skills` | List available skills with optional search. Shows health status (healthy/DISABLED). |
| `read_skill` | Inspect skill metadata, parameters, dependencies, source code. |
| `run_skill` | Execute an installed skill with JSON parameters. Subprocess with timeout and circuit breaker. |

### Scheduling

| Tool | Description |
|---|---|
| `cron_manage` | Create, list, pause, resume, delete scheduled jobs and reminders. Supports `in 10m`, `at 4:50pm`, `every 1h`, `daily`, etc. |

### Background Tasks

| Tool | Description |
|---|---|
| `spawn_agent` | Launch independent background task. Simplified agent loop, max 15 iterations, full tool access. |
| `list_tasks` | Show active background tasks with IDs and status. |

---

## Evolved Skills

Skills are AI-generated Python/Bash scripts that the agent creates at runtime using `skill_factory`. This is what makes Aeon self-evolving — it writes its own tools to fill capability gaps.

### Skill Structure

```
~/.aeon/skills/my_skill/
  ├── SKILL.md           # YAML frontmatter + description
  ├── main.py            # Python entrypoint (JSON stdin → JSON stdout)
  ├── requirements.txt   # pip dependencies
  └── lib/               # pip install --target lib
```

### Lifecycle

1. **Create** — agent calls `skill_factory` with name, description, code, parameters, dependencies
2. **Auto-test** — skill is tested immediately after creation
3. **Load** — on startup, scans `~/.aeon/skills/`, parses metadata, validates entrypoints
4. **Execute** — `run_skill` spawns subprocess with JSON I/O, timeout, environment variables
5. **Health** — circuit breaker disables after 3 consecutive failures; success resets counter
6. **Update** — `skill_factory` with `update=true` increments version, resets health

### Execution Model

- **JSON I/O**: skill reads params from stdin, writes JSON result to stdout
- **Dependencies**: pip packages installed to `skill/lib/` via `--target`
- **Shared venv**: `~/.aeon/base_venv/` available for all skills (requests, httpx, beautifulsoup4, pyyaml)
- **Timeout**: configurable per skill (default 30s)
- **Credential scrubbing**: applied to skill output before the LLM sees it

---

## Memory System

SQLite with FTS5 full-text search (`internal/memory/store.go`).

### Interface

| Operation | Description |
|---|---|
| `store` | Save a memory with content, category, tags, importance |
| `recall` | Search by keywords (FTS5 OR semantics), ranked by importance |
| `get` | Retrieve a specific memory by ID |
| `list` | List memories by category |
| `forget` | Delete a memory |
| `count` | Count memories by category |

### Categories

- **core** — fundamental facts about the user/system (importance: 0.8)
- **daily** — day-to-day observations
- **lesson** — learned from mistakes (importance: 0.85)
- **correction** — user corrections (importance: 0.9)
- **conversation** — auto-saved significant messages
- **custom** — user-defined

### Context Injection

At every turn, the agent searches memory using the user's message as a query. Matching memories are injected into the system prompt, giving the agent persistent context across sessions.

### Tuning

SQLite configured with WAL mode, memory-mapped I/O, and in-memory temp tables for performance.

---

## Security Model

Six layers of defense (`internal/security/policy.go`):

### 1. Command Deny-List (Hard Block)

Regex patterns that are **always blocked**, no override:

- `rm -rf /` and `rm -rf /*` (filesystem wipe)
- `mkfs.*`, `dd if=.*of=/dev/[sv]d` (disk format/overwrite)
- Fork bomb: `:(){ :|:& };`
- Redirect to disk device: `>/dev/[sv]d`
- Self-termination: `systemctl stop aeon`, `pkill aeon`, `killall aeon`

### 2. Approval Gate

Commands matching approval patterns require explicit user confirmation:

- `curl ... | sh/bash` (pipe-to-shell)
- `wget ... | sh/bash` (pipe-to-shell)

Flow: tool returns `NeedsApproval` → user gets approve/deny buttons → 60s timeout → re-execute if approved.

### 3. Credential Scrubbing

Applied to **all** tool output before it enters conversation history:

- API keys: Anthropic, OpenAI, GitHub PAT, Google, Stripe, AWS, Telegram, GitLab
- Tokens: Bearer, JWT patterns
- Database URLs: postgres://, mysql://, mongodb://
- Private keys: RSA, EC, OPENSSH
- Generic: high-entropy 20+ char strings after `key=`, `token=`, `password=`
- All matches replaced with `[REDACTED]`

### 4. Path Containment

File operations checked against allowed directories (configurable in `config.yaml`). Symlinks resolved before checking.

### 5. Failure Circuit Breaker

Skills automatically disabled after 3 consecutive failures. Prevents runaway loops. Reset on successful update.

### 6. Process Limits

Timeout + process tree kill for runaway scripts. Configurable per skill and per shell command (max 600s).

---

## Scheduler

SQLite-backed cron job store (`internal/scheduler/scheduler.go`).

### Schedule Expressions

| Format | Example | Type |
|---|---|---|
| `in Xm/Xh` | `in 10m`, `in 2h` | One-shot (delay from now) |
| `at HH:MM` | `at 16:50`, `at 4:50pm` | One-shot (next occurrence) |
| `every Xm/Xh/Xd` | `every 5m`, `every 1h` | Recurring |
| Named | `hourly`, `daily`, `weekly` | Recurring |

### Behavior

- Tick loop runs every 60 seconds
- One-shot jobs auto-pause after firing
- Recurring jobs compute next_run after each execution
- Jobs auto-pause after 5 consecutive failures
- Max concurrency enforced (default 3)
- No re-entrant stacking (skip if already running)

---

## Subagents

Background task delegation (`internal/agent/subagent.go`).

- **Spawn**: creates independent agent loop with full tool access
- **Limits**: max 3 concurrent, max 15 iterations per task
- **Result**: posted to original channel on completion
- **Cancel**: via `/stop` command or `StopAll()`
- Credential scrubbing applied to task output

---

## Provider Chain

At least one LLM provider is required. Multiple providers enable routing and failover.

| Provider | Interface | Notes |
|---|---|---|
| **Z.ai** | OpenAI-compatible | GLM models (default primary) |
| **Anthropic** | Native Messages API | Claude models, strict user/assistant alternation |
| **Gemini** | OpenAI-compatible | Also handles audio transcription and TTS |
| **OpenAI-compatible** | Standard API | Ollama, LM Studio, vLLM, OpenRouter, etc. |

### Routing

Three roles assignable in `config.yaml`:

- **primary** — default for all requests
- **fast** — used for lightweight tasks (e.g., memory consolidation)
- **fallback** — automatic failover if primary fails

If a role has no assigned provider, it falls back to `primary`.

---

## Project Structure

```
cmd/aeon/
  main.go                  # entrypoint — interactive, serve, init, uninstall

internal/
  agent/
    loop.go                # core agent loop (message handling, tool execution, history)
    subagent.go            # parallel subagent delegation
    approval.go            # dangerous command approval workflow
    cost_tracker.go        # LLM token usage tracking

  bootstrap/
    init.go                # system detection, dependency install, workspace setup
    deps.go                # dependency injection — builds all shared services

  bus/
    bus.go                 # message bus — channels produce, agent loop consumes

  channels/
    telegram.go            # Telegram bot (long-polling, typing indicator, voice)
    cli.go                 # local terminal interface
    channel.go             # channel interface
    transcribe.go          # Gemini audio transcription

  config/
    config.go              # YAML config loading, provider routing

  memory/
    store.go               # SQLite FTS5 memory + conversation history
    consolidate.go         # history compaction (LLM summarization)

  providers/
    anthropic.go           # Anthropic native Messages API
    openai_compat.go       # Gemini / Ollama / any OpenAI-compatible endpoint
    chain.go               # provider chain with routing and failover
    factory.go             # provider construction from config

  scheduler/
    scheduler.go           # cron jobs + one-shot reminders

  security/
    policy.go              # command deny-lists, path containment, credential scrubbing

  skills/
    loader.go              # skill discovery, loading, warm pool

  tools/
    shell_exec.go          # shell command execution
    memory_tools.go        # memory store/recall
    skill_tools.go         # skill factory, find, read, run
    cron_tools.go          # cron job management
    log_tools.go           # log reading
    registry.go            # tool registry

deploy/
  aeon.service             # systemd service unit
  install.sh               # installation script

config.example.yaml        # example configuration
Dockerfile                 # multi-stage build (builder + runtime)
docker-compose.yml         # dev, test, serve services
Makefile                   # build targets
```

---

## Key Patterns

### Anthropic API Message Alternation

Anthropic requires strict user/assistant alternation. Consecutive tool results must be merged into one `user` message with multiple `tool_result` blocks.

### Memory Search (FTS5 OR Semantics)

Memory uses FTS5 with keyword extraction, not raw queries. Keywords are extracted from the user's message and searched with OR logic. Results ranked by importance score.

### Session Persistence

Session ID is resumed from DB on restart so conversation history persists. Tool calls go into both the local `messages` slice and the persistent `history` table.

### Provider Switching

The `/model` command clears conversation history to avoid cross-provider tool call ID mismatches. Different providers use different tool call ID formats.

### Credential Scrubbing

A regex-based scrubber strips API keys and tokens from tool output before it enters conversation history. This prevents accidental key leakage through the LLM.

### Tool Result Dual Surface

`ToolResult{ForLLM, ForUser, Silent}` separates what the model sees from what the user sees. Allows verbose output for the LLM while keeping user messages clean.

### Graceful Degradation

Missing dependencies disable features, not the system:
- No Python → skills disabled, DNA tools still work
- No Telegram token → CLI only
- No ffmpeg → voice transcription unavailable
- Single provider fails → fallback provider takes over

---

## Comparison with Other Agents

Aeon compared against other agentic frameworks from the same research lineage:

<table>
<thead>
<tr>
<th>Feature</th>
<th><strong>Aeon</strong></th>
<th>IronClaw</th>
<th>ZeroClaw</th>
<th>PicoClaw</th>
<th>NanoBot</th>
<th>NanoClaw</th>
<th>OpenClaw</th>
<th>TinyClaw</th>
</tr>
</thead>
<tbody>
<tr>
<td><strong>Language</strong></td>
<td><strong>Go</strong></td>
<td>Rust</td>
<td>Rust</td>
<td>Go</td>
<td>Python</td>
<td>TypeScript</td>
<td>TypeScript</td>
<td>TypeScript (Bun)</td>
</tr>
<tr>
<td><strong>Binary Size</strong></td>
<td><strong>~10 MB</strong></td>
<td>~8 MB</td>
<td>~7 MB</td>
<td>~12 MB</td>
<td>N/A (interpreted)</td>
<td>N/A (interpreted)</td>
<td>N/A (interpreted)</td>
<td>N/A (interpreted)</td>
</tr>
<tr>
<td><strong>Dependencies</strong></td>
<td><strong>2 direct</strong></td>
<td>~45</td>
<td>~152</td>
<td>~20</td>
<td>~26</td>
<td>~9</td>
<td>~58</td>
<td>1*</td>
</tr>
<tr>
<td><strong>CGO-Free</strong></td>
<td><strong>Yes</strong></td>
<td>N/A</td>
<td>N/A</td>
<td>Yes</td>
<td>N/A</td>
<td>N/A</td>
<td>N/A</td>
<td>N/A</td>
</tr>
<tr>
<td><strong>Channels</strong></td>
<td><strong>2 (Telegram, CLI)</strong></td>
<td>2</td>
<td>2</td>
<td>15+</td>
<td>13+</td>
<td>1</td>
<td>27+</td>
<td>3</td>
</tr>
<tr>
<td><strong>Self-Evolving</strong></td>
<td><strong>Yes</strong></td>
<td>Yes</td>
<td>No</td>
<td>No</td>
<td>No</td>
<td>No</td>
<td>No</td>
<td>No</td>
</tr>
<tr>
<td><strong>Memory</strong></td>
<td><strong>SQLite FTS5</strong></td>
<td>SQLite</td>
<td>SQLite</td>
<td>SQLite</td>
<td>JSON file</td>
<td>JSON file</td>
<td>SQLite</td>
<td>SQLite</td>
</tr>
<tr>
<td><strong>Security Model</strong></td>
<td><strong>Deny-list + Approval + Scrub</strong></td>
<td>WASM sandbox + AES-256-GCM</td>
<td>ChaCha20 + Autonomy levels</td>
<td>OAuth + Token refresh</td>
<td>Shell deny patterns</td>
<td>Container mount security</td>
<td>Secret providers + detect-secrets</td>
<td>SHIELD.md + TOTP + 3-tier</td>
</tr>
<tr>
<td><strong>Scheduler</strong></td>
<td><strong>Cron + Reminders</strong></td>
<td>Yes</td>
<td>Yes</td>
<td>Yes</td>
<td>Yes</td>
<td>No</td>
<td>Yes</td>
<td>Yes</td>
</tr>
<tr>
<td><strong>Provider Agnostic</strong></td>
<td><strong>Yes</strong></td>
<td>Yes</td>
<td>Yes</td>
<td>Yes</td>
<td>Yes</td>
<td>No</td>
<td>Yes</td>
<td>Yes</td>
</tr>
<tr>
<td><strong>Docker Required</strong></td>
<td><strong>No</strong></td>
<td>No</td>
<td>No</td>
<td>No</td>
<td>No</td>
<td>Yes</td>
<td>No</td>
<td>No</td>
</tr>
<tr>
<td><strong>Offline Support</strong></td>
<td><strong>Yes (Ollama)</strong></td>
<td>Yes</td>
<td>Yes</td>
<td>Yes</td>
<td>No</td>
<td>No</td>
<td>No</td>
<td>No</td>
</tr>
</tbody>
</table>

<sub>* TinyClaw uses a monorepo with 1 root dependency.</sub>

**Key differentiators:**
- Aeon and IronClaw are the only self-evolving agents (write their own tools at runtime)
- Aeon has the fewest dependencies (2 direct: `yaml.v3` and `modernc.org/sqlite`)
- Aeon produces a single static binary — no runtime, no VM, no interpreter
- Full offline support via Ollama (local models)

---

## Performance

### Binary

- **Release build**: ~10 MB (`CGO_ENABLED=0`, `-ldflags "-s -w"`)
- **Debug build**: ~15 MB (with symbols)
- **Cross-compile**: linux/amd64, linux/arm64 via `make build-linux`

### Startup

- Cold start: sub-second (single binary, SQLite opens instantly)
- Session resume: loads prior history from DB
- Skill loading: scans `~/.aeon/skills/` directory at startup

### Runtime

- SQLite: WAL mode, memory-mapped I/O, in-memory temp tables
- Tool execution: parallel when independent (Go goroutines)
- Skill warm pool: keeps frequently-used Python processes alive
- Memory: ~20-40 MB resident depending on history size

---

## Monitoring & Troubleshooting

### Logs

```bash
# Read recent logs
aeon                       # logs print to stderr in CLI mode
journalctl -u aeon -f      # systemd service logs

# The agent can read its own logs via the log_read tool
```

### Database

All state lives in `~/.aeon/aeon.db` (SQLite):

```bash
sqlite3 ~/.aeon/aeon.db ".tables"
# memories, conversation_history, cron_jobs
```

### Common Issues

| Issue | Cause | Fix |
|---|---|---|
| "no providers configured" | Missing API keys | Add keys to `~/.aeon/config.yaml` |
| Skills disabled | 3+ consecutive failures | Fix the skill code, call `skill_factory` with `update=true` |
| Cron jobs not firing | Auto-paused after 5 failures | Check `cron_manage list`, fix underlying issue |
| Voice not working | Missing ffmpeg | Run `aeon init` to install |
| Memory search empty | Wrong query format | Memory uses keyword extraction, not natural language |

### Backup

```bash
# Back up all Aeon state
cp -r ~/.aeon ~/.aeon-backup

# Or just the database
cp ~/.aeon/aeon.db ~/aeon-backup.db
```

---

## Author

Created by **[Jafran Hasan](https://linkedin.com/in/iamjafran)** ([@imjafran](https://github.com/ImJafran))

## License

[MIT](LICENSE)
