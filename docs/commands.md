# LupusAria Command Reference

This document tracks chat commands, who can use them, and whether they call the configured AI provider.

## Summary

| Command | Who Can Use It | Uses AI | Notes |
| --- | --- | --- | --- |
| `@LupusAria <message>` | Everyone | Yes | Direct mention prompt. Uses chat context and stream context. |
| `!ask <question>` | Everyone | Yes | Explicit question prompt. Uses chat context and stream context. |
| `!lurk [reason]` | Everyone | Yes | Generates a short lurk send-off. |
| `!bot` | Everyone | No | Shows public usage. Does not expose cost, keys, or private config. |
| `!help` | Everyone | No | Alias of `!bot`. |
| `!commands` | Everyone | No | Alias of `!bot`. |
| `!reset` | Broadcaster only | No | Clears the bot's in-memory chat context. |
| `!autoso` | Broadcaster only | No | Builds a frozen eligible streamer queue and sends the first shoutout page. |
| `!autoso next` | Broadcaster only | No | Sends the next page from the frozen queue. |
| `!autoso refresh` | Broadcaster only | No | Rebuilds the queue from current watch-time and stream-history data. |
| `!autoso status` | Broadcaster only | No | Shows tracker counts without exposing private cost details. |

## AI Commands

AI commands are subject to:

- `GLOBAL_COOLDOWN_SECONDS`
- `USER_COOLDOWN_SECONDS`
- `MAX_AI_REQUESTS_PER_HOUR`
- `DAILY_AI_BUDGET_USD`
- `MONTHLY_AI_BUDGET_USD`

They use the configured provider from `AI_PROVIDER`. Current supported providers are `mock`, `gemini`, and `openai-compatible`.

AI responses use the shared voice rules in [personality.md](personality.md). Command-specific prompts can add tighter constraints, but they should not redefine the bot's whole identity.

## Broadcaster Commands

Broadcaster commands are restricted to the channel owner. The bot checks Twitch IRC tags when available and also falls back to matching the username against the channel name.

`!autoso` commands do not call AI, but they do use Twitch Helix APIs for user lookup, stream-history lookup, and chatter snapshots.

## Public Safety

Public chat responses must not reveal:

- API keys
- Twitch tokens
- Refresh tokens
- Client secrets
- Budget state
- Cost/spend details
- Internal logs or paths
