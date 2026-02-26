# MVP Plan: Aeon (The Self-Evolving Agentic Kernel)

## 1. The Core Concept
**Aeon** is a minimalist, high-performance Go-based "Agentic Kernel" designed for permanent residency on a Linux VPS or Server. It is the "Seed" of an autonomous intelligence that evolves its own capabilities by writing, testing, and managing its own tools.

### Key Philosophy (Validated MVP)
- **Lean Evolution:** Focuses on the "Write-Test-Correct" loop using Python (in VirtualEnvs) and Bash.
- **Subscription-First:** Primarily interfaces with **Claude Code** (via PTY) to leverage your Anthropic subscription, with **Gemini API** as a multimodal fallback (Audio/Image).
- **DNA vs. Evolved:** 
    - **DNA (Fixed):** Hardcoded Go capabilities for system access and safety.
    - **Evolved (Mutable):** AI-generated scripts registered as MCP tools.
- **Human-in-the-Loop Safety:** A "Confirm" gate for dangerous system commands (e.g., `rm`, `shutdown`).

## 2. System Architecture
### A. The Aeon Kernel (Go)
- **PTY Orchestrator:** Manages the interactive `claude` CLI session.
- **Dynamic MCP Host:** Exposes tools to the LLM; auto-reloads when new skills are "born."
- **VirtualEnv Manager:** Automatically creates and isolates Python environments for evolved skills.
- **Background Scheduler:** A lightweight Go routine that executes Cron-style tasks from SQLite.
- **Telegram Bridge:** The *exclusive* external channel for MVP (supports Text, Audio, Images).

### B. Tool Categories
#### 1. DNA (The "Nature" - Built-in)
- `rag_ops`: Query/Store memory in SQLite-vec.
- `vault_ops`: Securely store/retrieve encrypted API keys and credentials.
- `web_read`: Simple Markdown extraction via Jina Reader API (no headless browser).
- `analyze_multimodal`: Sends images/audio to Gemini API for description.
- `shell_exec`: Runs Bash commands with a "Dangerous Command" filter.
- `skill_factory`: Registers/Updates a script as an active MCP tool.
- `cron_manage`: Schedule, list, or delete recurring background tasks.

#### 2. Evolved (The "Nurture" - AI-Generated)
- Scripts stored in `~/.aeon/skills/`.
- **Constraint:** Must be Python (PEP 8) or Bash.
- **Example:** `wordpress_post_generator.py` or `server_log_monitor.sh`.

## 3. The "Evolutionary" Workflow (The MVP Loop)
1. **Request:** User sends a voice note to Telegram: "Check my site's uptime every hour and alert me here."
2. **Reasoning:** Aeon (Claude) realizes it needs an uptime checker and a scheduler.
3. **Creation:** Aeon writes `uptime_check.py` to the `skills/` folder.
4. **Validation:** Aeon runs the script in its managed VirtualEnv. If it fails (e.g., missing `requests` library), Aeon uses `pip install` within the Venv and tries again.
5. **Registration:** Aeon calls `skill_factory` to add `uptime_check` to its MCP manifest.
6. **Automation:** Aeon calls `cron_manage` to schedule the tool every hour.

## 4. Advanced Evolutionary Features (The Singularity Layer)
- **The Vault (Secrets Management):** Evolved scripts never contain plaintext secrets. Aeon uses the `vault_ops` DNA tool to request credentials at runtime, keeping the environment secure.
- **The Soul (Identity Persistence):** A core `IDENTITY.md` injected into every LLM session (Claude/Gemini/DeepSeek) to maintain a consistent persona, goals, and rules across model switches.
- **The Pulse (Active Observability):** A heartbeat mechanism on Telegram. Aeon proactively "taps the user on the shoulder" if a background task fails repeatedly or if it reaches a milestone in its evolution.
- **The Ancestry (Skill Versioning):** The Go Kernel maintains an archive of all previous versions of evolved skills. If a self-correction loop fails, Aeon can autonomously "roll back" to a stable version.

## 5. Security & Safety (The "Safety Valve")
- **Isolated Skills:** Every evolved Python tool runs in a dedicated VirtualEnv to prevent dependency hell.
- **Command Interception:** The Go Kernel parses all `shell_exec` calls. If a "High-Risk" pattern is detected, it pauses and waits for a "YES" from the user via Telegram.
- **State Persistence:** All active cron jobs and skill metadata are stored in SQLite, allowing Aeon to resume after a VPS reboot.

## 6. MVP Scope (What we REMOVED)
- **No Headless Browser:** Replaced by Jina Reader API for simplicity.
- **No Multi-Channel:** Focused exclusively on Telegram and Terminal.
- **No Local Audio/Vision Models:** Offloaded to Gemini API to save VPS resources.
- **No Go-Code Evolution:** Limited to Python and Bash for faster iteration.

---
**Aeon:** The smallest seed of AGI, planted on your server.
