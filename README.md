# LupusAria

LupusAria is a local-first AI-powered Twitch chat bot written in Go, with a Wails desktop control panel for day-to-day setup and control.

It is built for one streamer: simple to run locally, cheap to operate, and easy to inspect.

## Features

- Twitch chat connection over IRC/TLS.
- AI replies for direct mentions, `!ask`, and `!lurk`.
- Short rolling chat context plus cached Twitch stream context.
- Deterministic Ursa knowledge injection from `docs/knowledge/ursa.md`.
- AutoSO tracking from chatters, watch time, and recent stream history.
- Optional ad alerts with Twitch ad schedule support.
- Global, per-user, hourly, daily, and monthly AI guardrails.
- Gemini, OpenAI-compatible, and mock AI providers.
- Local Wails control panel for non-secret configuration and runtime controls.

## Quick Start

1. Copy `.env.example` to `.env`.
2. Fill in Twitch bot username, OAuth token, and channel.
3. Start with `AI_PROVIDER=mock`.
4. Run the bot:

```bash
go run ./cmd/lupusaria
```

Switch to `AI_PROVIDER=gemini` when you are ready to use real AI replies.

## Desktop App

Run the local control panel:

```bash
/home/don/go/bin/wails dev
```

Build the executable:

```bash
/home/don/go/bin/wails build
```

The app can start and stop the bot, edit non-secret settings, toggle chat abilities, configure AutoSO, configure ad alerts, and show recent activity.

On Linux, Wails requires WebKitGTK development packages. If Wails reports `Package 'webkit2gtk-4.0' not found`, install the Wails Linux dependencies for your distro and rerun the build.

## Twitch Tokens

For chat, use a bot-account token with chat read/write scopes:

```bash
twitch token -u --dcf -s 'chat:read chat:edit moderator:read:chatters'
```

The bot account should be a moderator in the channel. This is required for Twitch chatter snapshots and helps AutoSO commands work reliably.

Ad alerts require a broadcaster token with `channel:read:ads`:

```bash
twitch token -u --dcf -s 'channel:read:ads'
```

## Cost Controls

AI calls only happen for enabled AI behaviors, such as direct mentions, `!ask`, `!lurk`, and AI-powered ad alert messages. Keep prompts small by using modest chat context and targeted knowledge sections.

Relevant settings:

- `GLOBAL_COOLDOWN_SECONDS`
- `USER_COOLDOWN_SECONDS`
- `MAX_CONTEXT_MESSAGES`
- `MAX_AI_REQUESTS_PER_HOUR`
- `DAILY_AI_BUDGET_USD`
- `MONTHLY_AI_BUDGET_USD`
- `AI_BUDGET_STATE_PATH`

The knowledge base is tag-matched. If no section matches a viewer request, the prompt explicitly says no known facts matched so the model should avoid guessing.

## Security Notes

- Keep `.env`, token state files, and budget state files local and gitignored.
- The app writes local secret/state files with owner-only permissions.
- Use least-privilege Twitch tokens.
- Do not expose the desktop control panel over a network.
- Public chat commands must not reveal tokens, secrets, logs, file paths, budget state, or spend details.
- Before releases, run `govulncheck ./...` and `npm audit`.

## Docs

- [Command reference](docs/commands.md)
- [Personality guide](docs/personality.md)
- [Ursa knowledge base](docs/knowledge/ursa.md)
