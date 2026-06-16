# LupusAria

LupusAria is a local-first AI-powered Twitch chat bot written in Go.

The first version is intentionally small: it connects directly to Twitch chat, listens for mentions or `!ask`, keeps a short rolling chat context, and replies through a pluggable AI provider.

## Why local-first?

This bot is designed for one streamer, not a hosted multi-tenant service. Running locally keeps the architecture simple, cheap, and easy to debug while still leaving room to add hosting later.

## Current Features

- Connects to Twitch chat over IRC/TLS.
- Responds to `@BotName ...` and `!ask ...`.
- Keeps recent chat context in memory.
- Adds cached Twitch stream context to AI prompts.
- Uses global and per-user cooldowns.
- Supports a `mock` AI provider for safe local testing.
- Supports Gemini for low-cost real replies.
- Supports an OpenAI-compatible chat completions endpoint as an alternate provider.

## Setup

1. Copy `.env.example` to `.env`.
2. Fill in your Twitch bot username, OAuth token, and channel.
3. Start with `AI_PROVIDER=mock` to verify Twitch chat works.
4. Switch to `AI_PROVIDER=gemini` when you are ready to call a real model.

```bash
go run ./cmd/lupusaria
```

## Twitch Token

For local development, use a token for the bot account with chat read/write scopes. Twitch tokens usually look like `oauth:...` for IRC.

## Cost Controls

The bot only calls AI when directly mentioned or when `!ask` is used. Cooldowns are enabled by default:

- `GLOBAL_COOLDOWN_SECONDS`
- `USER_COOLDOWN_SECONDS`
- `STREAM_CONTEXT_TTL_SECONDS`
- `MAX_AI_REQUESTS_PER_HOUR`
- `DAILY_AI_BUDGET_USD`
- `MONTHLY_AI_BUDGET_USD`
- `AI_BUDGET_STATE_PATH`

Keep `MAX_CONTEXT_MESSAGES` modest so prompts stay small.

For Gemini, start with:

```env
AI_PROVIDER=gemini
GEMINI_API_KEY=your_key_here
GEMINI_MODEL=gemini-3.1-flash-lite
```

## Commands

- `@LupusAria <message>` asks the bot directly.
- `!ask <question>` asks the bot directly.
- `!lurk [reason]` generates a short lurk send-off.
- `!bot`, `!help`, or `!commands` show public usage.
- `!reset` clears recent chat context for the broadcaster only.

Spend and budget details are intentionally not exposed through public chat commands.

## Twitch Token Refresh

The bot can refresh its Twitch access token on startup when these are set:

```env
TWITCH_CLIENT_ID=your_twitch_app_client_id
TWITCH_CLIENT_SECRET=your_twitch_app_client_secret
TWITCH_REFRESH_TOKEN=your_refresh_token
TWITCH_TOKEN_STATE_PATH=.lupusaria-twitch-token.json
```

The token state file is local and gitignored.
