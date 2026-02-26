# Aeon Development Plan

## Overview

Phased build plan. Each phase produces a working, testable system. No phase depends on future work — you can stop after any phase and have something useful.

---

## Phase 0: Project Skeleton & Build System
**Goal:** Single static binary with zero external dependencies. Compilable, CI-ready.

### Tasks
- [ ] Initialize Go module (`github.com/yourusername/aeon`)
- [ ] Set up directory structure:
  ```
  cmd/aeon/main.go
  internal/
    agent/       -- loop, context
    bootstrap/   -- aeon init wizard, dependency detection
    bus/         -- message types, bus
    channels/    -- channel interface + CLI channel
    providers/   -- provider interface + all providers
    tools/       -- tool interface, registry
    skills/      -- skill loader, factory, venv manager
    memory/      -- memory interface + SQLite impl
    security/    -- policy, deny patterns
    scheduler/   -- cron store, ticker
    config/      -- config schema, loader
  workspace/
    AGENT.md
    SOUL.md
  ```
- [ ] Use pure Go SQLite driver (`modernc.org/sqlite`) — no CGo, no external libs, static binary
- [ ] Config file format (YAML): API keys, paths, feature flags, with `${ENV_VAR}` expansion
- [ ] Basic `main.go`: load config, print banner, exit
- [ ] Implement `aeon init` bootstrap wizard:
  - Detect OS and architecture
  - Check for Python 3 → offer to install if missing, or skip (skills disabled)
  - Check for LLM providers (claude binary, env vars for API keys)
  - Interactive provider selection (primary + optional fallback)
  - Optional Telegram setup
  - Generate `~/.aeon/config.yaml` with detected settings
  - Create workspace directory structure
  - Create base_venv if Python available (install common packages)
- [ ] Implement `aeon init --python` (install Python + bootstrap venv later)
- [ ] Makefile: `build`, `run`, `test`, `lint`
  - `build` produces a single static binary (no CGo: `CGO_ENABLED=0`)
- [ ] `.gitignore`, `README.md`

### Design Decisions
- **Pure Go SQLite** (`modernc.org/sqlite`): ~5-10% slower than CGo SQLite, but enables fully static binary with zero system dependencies. Worth the tradeoff for single-binary deployment.
- **Graceful degradation:** Every optional dependency (Python, providers, Telegram) has a "disabled" state. Aeon always starts if at least one provider is configured.

### Exit Criteria
- `make build` produces a **single static binary** (verify: `ldd aeon` shows "not a dynamic executable")
- Binary runs on a fresh Linux box with nothing installed
- `aeon init` detects system state and generates valid config
- `aeon init` on a system without Python → warns, skips skills, proceeds
- `make test` runs

---

## Phase 1: Message Bus + CLI Channel + Agent Loop Shell
**Goal:** Type a message in terminal, see it flow through the bus, get a hardcoded response back.

### Tasks
- [ ] Define `InboundMessage` / `OutboundMessage` types
  ```go
  type InboundMessage struct {
      Channel   string
      ChatID    string
      UserID    string
      Content   string
      MediaType string // text, image, audio
      MediaURL  string
      Timestamp time.Time
  }
  ```
- [ ] Implement `MessageBus` (Go channels — buffered)
- [ ] Implement `CLIChannel`: reads stdin, publishes to bus; subscribes to bus, prints to stdout
- [ ] Implement `AgentLoop` shell:
  - Subscribe to inbound messages
  - For now: echo back with `[Aeon] ` prefix
  - Publish outbound message
- [ ] Wire everything in `main.go`: bus → CLI channel → agent loop

### Exit Criteria
- Run binary, type "hello", see "[Aeon] hello" echoed back
- Clean shutdown on Ctrl+C (context cancellation)

---

## Phase 2: Provider System
**Goal:** Agent loop sends messages to an LLM and returns real responses. At least one provider must work.

### Tasks
- [ ] Define `Provider` interface:
  ```go
  type Provider interface {
      Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
      Name() string
      Available() bool
  }
  type CompletionRequest struct {
      SystemPrompt string
      Messages     []Message
      Tools        []ToolDef
      Hint         string // "fast", "normal", "complex" — for routing
  }
  type CompletionResponse struct {
      Content   string
      ToolCalls []ToolCall
      Usage     TokenUsage
      Provider  string
  }
  ```
- [ ] Implement providers (each is independent — enable whichever you have):
  - **ClaudeCLIProvider** (persistent subprocess):
    - Spawn `claude -p --output-format json --dangerously-skip-permissions --no-chrome -` once
    - Keep alive — stdin/stdout pipe, no per-call spawn
    - Auto-restart on crash (backoff: 1s, 2s, 4s, max 30s)
    - Health check: kill and restart if no response within timeout
  - **AnthropicAPIProvider** (direct HTTP):
    - Standard Anthropic messages API
    - Supports any Claude model (Haiku, Sonnet, Opus)
  - **GeminiAPIProvider** (direct HTTP):
    - Google Generative AI API
    - Supports text + multimodal (audio/image)
  - **OpenAICompatProvider** (direct HTTP):
    - Any OpenAI-compatible endpoint (Ollama, OpenRouter, LM Studio, vLLM, etc.)
    - Configurable base_url + model
    - Enables fully offline operation
- [ ] Implement `ProviderChain` (routing layer):
  ```go
  type ProviderChain struct {
      providers map[string]Provider  // all configured providers
      roles     ProviderRoles        // primary, fast, multimodal, fallback
  }
  ```
  - If only 1 provider configured → all roles point to it (no routing logic)
  - If multiple → route by role: "fast" → fast provider, "normal" → primary, etc.
  - Automatic failover: primary fails → try fallback → error if all fail
- [ ] Update `AgentLoop`:
  - Build system prompt (static for now)
  - Forward user message to provider chain
  - Return LLM response to bus
- [ ] Auto-detection on `aeon init`:
  - Check if `claude` binary exists → offer Claude CLI
  - Check for env vars (`ANTHROPIC_API_KEY`, `GEMINI_API_KEY`) → offer API providers
  - Prompt user to configure at least one
- [ ] Add configurable timeout (default: 120s)

### Exit Criteria
- **With Claude CLI only:** type message, get response
- **With API key only:** type message, get response (no CLI needed)
- **With Ollama only:** type message, get response (fully offline)
- **With multiple:** simple query routes to fast provider (verify via logs)
- Kill the primary provider mid-conversation → fallback activates seamlessly
- Zero providers configured → clear error message telling user how to set one up

---

## Phase 3: Tool System (DNA Tools)
**Goal:** LLM can call tools. Start with `shell_exec` and `file_read`.

### Tasks
- [ ] Define `Tool` interface:
  ```go
  type Tool interface {
      Name() string
      Description() string
      Parameters() json.RawMessage // JSON Schema
      Execute(ctx context.Context, params json.RawMessage) (ToolResult, error)
  }
  type ToolResult struct {
      ForLLM  string // what the model sees
      ForUser string // what gets sent to chat (optional)
      Silent  bool   // suppress user output
  }
  ```
- [ ] Implement `ToolRegistry`:
  - Register/deregister tools
  - Generate sorted tool definitions for provider
  - Lookup by name
- [ ] Implement tool loop in `AgentLoop`:
  1. Call provider
  2. If response has tool calls → execute **independent tools in parallel** (via goroutines) → append results → loop
  3. If text response → send to user → break
  4. Max iterations guard (default: 20)
- [ ] Tool result truncation: cap output at 2000 chars for LLM, store full output in session for `get_full_output` tool
- [ ] Implement `shell_exec` tool:
  - Run `sh -c <command>` via `exec.CommandContext`
  - Timeout (default: 30s)
  - Output truncation (10,000 chars)
  - Process tree termination on timeout
- [ ] Implement `file_read` tool:
  - Read file contents with line limit
  - Size guard (max 100KB)
- [ ] Implement `file_write` tool
- [ ] Implement `file_edit` tool (find/replace)

### Exit Criteria
- Ask Claude "list files in current directory" → it calls `shell_exec` → returns `ls` output
- Ask Claude "read the config file" → it calls `file_read` → returns content
- Tool loop terminates properly (no infinite loops)

---

## Phase 4: Security Policy
**Goal:** Dangerous commands are blocked or require approval.

### Tasks
- [ ] Implement `SecurityPolicy`:
  - Deny-pattern list (compiled regexes)
  - Configurable via YAML
  - Returns `Allowed | Denied | NeedsApproval`
- [ ] Wire into `shell_exec`:
  - Check command against policy before execution
  - If `Denied` → return error to LLM
  - If `NeedsApproval` → hold execution, send approval request
- [ ] Implement credential scrubbing:
  - Regex scan of tool output for API keys, tokens, passwords
  - Replace with `****[REDACTED]`
  - Run on all tool results before they enter conversation history
- [ ] Implement workspace path containment:
  - Configurable allowed paths
  - `file_read/write/edit` enforce boundaries
- [ ] Approval queue (in-memory for now):
  - Store pending approvals
  - CLI channel: prompt "Allow? [y/N]"
  - Timeout → auto-deny (default: 60s)

### Exit Criteria
- `rm -rf /` is blocked, LLM sees error
- `sudo apt install` triggers approval prompt
- API keys in tool output are scrubbed
- File operations outside workspace are denied

---

## Phase 5: Memory System
**Goal:** Aeon remembers things across conversations.

### Tasks
- [ ] Implement SQLite memory store:
  - Tables: `memories(id, category, content, embedding, created_at, accessed_at)`
  - Categories: core, daily, conversation, custom
  - FTS5 index for keyword search
- [ ] SQLite performance tuning (applied at connection open):
  ```sql
  PRAGMA journal_mode=WAL;
  PRAGMA synchronous=NORMAL;
  PRAGMA cache_size=-8000;
  PRAGMA mmap_size=67108864;
  PRAGMA temp_store=MEMORY;
  ```
- [ ] Implement `memory_store` tool:
  - Store text with category and optional tags
- [ ] Implement `memory_recall` tool:
  - Keyword search via FTS5
  - Return top-N results with relevance scores
- [ ] Wire into agent loop context builder:
  - On each message, auto-recall relevant memories
  - Prepend to system prompt as context
- [ ] Auto-save: store significant user messages (>20 chars) to conversation memory
- [ ] Implement history compaction:
  - Trigger at 75% context window or >10 messages
  - LLM-summarize older messages (route to Haiku if available — fast + cheap)
  - Fallback: deterministic truncation (drop oldest 50%, append compression note)

### Phase 5b: Vector Search (Optional Enhancement)
- [ ] Add sqlite-vec for vector embeddings
- [ ] Embedding generation via local model or Gemini API
- [ ] Hybrid search: FTS5 keyword + vector cosine similarity + rank fusion

### Exit Criteria
- Tell Aeon "remember that my server IP is 1.2.3.4"
- Start new conversation, ask "what's my server IP" → recalls it
- Long conversations don't crash from context overflow

---

## Phase 6: Skill System (Evolved Tools)
**Goal:** Aeon can write, test, and register its own Python/Bash tools.

### Tasks
- [ ] Implement skill directory structure:
  ```
  ~/.aeon/
    base_venv/                  -- shared Python env (requests, httpx, bs4, pyyaml)
    skills/<skill_name>/
      SKILL.md                  -- metadata (YAML frontmatter)
      main.py                   -- entry point
      requirements.txt          -- deps beyond base_venv (optional)
      lib/                      -- overlay deps (pip install --target)
  ```
- [ ] Implement `SkillLoader`:
  - Scan skill directories on startup
  - Parse SKILL.md frontmatter (name, description, params, deps)
  - Register each as a tool in the registry
  - **Lazy loading:** only names + descriptions in system prompt; full details via `read_skill` tool
- [ ] Implement `BaseVenvManager`:
  - Create shared base_venv once with common packages
  - Per-skill: if extra deps needed → `pip install --target=skill_dir/lib`
  - Hash `requirements.txt` → skip reinstall if unchanged
  - Result: ~0 MB + <1s for typical skills (vs 50-100 MB + 5-10s per-skill venv)
- [ ] Implement `skill_factory` tool:
  - LLM provides: name, description, code, dependencies
  - Kernel writes files, installs overlay deps (if any)
  - Test run the skill
  - If fails: return error to LLM for correction (max 3 retries)
  - If passes: register in tool registry
- [ ] Implement skill execution:
  - Run as subprocess with base_venv + PYTHONPATH overlay
  - stdin: JSON params, stdout: JSON result
  - Process tree kill on timeout
- [ ] Implement warm Python pool:
  - Keep frequently-used skill processes alive (idle timeout: 60s)
  - Subsequent calls skip interpreter boot → ~20-50ms vs ~300ms cold start
  - Pool size configurable (default: 3 warm processes)
- [ ] Implement circuit breaker:
  - Track failure count per skill
  - Auto-disable after N failures (default: 3)
  - Notify user of disabled skill
- [ ] Implement `find_skills` / `read_skill` tools

### Exit Criteria
- Ask Aeon "create a tool that checks if a website is up"
- Aeon writes the Python script, installs deps (fast — shared venv), tests it
- Ask Aeon "check if example.com is up" → it uses the new tool
- Second invocation of same skill is noticeably faster (warm pool)
- A broken skill auto-disables after 3 failures

---

## Phase 7: Scheduler (Cron)
**Goal:** Aeon can schedule recurring tasks.

### Tasks
- [ ] Implement SQLite cron store:
  - Table: `cron_jobs(id, name, schedule, skill_name, params, enabled, last_run, next_run, fail_count)`
- [ ] Implement cron ticker:
  - Go goroutine, checks every 60 seconds
  - Fires matching jobs as system-channel inbound messages
- [ ] Implement `cron_manage` tool:
  - Actions: create, list, pause, resume, delete
  - Schedule format: cron expression (e.g., `0 * * * *`) or simple intervals (`every 1h`)
- [ ] Concurrency control:
  - Max N concurrent cron jobs (configurable, default: 3)
  - If job exceeds interval → skip next execution (no stacking)
- [ ] Failure handling:
  - Track consecutive failures per job
  - Auto-pause after N failures (default: 5)
  - Notify user

### Exit Criteria
- "Check my site every hour" → cron job created, executes on schedule
- `cron_manage list` shows all scheduled jobs
- Stacking prevention works
- Survives kernel restart (state in SQLite)

---

## Phase 8: Telegram Channel
**Goal:** Full Telegram integration — text, voice, images, approval buttons.

### Tasks
- [ ] Implement Telegram channel using Bot API:
  - Long polling (no webhook for simplicity)
  - Text messages → inbound
  - Voice notes → download, send to Gemini for transcription, → inbound as text
  - Images → download, store locally, → inbound with media reference
- [ ] Outbound message handling:
  - Auto-chunk messages >4096 chars
  - Markdown formatting
  - Progress indicators for long tasks (edit message with "thinking...")
- [ ] Approval flow via inline keyboards:
  - Dangerous command → send message with "Allow / Deny" buttons
  - Button callback → resolve approval in security policy
  - Timeout → auto-deny
- [ ] Implement `analyze_multimodal` tool:
  - Accept image/audio file paths
  - Send to Gemini API
  - Return text description
- [ ] Implement `web_read` tool:
  - Jina Reader API (`https://r.jina.ai/<url>`)
  - Return markdown content
  - Size limit on response

### Exit Criteria
- Send text to Telegram bot → get Claude response
- Send voice note → transcribed and processed
- Send image → analyzed by Gemini
- Dangerous command shows approval buttons
- Long responses are properly chunked

---

## Phase 9: Subagents & Spawn
**Goal:** Aeon can delegate tasks to background subagents.

### Tasks
- [ ] Implement `SubagentManager`:
  - Spawn a new agent loop instance with its own context
  - Runs as goroutine
  - Communicates results back via system channel
- [ ] Implement `spawn_agent` tool:
  - LLM specifies task description
  - Kernel spawns subagent, returns "task started" immediately
  - Subagent works in background, posts result when done
- [ ] Track active subagents per session
- [ ] `/stop` command cancels all active subagents
- [ ] Resource limits: max concurrent subagents (default: 3)

### Exit Criteria
- Ask Aeon to do two things in parallel → spawns subagent for one
- Subagent result appears in conversation
- `/stop` cancels running subagents

---

## Phase 10: Hardening & Polish
**Goal:** Production-ready for daily use.

### Tasks
- [ ] Graceful shutdown (drain bus, finish active tool calls, save state)
- [ ] Startup recovery (reload cron jobs, skill registry, warm pool, pending approvals)
- [ ] Structured logging (JSON, configurable level)
- [ ] Health/status command: "what skills do you have, what's scheduled, what failed?"
- [ ] Config validation on startup
- [ ] Provider chain health monitoring:
  - CLI process uptime, restart count
  - API fallback activation count
  - Per-provider latency tracking (log p50/p95)
- [ ] Warm pool lifecycle management:
  - Evict idle processes after timeout
  - Pre-warm top-N most-used skills on startup
- [ ] Systemd service file for VPS deployment
- [ ] Installation script (dependencies, base_venv setup, workspace init)
- [ ] Basic integration tests for each phase

### Exit Criteria
- Survives 24h unattended on a VPS
- Recovers from crashes without data loss
- Logs are useful for debugging
- Provider chain seamlessly falls back on CLI crash

---

## Phase Dependency Graph

```
Phase 0 (Skeleton)
    │
Phase 1 (Bus + CLI)
    │
Phase 2 (Provider Chain) ← persistent CLI + optional API fallback
    │
Phase 3 (Tool System) ← parallel execution, result truncation
    ├──────────────┐
Phase 4 (Security) Phase 5 (Memory) ← SQLite tuning, compaction
    │              │
    └──────┬───────┘
           │
    Phase 6 (Skills) ← shared venv, warm pool, hash caching
           │
    Phase 7 (Scheduler)
           │
    Phase 8 (Telegram)
           │
    Phase 9 (Subagents)
           │
    Phase 10 (Hardening) ← pre-warm, health monitoring
```

Phases 4 and 5 can be developed in parallel after Phase 3.
Performance optimizations are baked into each phase — not a separate "optimization pass".

---

## Tech Stack Summary

| Component | Technology |
|---|---|
| Language | Go 1.22+ |
| LLM Providers | Claude CLI, Anthropic API, Gemini API, OpenAI-compatible (min 1 required) |
| Database | SQLite 3 (pure Go — modernc.org/sqlite, WAL mode) |
| Vector Search | sqlite-vec (Phase 5b) |
| Web Extraction | Jina Reader API |
| Chat Interface | Telegram Bot API + CLI |
| Skill Runtime | Python 3.10+ (shared base venv + overlay) + Bash |
| Config Format | YAML |
| Deployment | Systemd on Linux VPS, or run directly on desktop |

---

## Estimated Complexity

| Phase | Effort | Files (~) | Key Perf Feature |
|---|---|---|---|
| 0 - Skeleton | Low | 10-15 | — |
| 1 - Bus + CLI | Low | 5-8 | — |
| 2 - Provider Chain | Medium-High | 6-10 | Persistent CLI, API fallback, routing |
| 3 - Tool System | Medium-High | 10-15 | Parallel tool execution, result truncation |
| 4 - Security | Medium | 3-5 | Credential scrubbing |
| 5 - Memory | Medium | 5-8 | SQLite WAL + tuning, aggressive compaction |
| 6 - Skills | High | 10-15 | Shared base venv, warm pool, hash caching |
| 7 - Scheduler | Medium | 3-5 | Concurrency limits |
| 8 - Telegram | Medium-High | 5-8 | — |
| 9 - Subagents | Medium | 3-5 | — |
| 10 - Hardening | Medium | 5-10 | Pre-warm skills, provider health monitoring |
