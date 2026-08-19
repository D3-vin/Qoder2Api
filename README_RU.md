# ⚡ QODER2API

OpenAI и Anthropic-совместимый шлюз для аккаунтов Qoder IDE — один Go-бинарник с пулом аккаунтов.

[![Version](https://img.shields.io/badge/version-1.2.0-blue)](https://github.com/D3-vin/Qoder2Api/releases)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[![Telegram](https://img.shields.io/badge/Telegram-@D3_vin-blue?logo=telegram)](https://t.me/D3_vin)
[![Author](https://img.shields.io/badge/Author-@D3vin_dev-blue?logo=telegram)](https://t.me/D3_vin)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Qoder2Api)

[Возможности](#возможности) • [Быстрый старт](#быстрый-старт) • [Использование](#использование) • [Конфигурация](#конфигурация) • [Устранение неполадок](#устранение-неполадок) • [Контакты](#контакты)

[English](README.md) | [Русский](#)

---

## Возможности

- 🚀 **Один бинарник** — HTTP-шлюз, пул аккаунтов и встроенный дашборд в одном Go-бинарнике; без внешних сервисов
- 🔓 **OpenAI-совместимость** — `/v1/chat/completions`, `/v1/models`, `/v1/usage`; работает с любым клиентом формата OpenAI
- 🎭 **Anthropic-совместимость** — `/v1/messages` для Claude Code и других клиентов формата Anthropic
- 🔄 **Пул аккаунтов с failover** — несколько PAT, автоматическое переключение на следующий аккаунт при лимитах агента (код 115)
- 📊 **Встроенный дашборд** — live HTML UI на корневом URL: аккаунты, квоты, модель по умолчанию, настройки context/thinking — изменения сохраняются в `config.json`
- 🎯 **Имена моделей напрямую** — клиенты указывают реальные имена моделей Qoder (например, `qwen3.8-max`)
- 🌐 **Кросс-платформенность** — Windows, macOS, Linux
- 📦 **Автономность** — чистый Go, минимум зависимостей

> 💡 **Работает с Claude Code из коробки!** Просто укажите его переменным окружения адрес шлюза и выберите модель Qoder.

---

## Быстрый старт

### 1. Скачать

Скачайте последний релиз для вашей платформы:
- [Windows (64-bit)](https://github.com/D3-vin/Qoder2Api/releases)
- [Linux (64-bit)](https://github.com/D3-vin/Qoder2Api/releases)
- [macOS Intel](https://github.com/D3-vin/Qoder2Api/releases)
- [macOS Apple Silicon](https://github.com/D3-vin/Qoder2Api/releases)

Или соберите из исходников:

```bash
git clone https://github.com/D3-vin/Qoder2Api.git
cd Qoder2Api
go build -trimpath -ldflags="-s -w" -o qoder2api .
```

### 2. Конфигурация

```bash
cp .env.example .env
# Отредактируйте .env — задайте QODER_PAT из настроек аккаунта Qoder (см. Конфигурацию)
```

### 3. Запуск

```bash
./qoder2api
```

Дашборд: http://127.0.0.1:8963/

### 4. Тест

```bash
curl -X POST http://127.0.0.1:8963/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3.8-max","messages":[{"role":"user","content":"Привет!"}],"stream":false}'
```

---

## Использование

### Стриминг

```bash
curl -N -X POST http://127.0.0.1:8963/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen3.8-max","stream":true,"messages":[{"role":"user","content":"Скажи hi"}]}'
```

### Подключение Claude Code

Клиенты указывают имена моделей напрямую — направьте переменные окружения Claude Code на модели Qoder:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8963
export ANTHROPIC_AUTH_TOKEN=***
export ANTHROPIC_DEFAULT_HAIKU_MODEL=qwen3.8-max
export ANTHROPIC_DEFAULT_SONNET_MODEL=qwen3.8-max
export ANTHROPIC_DEFAULT_OPUS_MODEL=qwen3.8-max
export CLAUDE_CODE_SUBAGENT_MODEL=qwen3.8-max
```

### Подключение любого OpenAI-клиента

| Настройка | Значение |
|---|---|
| Base URL | `http://127.0.0.1:8963/v1` |
| API Key | любое непустое значение |
| Модель | любое имя из `GET /v1/models` (например, `qwen3.8-max`) |

### Модели

Живой список моделей отдаётся из Qoder на `GET /v1/models`. Встроенные алиасы:

| Модель | Qoder key |
|---|---|
| `qwen3.8-max` | `qmodel_38max` |
| `qwen3.7-max` | `qmodel_latest` |
| `qwen3.7-plus` | `qmodel` |
| `deepseek-v4-pro` | `dmodel` |
| `deepseek-v4-flash` | `dfmodel` |
| `lite` | `lite` |
| `auto` | `auto` |

### API-эндпоинты

| Метод | Путь | Описание |
|---|---|---|
| `POST` | `/v1/chat/completions` | OpenAI-чат (stream / non-stream) |
| `POST` | `/v1/messages` | Anthropic messages |
| `GET` | `/v1/models` | Живой список моделей из Qoder |
| `GET` | `/v1/usage` | Квота активного аккаунта |
| `GET` | `/` | Дашборд |
| `GET` | `/api/status` | Статус сервера, активный аккаунт, модель по умолчанию |
| `GET` | `/api/accounts` | Список аккаунтов с квотами |
| `POST` | `/api/accounts/add` | Добавить PAT во время работы |
| `POST` | `/api/accounts/remove` | Удалить аккаунт |
| `POST` | `/api/accounts/select` | Переключить активный аккаунт |
| `POST` | `/api/quota/refresh` | Обновить квоты |
| `GET` | `/api/promo` | Промо-активности и бесплатные квоты |
| `POST` | `/api/settings` | Модель по умолчанию, context/thinking для моделей |

---

## Структура проекта

```
├── main.go               # Точка входа, загрузка .env, запуск пула
├── server/               # HTTP-сервер, пул аккаунтов, дашборд, мосты протоколов
├── api/                  # Клиенты upstream API (bearer, signature, OpenAPI)
├── auth/                 # Построение токенов и подписей
├── encoding/             # Wire-кодирование Qoder
├── httputil/             # HTTP-клиент и хелперы
├── baseprompt_min.json   # Минимальный профиль промпта (~2 KB)
├── baseprompt.json       # Полный профиль промпта (дамп CLI)
├── .env.example          # Шаблон конфигурации
├── config.json           # Runtime-состояние (управляется дашбордом)
└── README.md
```

---

## Конфигурация

Настройки в файле `.env` (загружается автоматически) или переменных окружения:

| Переменная | По умолчанию | Описание |
|---|---|---|
| `QODER_PAT` | *(обязательно)* | Qoder Personal Access Token |
| `QODER_PAT_LIST` | *(пусто)* | Несколько PAT (через запятую/перевод строки); приоритет над `QODER_PAT`, пул переключается при лимитах агента |
| `QODER_HOST` | `127.0.0.1` | Адрес привязки |
| `QODER_PORT` | `8963` | Порт сервера |
| `QODER_MODEL` | `lite` | Модель по умолчанию |
| `QODER_PROMPT_PROFILE` | `min` | Размер промпта: `min` (~2 KB) или `full` (дамп CLI) |
| `QODER_PROXY_ENABLED` | `false` | Включить исходящий прокси |
| `QODER_PROXY_URL` | *(пусто)* | URL исходящего прокси (например, `http://127.0.0.1:8888`) |
| `QODER_INSECURE_SKIP_VERIFY` | `false` | Пропускать TLS-верификацию для прокси |
| `QODER_MACHINE_ID` / `QODER_MACHINE_TOKEN` / `QODER_MACHINE_TYPE` | *(пусто)* | Machine-данные из вашей авторизованной сессии Qoder IDE |

Изменения дашборда (PAT, активный аккаунт, модель по умолчанию, context/thinking) сохраняются в `config.json` рядом с бинарником.

---

## Устранение неполадок

| Симптом | Причина | Решение |
|---|---|---|
| `no PAT configured` при старте | Нет токена | Задайте `QODER_PAT` или `QODER_PAT_LIST` в `.env` |
| Ошибка лимита агента (код 115) | Квота аккаунта исчерпана | Добавьте PAT — пул переключится автоматически |
| Запросы падают при старте | Неверные machine-данные | Задайте `QODER_MACHINE_ID` / `QODER_MACHINE_TOKEN` / `QODER_MACHINE_TYPE` из вашей IDE-сессии |
| Нет полного промпта | Не найден `baseprompt.json` | Положите `baseprompt.json` рядом с бинарником для `QODER_PROMPT_PROFILE=full` |

---

## Контакты

- **GitHub**: https://github.com/D3-vin/Qoder2Api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **Чат**: [@D3vin_chat](https://t.me/D3vin_chat)
- **Автор**: [@D3vin_dev](https://t.me/D3_vin)

---

## ⚠️ Отказ от ответственности

Программное обеспечение предоставляется **исключительно в образовательных целях** — для демонстрации технических возможностей API-мостов. Предоставляется **«как есть», без каких-либо гарантий**. Автор **не несёт ответственности** за любые последствия использования. Пользователи обязаны соблюдать применимое законодательство и условия использования всех задействованных сторонних сервисов.

## Лицензия

MIT License — см. [LICENSE](LICENSE)
