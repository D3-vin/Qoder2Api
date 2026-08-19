# ⚡ QODER2API

OpenAI & Anthropic-compatible gateway for Qoder IDE accounts — single Go binary with an account pool.

[![Version](https://img.shields.io/badge/version-1.2.0-blue)](https://github.com/D3-vin/Qoder2Api/releases)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[![Telegram](https://img.shields.io/badge/Telegram-@D3_vin-blue?logo=telegram)](https://t.me/D3_vin)
[![Author](https://img.shields.io/badge/Author-@D3vin_dev-blue?logo=telegram)](https://t.me/D3_vin)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Qoder2Api)

[Features](#features) • [Quick Start](#quick-start) • [Usage](#usage) • [Configuration](#configuration) • [Troubleshooting](#troubleshooting) • [Contact](#contact)

[English](#) | [Русский](README_RU.md)

---

## Features

- 🚀 **Single binary** — HTTP gateway, account pool and embedded dashboard in one Go binary; no external services
- 🔓 **OpenAI-compatible** — `/v1/chat/completions`, `/v1/models`, `/v1/usage`; drop-in for any OpenAI-format client
- 🎭 **Anthropic-compatible** — `/v1/messages` for Claude Code and other Anthropic-format clients
- 🔄 **Account pool with failover** — multiple PATs, automatic switch to the next account on agent-limit errors (code 115)
- 📊 **Embedded dashboard** — live HTML UI at the root URL: accounts, quotas, default model, context/thinking settings — changes persist to `config.json`
- 🎯 **Model names pass-through** — clients configure real Qoder model names directly (e.g. `qwen3.8-max`)
- 🌐 **Cross-platform** — Windows, macOS, Linux
- 📦 **Standalone** — pure Go, minimal dependencies

> 💡 **Works with Claude Code out of the box!** Just point its env vars at the gateway and pick a Qoder model name.

---

## Quick Start

### 1. Download

Download the latest release for your platform:
- [Windows (64-bit)](https://github.com/D3-vin/Qoder2Api/releases)
- [Linux (64-bit)](https://github.com/D3-vin/Qoder2Api/releases)
- [macOS Intel](https://github.com/D3-vin/Qoder2Api/releases)
- [macOS Apple Silicon](https://github.com/D3-vin/Qoder2Api/releases)

Or build from source:

```bash
git clone https://github.com/D3-vin/Qoder2Api.git
cd Qoder2Api
go build -trimpath -ldflags="-s -w" -o qoder2api .
```

### 2. Configure

```bash
cp .env.example .env
# Edit .env — set QODER_PAT from your Qoder account settings (see Configuration)
```

> If `.env.example` is missing (bare release binary), just run the binary once —
> it extracts `.env.example`, `baseprompt_min.json` and `baseprompt.json` automatically.

### 3. Run

```bash
./qoder2api
```

Dashboard: http://127.0.0.1:8963/

### 4. Test

```bash
curl -X POST http://127.0.0.1:8963/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3.8-max","messages":[{"role":"user","content":"Hello!"}],"stream":false}'
```

---

## Usage

### Streaming

```bash
curl -N -X POST http://127.0.0.1:8963/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3.8-max","stream":true,"messages":[{"role":"user","content":"Say hi"}]}'
```

### Connect Claude Code

Clients configure model names directly — point Claude Code env vars at Qoder model names:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8963
export ANTHROPIC_AUTH_TOKEN=***
export ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3.8-max
export ANTHROPIC_DEFAULT_SONNET_MODEL=qwen3.8-max
export ANTHROPIC_DEFAULT_OPUS_MODEL=qwen3.8-max
export CLAUDE_CODE_SUBAGENT_MODEL=qwen3.8-max
```

### Connect any OpenAI client

| Setting | Value |
|---|---|
| Base URL | `http://127.0.0.1:8963/v1` |
| API Key | any non-empty value |
| Model | any name from `GET /v1/models` (e.g. `qwen3.8-max`) |

### Models

Live model list is served from Qoder at `GET /v1/models`. Built-in aliases:

| Model | Qoder key |
|---|---|
| `qwen3.8-max` | `qmodel_38max` |
| `qwen3.7-max` | `qmodel_latest` |
| `qwen3.7-plus` | `qmodel` |
| `deepseek-v4-pro` | `dmodel` |
| `deepseek-v4-flash` | `dfmodel` |
| `lite` | `lite` |
| `auto` | `auto` |

### API endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/chat/completions` | OpenAI chat (stream / non-stream) |
| `POST` | `/v1/messages` | Anthropic messages |
| `GET` | `/v1/models` | Live model list from Qoder |
| `GET` | `/v1/usage` | Active account quota |
| `GET` | `/` | Dashboard |
| `GET` | `/api/status` | Server status, active account, default model |
| `GET` | `/api/accounts` | List accounts with quotas |
| `POST` | `/api/accounts/add` | Add a PAT at runtime |
| `POST` | `/api/accounts/remove` | Remove an account |
| `POST` | `/api/accounts/select` | Switch active account |
| `POST` | `/api/quota/refresh` | Refresh quotas |
| `GET` | `/api/promo` | Promo activities and free quotas |
| `POST` | `/api/settings` | Default model, per-model context/thinking |

---

## Project Structure

```
├── main.go               # Entry point, .env loader, pool startup
├── server/               # HTTP server, account pool, dashboard, protocol bridges
├── api/                  # Upstream API clients (bearer, signature, OpenAPI)
├── auth/                 # Token and signature building
├── encoding/             # Qoder wire encoding
├── httputil/             # HTTP client helpers
├── assets/               # Embedded templates + .env.example (auto-extracted on first run)
├── config.json           # Runtime state (managed by dashboard)
└── README.md
```

---

## Configuration

Settings in `.env` (auto-loaded) or environment variables:

| Variable | Default | Description |
|---|---|---|
| `QODER_PAT` | *(required)* | Qoder Personal Access Token |
| `QODER_PAT_LIST` | *(empty)* | Multiple PATs (comma/newline separated); takes precedence over `QODER_PAT`, pool fails over on agent-limit errors |
| `QODER_HOST` | `127.0.0.1` | Bind address |
| `QODER_PORT` | `8963` | Server port |
| `QODER_MODEL` | `lite` | Default model |
| `QODER_PROMPT_PROFILE` | `min` | Prompt size: `min` (~2 KB) or `full` (CLI dump) |
| `QODER_PROXY_ENABLED` | `false` | Enable outbound proxy |
| `QODER_PROXY_URL` | *(empty)* | Outbound proxy URL (e.g. `http://127.0.0.1:8888`) |
| `QODER_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification for the proxy |
| `QODER_MACHINE_ID` / `QODER_MACHINE_TOKEN` / `QODER_MACHINE_TYPE` | *(empty)* | Machine credentials from your authenticated Qoder IDE session |

Dashboard changes (PATs, active account, default model, per-model context/thinking) persist to `config.json` next to the binary.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `no PAT configured` at startup | Missing token | Set `QODER_PAT` or `QODER_PAT_LIST` in `.env` |
| Agent-limit error (code 115) | Account quota exhausted | Add more PATs — the pool fails over automatically |
| Request fails on startup | Bad machine credentials | Set `QODER_MACHINE_ID` / `QODER_MACHINE_TOKEN` / `QODER_MACHINE_TYPE` from your IDE session |
| Full prompt missing | `baseprompt.json` not found | Place `baseprompt.json` next to the binary for `QODER_PROMPT_PROFILE=full` |

---

## Contact

- **GitHub**: https://github.com/D3-vin/Qoder2Api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **Chat**: [@D3vin_chat](https://t.me/D3vin_chat)
- **Author**: [@D3vin_dev](https://t.me/D3_vin)

---

## ⚠️ Disclaimer

This software is provided **for educational purposes only**, to demonstrate technical possibilities of API bridging. It is provided **"as is" without any warranty**. The author bears **no responsibility** for any consequences of its use. Users must comply with applicable laws and the terms of service of all third-party services involved.

## License

MIT License — see [LICENSE](LICENSE)
