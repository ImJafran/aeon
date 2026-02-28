<h1 align="center">Aeon</h1>

<p align="center">
  <strong>A self-evolving, system-level AI agent that lives inside your kernel.</strong><br>
  Not a chatbot. Not an informer. A virtual sysadmin that acts on your behalf.
</p>

<p align="center">
  <a href="https://github.com/ImJafran/aeon/releases"><img src="https://img.shields.io/badge/version-0.0.1--beta-blue?style=flat-square" alt="Version" /></a>
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.24+" /></a>
  <a href="https://www.sqlite.org"><img src="https://img.shields.io/badge/SQLite-FTS5-003B57?style=flat-square&logo=sqlite&logoColor=white" alt="SQLite FTS5" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="MIT License" /></a>
</p>

<p align="center">
  <a href="#install">Install</a> &middot;
  <a href="#get-started">Get Started</a> &middot;
  <a href="#configuration">Configuration</a> &middot;
  <a href="#commands">Commands</a> &middot;
  <a href="#deployment">Deployment</a> &middot;
  <a href="#uninstall">Uninstall</a>
</p>

<p align="center">
  <a href="ENGINEERING.md">Engineering Deep Dive</a> &middot;
  <a href="DEVELOPMENT.md">Development</a>
</p>

---

> **Beta:** v0.0.1-beta. APIs and config format may change.

## What is Aeon?

Aeon is a **system-level AI agent** with deep Linux/Unix integration. It has root access guarded by a sandboxed security policy. It's self-evolving, self-learning, and self-correcting — a general-purpose agent that **does things**, not a chatbot that talks about them.

It starts with core tools (shell, files, memory, cron) and **grows by writing its own Python skills**. Single Go binary, no CGO, no runtime dependencies. Runs as a Telegram bot or local CLI.

---

## Install

```bash
curl -sSL https://raw.githubusercontent.com/ImJafran/aeon/main/deploy/install.sh | bash
```

The script installs Go (if needed), builds Aeon, and runs `aeon init` to set up everything.

<details>
<summary>Other install methods</summary>

### Using `go install`

Requires [Go 1.24+](https://go.dev/dl/).

```bash
go install github.com/ImJafran/aeon/cmd/aeon@latest
```

### From source

```bash
git clone https://github.com/ImJafran/aeon.git
cd aeon
make install    # builds + installs to ~/.local/bin/aeon
```

</details>

---

## Get Started

```bash
# 1. First-time setup — installs Python, ffmpeg, creates config
aeon init

# 2. Add your API keys
nano ~/.aeon/config.json

# 3. Run
aeon              # interactive CLI
aeon serve        # Telegram daemon
```

That's it. `aeon init` detects your system, installs missing dependencies, sets up the workspace, and generates a config file.

```
$ aeon init

🌱 Aeon v0.0.1-beta — First-Time Setup
========================================

[1/4] Checking system...
  ✓ OS: linux (amd64)
  ✓ SQLite: compiled into binary
  ✗ Python: not found
  ✗ ffmpeg: not found

[2/4] Installing dependencies...
  ✓ Python installed: Python 3.12.3
  ✓ ffmpeg installed

[3/4] Detecting LLM providers...
  ✓ ANTHROPIC_API_KEY set
  ✓ GEMINI_API_KEY set

[4/4] Setting up workspace...
  ✓ Workspace created at ~/.aeon
  ✓ Config written to ~/.aeon/config.json
  ✓ Base Python environment ready

✓ Setup complete!
```

---

## Configuration

All config lives in `~/.aeon/config.json`. See [`config.example.json`](config.example.json) for the full template.

```json
{
  "provider": {
    "zai": {
      "enabled": true,
      "api_key": "your-zai-api-key",
      "default_model": "glm-4.7"
    },
    "anthropic": {
      "enabled": true,
      "api_key": "sk-ant-your-key",
      "default_model": "claude-sonnet-4-6"
    },
    "gemini": {
      "enabled": true,
      "api_key": "your-gemini-key",
      "default_model": "gemini-2.5-flash-lite"
    }
  },
  "channels": {
    "telegram": {
      "enabled": true,
      "bot_token": "your-telegram-bot-token",
      "allowed_users": [0]
    }
  },
  "routing": {
    "primary": "zai",
    "fast": "anthropic_fast",
    "fallback": "gemini"
  }
}
```

### Supported Providers

| Provider | Notes |
|---|---|
| **Z.ai** | GLM models via OpenAI-compatible API (default) |
| **Anthropic** | Claude models via native Messages API |
| **Gemini** | Also handles voice transcription and TTS |
| **Ollama** | Fully offline, local models |
| **Any OpenAI-compatible** | LM Studio, vLLM, OpenRouter, etc. |

### Telegram Bot Setup

1. Message [@BotFather](https://t.me/BotFather) -> `/newbot`
2. Copy the token into `config.json` -> `channels.telegram.bot_token`
3. Get your user ID from [@userinfobot](https://t.me/userinfobot)
4. Add it to `channels.telegram.allowed_users`
5. Restart Aeon

---

## Commands

| Command | Description |
|---|---|
| `/status` | System info — provider, tools, memory, active tasks |
| `/model` | Switch LLM provider at runtime |
| `/model gemini` | Switch to a specific provider |
| `/new` | Clear conversation history (memory persists) |
| `/stop` | Cancel running tasks |
| `/skills` | List evolved skills |
| `/help` | List available commands |

---

## Deployment

### As a systemd Service

```bash
sudo cp deploy/aeon.service /etc/systemd/system/
sudo systemctl enable --now aeon
```

---

## Uninstall

Removes the binary, `~/.aeon/` (config, database, skills, logs), and systemd service:

```bash
aeon uninstall
```

If you cloned the source, remove it separately: `rm -rf /path/to/aeon`

---

## More

- **[Engineering Deep Dive](ENGINEERING.md)** — architecture, comparison with other agents, tools, skills, security model, performance, troubleshooting
- **[Development](DEVELOPMENT.md)** — how to contribute, build & test, branch naming, PR workflow

---

## Author

Created by **[Jafran Hasan](https://linkedin.com/in/iamjafran)** ([@imjafran](https://github.com/ImJafran))

## License

[MIT](LICENSE)
