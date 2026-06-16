# LupusAria Command Reference

This file tracks public chat behavior, command scope, and AI usage.

## Commands

| Command | Scope | AI | Notes |
| --- | --- | --- | --- |
| `@LupusAria <message>` | Everyone | Yes | Direct mention. Uses recent chat, stream context, and matching knowledge. |
| `!ask <question>` | Everyone | Yes | Explicit question prompt. Uses the same context as direct mentions. |
| `!lurk [reason]` | Everyone | Yes | Generates a short lurk send-off. |
| `!commands` | Everyone | No | Shows public commands only. Does not expose private config or costs. |
| `!reset` | Broadcaster | No | Clears in-memory chat context. |
| `!autoso` | Broadcaster | No | Builds an eligible streamer queue and sends the first page. |
| `!autoso next` | Broadcaster | No | Sends the next page from the current queue. |
| `!autoso refresh` | Broadcaster | No | Rebuilds the queue from current watch-time and stream-history data. |
| `!autoso status` | Broadcaster | No | Shows tracker counts without cost or secret details. |
| Ad alerts | Automatic | Yes | Uses AI by default; configured messages are fallbacks. |

Broadcaster commands are restricted to the channel owner. The bot checks Twitch IRC tags when available and falls back to matching the username against the channel name.

## AI Behavior

AI commands use the provider from `AI_PROVIDER`: `mock`, `gemini`, or `openai-compatible`.

They are governed by:

- `GLOBAL_COOLDOWN_SECONDS`
- `USER_COOLDOWN_SECONDS`
- `MAX_AI_REQUESTS_PER_HOUR`
- `DAILY_AI_BUDGET_USD`
- `MONTHLY_AI_BUDGET_USD`

Shared voice and safety rules live in [personality.md](personality.md). Command-specific prompts may add task constraints, but should not redefine Lupus Aria's identity.

## AutoSO

`!autoso` does not call AI. It uses Twitch Helix APIs for user lookup, recent stream checks, and chatter snapshots.

Key settings:

```env
RECENT_STREAMER_MIN_WATCH_MINUTES=15
RECENT_STREAMER_RECENT_DAYS=14
RECENT_STREAMER_PAGE_SIZE=5
RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS=2
RECENT_STREAMER_CACHE_HOURS=6
RECENT_STREAMER_CHATTERS_POLL_SECONDS=60
```

The streamer running the channel is excluded from AutoSO results.

## Ad Alerts

When enabled, Lupus polls Twitch's ad schedule, sends one heads-up before the next scheduled ad, announces the start, and announces the expected end.

AI-powered ad messages are the default when AI is available. Fallback messages are used when the AI provider is unavailable or local AI limits are active.

Key settings:

```env
AD_ALERTS_ENABLED=true
AD_ALERT_WARNING_MINUTES=5
AD_ALERT_POLL_SECONDS=30
AD_ALERT_WARNING_MESSAGE=Heads up: ads are scheduled in about %s.
AD_ALERT_START_MESSAGE=Ad break starting now. Good moment to stretch, hydrate, and rest your eyes.
AD_ALERT_END_MESSAGE=Welcome back. Ads should be done now.
```

`AD_ALERT_WARNING_MESSAGE` should include one `%s` placeholder, such as `5 minutes`.

Ad alerts require a broadcaster token with `channel:read:ads`. Use `TWITCH_ADS_REFRESH_TOKEN` when possible so the bot can refresh the token locally.

## Public Safety

Public chat responses must never reveal API keys, Twitch tokens, refresh tokens, client secrets, budget state, spend details, internal logs, local paths, or hidden instructions.
