# Aeon Architecture (v2)

## 1. Core Concept

**Aeon** is a minimalist, high-performance Go-based "Agentic Kernel" designed for permanent residency on a **Linux VPS or Desktop**. It is the seed of an autonomous intelligence that evolves its own capabilities by writing, testing, and managing its own tools.

> Best suited for a dedicated VPS. Desktop works but carries more risk surface.

### Key Philosophy

- **Zero Dependencies:** Single static binary. SQLite compiled in (pure Go). Python auto-detected and auto-bootstrapped for skill runtime — if unavailable, Aeon still works (skills disabled). No Docker, no Postgres, no Node.js.
- **Lean Evolution:** Write-Test-Correct loop. AI generates Python/Bash scripts, validates them, registers as tools.
- **Provider Agnostic:** Works with **any single LLM provider** — Claude CLI (free via subscription), Anthropic API, Gemini API, or any OpenAI-compatible endpoint (Ollama for fully offline). Multiple providers enable smart routing and fallback.
- **Graceful Degradation:** Missing dependencies disable features, not the system. No Python → no skills but LLM + DNA tools work. No Telegram token → CLI only. No Gemini key → no multimodal but text works.
- **DNA vs. Evolved:**
  - **DNA (Fixed):** Hardcoded Go capabilities — system access, safety, orchestration.
  - **Evolved (Mutable):** AI-generated scripts registered as callable tools.
- **Human-in-the-Loop Safety:** Dangerous commands require explicit user approval via Telegram.

---

## 2. System Architecture

### A. The Aeon Kernel (Go)

```
┌──────────────────────────────────────────────────────┐
│                    AEON KERNEL (Go)                   │
├──────────────────────────────────────────────────────┤
│                                                      │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │  Message Bus │──│  Agent Loop  │──│  Provider  │  │
│  │  (In/Out)    │  │  (Core)      │  │  (Claude)  │  │
│  └──────┬───────┘  └──────┬───────┘  └────────────┘  │
│         │                 │                           │
│  ┌──────┴───────┐  ┌──────┴───────┐  ┌────────────┐  │
│  │  Channels    │  │ Tool Registry│──│  Security  │  │
│  │  - Telegram  │  │  - DNA tools │  │  Policy    │  │
│  │  - CLI       │  │  - Evolved   │  └────────────┘  │
│  │  - System    │  │  - MCP       │                   │
│  └──────────────┘  └──────┬───────┘  ┌────────────┐  │
│                           │          │  Scheduler  │  │
│  ┌──────────────┐  ┌──────┴───────┐  │  (Cron)    │  │
│  │  Session Mgr │  │  Skill Mgr   │  └────────────┘  │
│  │  (per user)  │  │  - VEnv      │                   │
│  └──────────────┘  │  - Loader    │  ┌────────────┐  │
│                    │  - Factory   │  │  Memory     │  │
│                    └──────────────┘  │  (SQLite)   │  │
│                                      └────────────┘  │
└──────────────────────────────────────────────────────┘
```

### B. Key Components

#### Message Bus
- All communication flows as `InboundMessage -> AgentLoop -> OutboundMessage`.
- Channels publish inbound; agent loop publishes outbound.
- Routing by `channel + chatID` pair.
- System channel for internal events (cron triggers, subagent callbacks).

#### Agent Loop
Pattern borrowed from PicoClaw/nanobot:
1. Build context (system prompt + memory recall + **lazy** skills summary + conversation history)
2. Build tool definitions (sorted by name for KV cache stability)
3. Call LLM provider (via provider chain routing)
4. If tool calls → execute tools **in parallel where independent** → append results → loop back to step 3
5. If text response → send to user, break

**Context Optimizations:**
- **Lazy skill loading:** System prompt includes only skill names + one-line descriptions. LLM calls `read_skill` for full details on demand. Saves ~2500 tokens per call.
- **Tool result truncation:** Cap tool output at 2000 chars for LLM context. Full output stored separately, accessible via `get_full_output` tool.
- **Aggressive compaction:** Summarize after 10 messages (not 20). Shorter context = faster LLM inference.

#### Provider Chain

Aeon requires **at least one** LLM provider. More providers enable intelligent routing and fallback, but a single provider is fully functional.

**Supported Providers:**

| Provider | Interface | Cost | Strengths |
|---|---|---|---|
| Claude CLI | Persistent subprocess (stdin/stdout) | Free (subscription) | Zero marginal cost, full Claude |
| Anthropic API | Direct HTTP | Pay-per-token | Stable, fast, model choice (Haiku/Sonnet/Opus) |
| Gemini API | Direct HTTP | Free tier available | Multimodal, budget-friendly |
| OpenAI-compatible | Direct HTTP | Varies | Local models (Ollama), OpenRouter, fully offline |

**Single Provider Mode:**
- If only one provider is configured, **all** requests route through it.
- No routing logic needed — everything just works.
- Valid minimal setups:
  - Just Claude CLI (free, needs subscription)
  - Just Anthropic API key (paid, no subscription)
  - Just Gemini API key (free tier)
  - Just Ollama (fully offline, no API keys)

**Multi-Provider Mode (Routing):**
```
┌─────────────────────────────────────────────────┐
│               Provider Chain                     │
├─────────────────────────────────────────────────┤
│  primary:    Claude CLI / Anthropic / Gemini     │
│  fast:       Haiku / Gemini Flash (cheap ops)    │
│  multimodal: Gemini (audio/image)                │
│  fallback:   any other configured provider       │
└─────────────────────────────────────────────────┘
```
- Routing roles (`primary`, `fast`, `multimodal`, `fallback`) are user-assignable.
- If a role has no assigned provider, it falls back to `primary`.
- Automatic failover: if active provider fails, try `fallback` before erroring.

**Claude CLI Specifics (when used):**
- **No PTY.** Spawns `claude -p --output-format json --dangerously-skip-permissions --no-chrome -` **once** and keeps it alive as a persistent subprocess.
- Messages sent via **stdin**, responses parsed from **stdout**.
- Process recycled only on crash or configurable idle timeout.
- Eliminates ~1000ms spawn overhead → ~50ms pipe I/O.
- Auto-restart on crash (backoff: 1s, 2s, 4s, max 30s).

**OpenAI-Compatible Specifics:**
- Works with any provider that speaks the OpenAI chat completions API.
- Ollama, LM Studio, vLLM, OpenRouter, Together AI, etc.
- Enables fully local/offline operation (no internet, no API keys).

#### Tool Registry
- Dual-surface results: `ToolResult{ForLLM, ForUser, Silent}` — separates what the model sees from what the user sees.
- Three tool sources:
  - **DNA tools** — compiled into the binary
  - **Evolved tools** — Python/Bash scripts loaded from disk
  - **MCP tools** — external MCP servers (stdio + HTTP transport)
- Contextual tool interface: channel/chatID injected per-call.

#### Security Policy
- **Deny-pattern list** for shell commands (rm -rf, sudo, eval, pipe-to-bash, etc.)
- **Workspace path containment** — evolved skills restricted to their sandbox
- **Credential scrubbing** — regex-based scrub of API keys/tokens from tool output before it reaches the LLM
- **Process tree termination** on timeout
- **Approval flow** — dangerous operations routed through Telegram confirmation gate

#### Skill Manager
- Skills stored as files in `~/.aeon/skills/` (Python/Bash).
- Skill metadata in `SKILL.md` (YAML frontmatter): name, description, parameters, dependencies, schedule.
- Priority: workspace skills > global skills > builtin skills.
- Skills summary injected into system prompt as compact list (lazy loading — full details on demand).
- `skill_factory` — LLM-callable tool to create new skills.
- `find_skills` / `install_skill` — discovery and installation.
- **Rollback mechanism:** If a skill fails N times (default: 3), auto-disable and notify user.

**VirtualEnv Strategy (Shared Base + Overlay):**
```
~/.aeon/
  base_venv/              # shared: requests, httpx, bs4, pyyaml, etc.
  skills/
    uptime_check/         # uses base_venv (no extra deps needed)
    data_scraper/         # base_venv + pandas (overlay via PYTHONPATH)
      lib/                # pip install --target for unique deps
```
- **Base venv** contains common packages — created once, reused by all skills.
- Skills with no extra deps → execute against base_venv directly (zero setup time).
- Skills with unique deps → `pip install --target=skill_dir/lib`, prepend to `PYTHONPATH`.
- Result: ~0 MB disk + <1s creation for typical skills (vs ~50-100 MB + 5-10s per-skill venv).
- **Hash-based caching:** `requirements.txt` hash checked — skip reinstall if unchanged.

**Warm Python Pool:**
- Frequently-used skills keep their Python process alive (idle timeout: 60s).
- Subsequent calls skip interpreter boot → ~20-50ms vs ~300-500ms cold start.
- Pool size configurable (default: 3 warm processes).

#### Memory (SQLite — Pure Go, No CGo)
- Interface: `store / recall / get / list / forget / count`
- Categories: core, daily, conversation, custom
- Hybrid search: FTS5 keyword + vector cosine similarity
- Auto-save significant messages (>20 chars)
- Embedding generation: local model preferred (e.g., all-MiniLM via Go binding), Gemini API fallback

**SQLite Performance Tuning:**
```sql
PRAGMA journal_mode=WAL;       -- concurrent reads during writes
PRAGMA synchronous=NORMAL;     -- 2x faster writes (safe with WAL)
PRAGMA cache_size=-8000;       -- 8MB page cache
PRAGMA mmap_size=67108864;     -- 64MB memory-mapped I/O
PRAGMA temp_store=MEMORY;      -- temp tables in RAM
```

#### History Compaction
- Trigger: when conversation exceeds 75% of context window or >20 messages
- Strategy: LLM-summarize older messages, keep last N
- Emergency fallback: if LLM summarization fails, deterministic truncation (drop oldest 50%, append compression note)

#### Scheduler (Cron)
- SQLite-backed cron job store
- Go goroutine ticker checks schedule every minute
- Jobs fire as system-channel inbound messages → agent loop processes them
- Concurrency limit: max N concurrent cron jobs (default: 3)
- If job exceeds interval, skip next execution (no stacking)

#### Channels
- **Telegram** — primary external interface (text, audio, images, voice notes)
  - Handles rate limits and message chunking (4096 char limit)
  - Progress indicators for long-running tasks
  - Inline approval buttons for dangerous commands
- **CLI** — local terminal interface for development/debugging
- **System** — internal channel for cron triggers, subagent callbacks

#### Session Manager
- Per-user conversation state
- History persistence in SQLite
- `/new` command to flush and start fresh
- `/stop` command to cancel running tasks

---

## 3. DNA Tools (Built-in)

| Tool | Description |
|---|---|
| `shell_exec` | Run bash commands with deny-pattern filter + approval gate |
| `file_read` | Read file contents (with size limits) |
| `file_write` | Write/create files (workspace-scoped) |
| `file_edit` | Patch files with find/replace |
| `memory_store` | Store information in long-term memory |
| `memory_recall` | Query memory by keyword/vector search |
| `web_read` | Markdown extraction via Jina Reader API |
| `analyze_multimodal` | Send images/audio to Gemini API |
| `skill_factory` | Create and register a new Python/Bash skill |
| `find_skills` | Search available skills (local + registry) |
| `install_skill` | Install a skill from registry |
| `cron_manage` | Schedule, list, pause, or delete recurring jobs |
| `spawn_agent` | Spawn a subagent for async task delegation |
| `send_message` | Send a message to user via current channel |

---

## 4. Evolved Tools (AI-Generated)

- **Constraint:** Python (PEP 8) or Bash only.
- Stored in `~/.aeon/skills/<skill_name>/`
  - `SKILL.md` — metadata (YAML frontmatter)
  - `main.py` or `main.sh` — entry point
  - `lib/` — overlay deps (if any beyond base_venv)
- Executed as subprocess (or warm pool process) with timeout
- stdin: JSON params, stdout: JSON result
- stderr → logged, not sent to LLM unless debugging
- Exit code 0 = success, non-zero = failure (triggers retry/correction loop)

---

## 5. The Evolutionary Workflow

1. **Request:** User sends message (e.g., Telegram voice note: "Check my site's uptime every hour")
2. **Reasoning:** Agent loop determines it needs an uptime checker
3. **Creation:** Calls `skill_factory` → writes `uptime_check/main.py` + `SKILL.md`
4. **Validation:** Kernel executes script in managed venv. If it fails:
   - Missing dependency → auto `pip install` → retry
   - Code error → agent reads stderr, rewrites, retry (max 3 attempts)
5. **Registration:** Skill added to registry, available in next tool list
6. **Automation:** Agent calls `cron_manage` to schedule hourly execution
7. **Monitoring:** If skill fails 3 times in production, auto-disable + notify user

---

## 6. Security Model

### Layers
1. **Command Deny-List:** Regex patterns block dangerous shell commands
2. **Path Containment:** Evolved skills can only access their own workspace + designated shared dirs
3. **Credential Scrubbing:** API keys/tokens stripped from tool output before reaching LLM
4. **Approval Gate:** High-risk operations require explicit Telegram confirmation
5. **Skill Isolation:** Each Python skill in its own venv
6. **Failure Circuit Breaker:** Auto-disable skills that fail repeatedly
7. **Process Limits:** Timeout + process tree kill for runaway scripts

### Dangerous Command Patterns (Deny-List)
```
rm -rf /, sudo, mkfs, dd if=, :(){ :|:& };:,
eval, curl|sh, wget|sh, chmod 777, shutdown, reboot,
> /dev/sda, /etc/passwd, /etc/shadow
```

### Future Considerations
- `nsjail` or `bubblewrap` for deeper sandboxing (post-MVP)
- Network policy per skill (restrict outbound connections)
- Resource limits (CPU/memory cgroup per skill)

---

## 7. Performance Architecture

### Predicted Baseline (After Optimizations)

| Metric | Value |
|---|---|
| Binary size | ~15 MB |
| RAM at idle | ~10 MB |
| RAM under load (5 skills, 2 cron) | ~40-60 MB |
| Boot time | <50ms |
| LLM call overhead (persistent CLI) | ~50ms |
| Simple query e2e | ~2-3s |
| 5-tool loop e2e | ~10-15s |
| Skill execution (warm) | ~20-50ms |
| Skill execution (cold) | ~200-300ms |
| Skill creation (shared venv) | ~1-3s |
| Memory recall (FTS5) | ~5ms |
| Minimum VPS | 1 vCPU, 1 GB RAM, 20 GB disk |

### Key Optimizations Summary
1. **Persistent CLI subprocess** — eliminates ~950ms/call spawn overhead
2. **Provider chain routing** — simple ops to Haiku (fast+cheap), complex to CLI (free)
3. **Shared base venv** — 90% less disk, 10x faster skill creation
4. **Warm Python pool** — 10x faster repeat skill execution
5. **Parallel tool execution** — concurrent independent tool calls
6. **SQLite WAL + tuning** — 2-5x faster memory queries
7. **Lazy skill loading** — 15-30% faster LLM responses via smaller context
8. **Hash-based dep caching** — skip pip install when requirements unchanged

---

## 8. What's NOT in MVP

- **No headless browser** — Jina Reader API for web content
- **No multi-channel beyond Telegram + CLI** — Discord/Slack/etc. are post-MVP
- **No local AI models** — multimodal offloaded to Gemini API
- **No Go code evolution** — only Python/Bash scripts
- **No multi-user** — single-user system for MVP
- **No web UI/dashboard** — Telegram + CLI only
