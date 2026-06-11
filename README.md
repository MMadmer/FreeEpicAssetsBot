# Epic Assets Notify Bot

<img width="1024" height="1024" alt="epic_assets_avatar" src="https://github.com/user-attachments/assets/3ba5ab1d-9fba-41a0-bae3-2a557eeefb91" />

Discord bot that tracks current `Limited-Time Free` assets on Fab and posts updates to subscribed Discord channels or direct messages.

## Features

- Daily Fab checks with change detection
- Server-level notification config with explicit channel/thread routing, role mention, enable/disable switch, and delivery check command
- Channel and DM subscriptions with backward-compatible aliases
- Per-server language on servers and personal language in DMs
- Built-in support for major world languages
- Asset links with optional image attachments per server
- Externalized localization catalogs in `locales/`
- SQLite storage for subscriptions, user profiles, latest assets, and deadline state
- One-time legacy JSON migration through the Go migrator

## Supported Languages

- `ar`: العربية
- `az`: Azərbaycanca
- `bn`: বাংলা
- `de`: Deutsch
- `en`: English
- `es`: Español
- `fr`: Français
- `hi`: हिन्दी
- `ja`: 日本語
- `ka`: ქართული
- `ko`: 한국어
- `pl`: Polski
- `pt`: Português
- `ru`: Русский
- `tr`: Türkçe
- `uk`: Українська
- `ur`: اردو
- `zh`: 简体中文

## Commands

All commands are top-level slash commands. There is no `/assets` wrapper anymore. Server configuration commands require Administrator permissions; DM commands affect only the current user.

- `/sub [channel]`: in DMs subscribes you to DM updates. In servers, creates or updates the server config, sets the selected channel/thread or current channel/thread, enables notifications immediately, and sends current assets once.
- `/unsub`: in DMs unsubscribes you. In servers, disables sending without deleting the configured channel, thread, role, language, or image settings.
- `/enable`: enables an existing server config. It does not choose a channel; if the config is incomplete or the bot lacks permissions, it explains what is missing.
- `/disable`: disables server sending without deleting the server config.
- `/set-channel [channel]`: sets the selected text/news channel, or the current channel if omitted. Also clears the configured thread.
- `/set-thread [thread]`: sets the selected thread, or the current thread if omitted. Notifications are sent to that thread while it is configured.
- `/clear-thread`: removes the configured thread and sends notifications back to the configured channel.
- `/set-role @role`: mentions the selected role in server notifications.
- `/clear-role`: removes the configured role mention.
- `/images [on|off]`: shows or changes whether asset images are attached to server notifications.
- `/settings`: shows the current DM or server configuration, including status, channel, thread, role, images, language, and next check time.
- `/check [channel]`: sends a small delivery-check notification to the configured channel/thread, or to a temporary selected channel/thread without saving it. It does not send the current asset list.
- `/time`: shows time left until the next scheduled check.
- `/lang [locale-code]`: shows or changes the language. In servers, changing the language requires Administrator permissions.

Slash commands are registered globally on startup with a full bulk overwrite, so global commands removed from this list are removed for every server after Discord propagates the update. Guild-scoped test commands are separate; if you created any for a test server, clear that guild with a guild command overwrite.

## Quick Start

Requirements:

- Go 1.26.1+
- A Discord bot token in `ASSETS_BOT_TOKEN`
- Playwright for Go installed with Firefox support for local runs:
  `go run github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 install firefox`
- Optional: `ASSETS_BOT_BROWSER` pointing to a Mozilla Firefox executable if you want to use a system Firefox instead of the bundled Playwright Firefox

Run the bot:

```bash
go run ./cmd/bot
```

Run the one-time legacy JSON migration:

```bash
go run ./cmd/migrate_legacy
```

## Environment

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `ASSETS_BOT_TOKEN` | Yes | - | Discord bot token |
| `ASSETS_BOT_LOCALE` | No | `ru-RU` | Default locale for DMs without a personal override and for channels without a saved locale |
| `ASSETS_BOT_DATA_DIR` | No | `/data` on Linux, `data` on Windows | Directory that stores the SQLite database |
| `ASSETS_BOT_DATABASE_URL` | No | `sqlite+aiosqlite:///<data-dir>/bot.db` | Explicit SQLite connection string; if omitted, the bot uses SQLite inside the data directory |
| `ASSETS_BOT_BROWSER` | No | Playwright Firefox | Optional Firefox executable or absolute path used for headless Fab scraping |

## Project Layout

- `cmd/bot`: main bot entrypoint
- `cmd/migrate_legacy`: one-time legacy JSON importer
- `internal/app`: bootstrap and store adapter
- `internal/discord`: Discord runtime, commands, scheduling, delivery
- `internal/fab`: Fab scrape/parser logic
- `internal/store/sqlite`: SQLite persistence layer
- `internal/i18n`: locale loading and translation
- `internal/model`: domain model and normalization
- `locales/`: localization catalogs

## Docker

Default bot image:

```bash
docker build -t epic-assets-notify-bot .
docker run --rm -e ASSETS_BOT_TOKEN=... epic-assets-notify-bot
```

Optional legacy migration image:

```bash
docker build --target migrate-legacy -t epic-assets-migrate .
docker run --rm -e ASSETS_BOT_DATA_DIR=/data -v $(pwd)/data:/data epic-assets-migrate
```

## License

MIT. See `LICENSE`.
