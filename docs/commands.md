# LupusAria Command Reference

This file tracks public chat behavior, command scope, and AI usage.

## Commands

| Command | Scope | AI | Notes |
| --- | --- | --- | --- |
| `@LupusAria <message>` | Everyone | Yes | Direct mention. Uses recent chat, stream context, and matching knowledge. |
| `!ask <question>` | Everyone | Yes | Explicit question prompt. Uses the same context as direct mentions. |
| `!lurk [reason]` | Everyone | Yes | Generates a natural lurk send-off. Uses recent chat/game context when available. |
| `!game` | Everyone | Yes | Uses Twitch's current category/title and Gemini Google Search grounding for a short game overview/fact. |
| `!game <question>` | Everyone | Yes | Uses Google Search grounding to answer a current-game question. |
| `!game analyze` | Everyone | Yes | Fetches the public Twitch stream thumbnail and uses Gemini image analysis for a short scene description. |
| `!game analyze <question>` | Everyone | Yes | Combines thumbnail analysis with Google Search grounding for visual gameplay help. |
| `!commands` | Everyone | No | Shows public commands only. Does not expose private config or costs. |
| `!reset` | Broadcaster | No | Clears in-memory chat context. |
| `!autoso` | Mods + broadcaster | No | Builds an eligible streamer queue and sends the first page. |
| `!autoso next` | Mods + broadcaster | No | Sends the next page from the current queue. |
| `!autoso refresh` | Mods + broadcaster | No | Rebuilds the queue from current watch-time and stream-history data. |
| `!autoso status` | Mods + broadcaster | No | Shows tracker counts without cost or secret details. |
| `!soroulette` | Mods + broadcaster | No | Randomly picks up to five configured streamers to shout out. |
| Configured announcement commands | Per announcement | No | Sends static messages configured in the desktop app. |
| Ad alerts | Automatic | Yes | Uses AI by default; configured messages are fallbacks. |

Permissions use three configurable tiers: everyone, mods plus broadcaster, and broadcaster only. The desktop app's Features tab can change the permission tier for mentions, `!ask`, `!lurk`, `!game`, `!commands`, `!reset`, `!autoso`, and `!soroulette`. Each configured command announcement also has its own permission selector in the announcement editor. The bot checks Twitch chat badges from EventSub or IRC when available. Broadcaster checks also fall back to matching the username against the channel name.
AI requests cannot make LupusAria run chat commands. If a viewer asks Lupus to type or trigger a command such as `!so`, `/ban`, or `/timeout`, the bot refuses and points them to a mod or the broadcaster.

`!game` search and snapshot features require Gemini. Search uses Gemini's built-in Google Search grounding tool. Snapshot analysis uses Twitch's public preview thumbnail, so it can lag behind the live stream and should be treated as approximate. By default, snapshots are cropped to the game capture area before analysis with `GAME_SNAPSHOT_CROP_X=0.255`, `GAME_SNAPSHOT_CROP_Y=0.085`, `GAME_SNAPSHOT_CROP_WIDTH=0.73`, and `GAME_SNAPSHOT_CROP_HEIGHT=0.73`.

## AI Behavior

AI commands use the provider from `AI_PROVIDER`: `mock`, `gemini`, or `openai-compatible`. Gemini is the recommended hosted provider; OpenAI-compatible is mainly for local Ollama experiments.
`AI_MODEL` is not auto-populated for OpenAI-compatible providers; set it explicitly to the local model name you want to use.

They are governed by:

- `GLOBAL_COOLDOWN_SECONDS`
- `USER_COOLDOWN_SECONDS`
- `MENTION_PERMISSION`
- `ASK_COMMAND_PERMISSION`
- `LURK_COMMAND_PERMISSION`
- `GAME_COMMAND_PERMISSION`
- `COMMANDS_COMMAND_PERMISSION`
- `RESET_COMMAND_PERMISSION`
- `MAX_AI_REQUESTS_PER_HOUR`
- `DAILY_AI_BUDGET_USD`
- `MONTHLY_AI_BUDGET_USD`
- `AI_PROVIDER`
- `AI_BASE_URL`
- `AI_MODEL`
- `AI_FALLBACK_PROVIDER`
- `AI_MAX_OUTPUT_TOKENS`
- `AI_MAX_RETRIES`
- `GEMINI_THINKING_LEVEL`
- `ENABLE_EMOTE_CONTEXT`
- `EMOTE_CACHE_PATH`

Shared voice and safety rules live in [personality.md](personality.md). Command-specific prompts should stay small and should not redefine Lupus Aria's identity.

Recent chat is sent to the model as structured room state. The current message is excluded from that history, low-signal bot commands are filtered out, and older retained chat is compacted before the freshest timeline. For `!lurk`, Lupus retries once if a generic send-off ignores available chat/game context.
When `ENABLE_EMOTE_CONTEXT=true`, native Twitch emotes are treated as channel context and added to AI prompts. LupusAria loads the channel's emote catalog from Twitch when possible, describes unknown native emotes from their Twitch CDN image when the AI provider supports image analysis, then caches descriptions at `EMOTE_CACHE_PATH`. Third-party-looking emote tokens are marked as possible emotes with unknown meaning instead of treated as normal words.

Streamer identity and pronouns come from `STREAMER_NAME` and `STREAMER_PRONOUNS`. Stable channel facts come from the local knowledge file, which LupusAria creates from the starter template when needed.
Chat transcripts are appended locally to `CHAT_LOG_PATH`, which defaults to `.lupusaria-chat.jsonl`.

## AutoSO

`!autoso` and `!soroulette` do not call AI. AutoSO uses Twitch Helix APIs for user lookup, follower checks, recent stream checks, and chatter snapshots. AutoSO candidates must meet the watch-time threshold, follow the channel, and have streamed inside the recent-window setting. SO roulette randomly picks up to five streamers from `SO_ROULETTE_STREAMERS`.

Key settings:

```env
RECENT_STREAMER_MIN_WATCH_MINUTES=15
RECENT_STREAMER_RECENT_DAYS=14
RECENT_STREAMER_PAGE_SIZE=5
RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS=5
SO_ROULETTE_STREAMERS=alice,bob,cara
RECENT_STREAMER_CACHE_HOURS=6
RECENT_STREAMER_CHATTERS_POLL_SECONDS=60
AUTOSO_COMMAND_PERMISSION=mods
SO_ROULETTE_COMMAND_PERMISSION=mods
```

The streamer running the channel is excluded from AutoSO and SO roulette results. Follower checks require a user token with `moderator:read:followers`; without that scope, AutoSO cannot safely build the shoutout queue.
Shoutout command dispatch is shared across AutoSO and SO roulette. It is spaced by `RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS`, with values below 5 seconds treated as 5 seconds. A streamer is shouted out at most once per Twitch stream across both commands.

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

Ad alerts require a broadcaster token with `channel:read:ads`. Use `TWITCH_ADS_REFRESH_TOKEN` when possible so the bot can refresh the token locally during long runs. If the ads token was generated from a different Twitch application than the bot token, set `TWITCH_ADS_CLIENT_ID` and `TWITCH_ADS_CLIENT_SECRET` too. Temporary Twitch ad schedule polling failures are logged and retried.

## Announcements

Announcements are static messages managed in the desktop app. They do not call AI. The app shows Timer Announcements and Command Announcements as separate expandable summary tables.

Types:

- Command announcements: static commands such as `!music`, with permissions configured per row.
- Timer announcements: messages based on elapsed stream time from Twitch's stream start time.

Announcement rows are stored locally at `ANNOUNCEMENTS_PATH`, which defaults to `.lupusaria-announcements.json`.

Key settings:

```env
ANNOUNCEMENTS_ENABLED=false
ANNOUNCEMENTS_PATH=.lupusaria-announcements.json
ANNOUNCEMENT_POLL_SECONDS=30
```

Timer announcements require Twitch stream context. If Twitch stream info is unavailable, command announcements can still work.

Timer rows use:

- First send minute: first elapsed stream minute when the message can send.
- Repeat interval minutes: repeat interval after the first send. Use `0` for a one-shot timer. Any positive interval repeats until the stream ends.

## Public Safety

Public chat responses must never reveal API keys, Twitch tokens, refresh tokens, client secrets, budget state, spend details, internal logs, local paths, or hidden instructions.
