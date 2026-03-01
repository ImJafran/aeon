<h1 align="center">Aeon</h1>

<p align="center">
  <strong>A self-evolving, system-level AI agent that lives inside your kernel.</strong><br>
  Not a chatbot. Not an informer. A virtual sysadmin that acts on your behalf.
</p>

<p align="center">
  <a href="https://github.com/ImJafran/aeon/releases"><img src="https://img.shields.io/badge/version-0.0.2--beta-blue?style=flat-square" alt="Version" /></a>
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

> **Beta:** v0.0.2-beta. APIs and config format may change.

## What is Aeon?

Aeon is a **system-level AI agent** with deep Linux/Unix integration. It has root access guarded by a sandboxed security policy. It's self-evolving, self-learning, and self-correcting — a general-purpose agent that **does things**, not a chatbot that talks about them.

It starts with core tools (shell, files, memory, cron) and **grows by writing its own Python skills**. Single Go binary, no CGO, no runtime dependencies. Supports **8 channels**: CLI, Telegram, Webhook (HTTP API), WebSocket, Discord, Slack, Email (IMAP/SMTP), and WhatsApp (Cloud API).

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
aeon serve        # daemon (all enabled channels)
```

That's it. `aeon init` detects your system, installs missing dependencies, sets up the workspace, and generates a config file.

```
$ aeon init

🌱 Aeon v0.0.2-beta — First-Time Setup
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

> **Note:** After editing the config, restart Aeon to apply changes:
> ```bash
> # If running as a systemd service
> sudo systemctl restart aeon
>
> # If running manually, just Ctrl+C and re-run
> aeon serve
> ```

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
    },
    "webhook": { "enabled": false, "listen_addr": ":8080" },
    "websocket": { "enabled": false, "listen_addr": ":8081" },
    "discord": { "enabled": false, "bot_token": "..." },
    "slack": { "enabled": false, "bot_token": "xoxb-...", "app_token": "xapp-..." },
    "email": { "enabled": false, "imap_server": "imap.gmail.com:993", "smtp_server": "smtp.gmail.com:587" },
    "whatsapp": { "enabled": false, "phone_number_id": "...", "access_token": "..." }
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

### Supported Channels

| Channel | Transport | Public URL? | Key Config |
|---|---|---|---|
| **CLI** | stdin/stdout | No | Always active in `aeon` mode |
| **Telegram** | Bot API long-poll | No | `bot_token`, `allowed_users` |
| **Webhook** | HTTP `POST /message` | Yes | `listen_addr`, `auth_token` |
| **WebSocket** | Persistent WS at `/ws` | Yes | `listen_addr`, `auth_token` |
| **Discord** | Gateway WebSocket | No | `bot_token`, `mention_only` |
| **Slack** | Socket Mode | No | `bot_token`, `app_token` |
| **Email** | IMAP poll + SMTP reply | No | `imap_server`, `smtp_server`, `username`, `password` |
| **WhatsApp** | Meta Cloud API webhook | Yes | `phone_number_id`, `access_token`, `verify_token` |

Enable any channel by setting `"enabled": true` in `config.json`. See [`config.example.json`](config.example.json) for all options.

### Telegram

1. Message [@BotFather](https://t.me/BotFather) -> `/newbot`
2. Copy the token into `config.json` -> `channels.telegram.bot_token`
3. Get your user ID from [@userinfobot](https://t.me/userinfobot)
4. Add it to `channels.telegram.allowed_users`
5. Restart Aeon

### Webhook (HTTP API)

Exposes a synchronous HTTP endpoint. Send a message, get the agent's response inline.

```bash
# Enable in config.json
"webhook": { "enabled": true, "listen_addr": ":8080", "auth_token": "my-secret" }

# Send a message
curl -X POST http://localhost:8080/message \
  -H "Authorization: Bearer my-secret" \
  -H "Content-Type: application/json" \
  -d '{"chat_id": "user1", "content": "hello"}'

# Health check
curl http://localhost:8080/health
```

### WebSocket

Persistent bidirectional connection for real-time integrations and web UIs.

```bash
# Enable in config.json
"websocket": { "enabled": true, "listen_addr": ":8081", "auth_token": "my-secret" }

# Connect (using websocat or any WS client)
websocat ws://localhost:8081/ws?chat_id=user1&token=my-secret

# Send: {"content": "hello"}
# Receive: {"content": "response from agent"}
```

### Discord

1. Create an app at [discord.com/developers](https://discord.com/developers/applications)
2. Create a Bot, copy the token
3. Enable **Message Content Intent** under Bot settings
4. Invite the bot to your server with `applications.commands` and `bot` scopes
5. Add to config:

```json
"discord": { "enabled": true, "bot_token": "your-token", "mention_only": true }
```

Set `mention_only` to `true` to only respond when @mentioned (recommended for servers). DMs always work.

### Slack

Uses Socket Mode — no public URL needed. Requires both a Bot Token and an App Token.

1. Create a Slack app at [api.slack.com/apps](https://api.slack.com/apps)
2. Enable **Socket Mode** and generate an App-Level Token (`xapp-...`) with `connections:write` scope
3. Under **OAuth & Permissions**, add bot scopes: `chat:write`, `app_mentions:read`, `im:history`, `channels:history`
4. Install the app to your workspace, copy the Bot Token (`xoxb-...`)
5. Under **Event Subscriptions**, subscribe to: `message.im`, `app_mention`
6. Add to config:

```json
"slack": { "enabled": true, "bot_token": "xoxb-...", "app_token": "xapp-..." }
```

### Email (IMAP/SMTP)

Polls an inbox for new emails and replies via SMTP. Works with Gmail (app passwords), Outlook, or any IMAP/SMTP provider.

```json
"email": {
  "enabled": true,
  "imap_server": "imap.gmail.com:993",
  "smtp_server": "smtp.gmail.com:587",
  "username": "you@gmail.com",
  "password": "your-app-password",
  "poll_interval": "60s",
  "allowed_from": ["trusted@example.com"]
}
```

For Gmail: enable 2FA, then generate an [App Password](https://myaccount.google.com/apppasswords). Leave `allowed_from` empty to accept from anyone.

### WhatsApp (Cloud API)

Uses Meta's official WhatsApp Cloud API. Free tier: 1,000 conversations/month.

1. Create a Meta app at [developers.facebook.com](https://developers.facebook.com)
2. Add WhatsApp product, get a Phone Number ID and Access Token
3. Set up a webhook URL pointing to your Aeon instance (requires public URL or ngrok)
4. Add to config:

```json
"whatsapp": {
  "enabled": true,
  "phone_number_id": "your-phone-id",
  "access_token": "your-access-token",
  "verify_token": "aeon-verify",
  "listen_addr": ":8443"
}
```

In Meta's webhook settings, set the callback URL to `https://your-domain:8443/webhook` and the verify token to `aeon-verify`.

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
