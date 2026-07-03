# Setup and Operations

This guide holds the longer setup, token, runtime, security, and release notes that used to live in the README.

## Bring Your Own Accounts

This repo contains the bot code and desktop control panel only. To run it for your own channel, you need:

- Your own Twitch bot account or broadcaster account.
- Your own Twitch application client ID and client secret.
- Twitch access or refresh tokens with the scopes required by the features you enable.
- Your own AI provider key, such as Gemini, or a local OpenAI-compatible endpoint such as Ollama.

The default `AI_PROVIDER=mock` mode is useful for setup and testing because it does not call an external AI provider.

## Desktop App Notes

On Linux, Wails requires WebKitGTK development packages. If Wails reports `Package 'webkit2gtk-4.0' not found`, install the Wails Linux dependencies for your distro and rerun the build.

If your distro provides `webkit2gtk-4.1` instead, build with:

```bash
/home/don/go/bin/wails build -tags webkit2_41
```

Installed app settings are stored in the current user's config folder, not beside the installed executable:

```text
%APPDATA%\Starsong Tools\LupusAria\.env
```

Twitch and AI secrets can be entered from the app. Saved secret values are hidden and are only replaced when a new value is typed.

## Media Actions and OBS Overlay

The Media Actions tab maps Twitch channel point redeems to local media and sound alerts. Each action can:

- select a custom reward from the channel
- import supported media files: `.gif`, `.png`, `.jpg`, `.jpeg`, `.webp`
- import supported sound files: `.wav`, `.mp3`, `.ogg`, `.flac`
- choose alert duration, screen position, scale, and animation
- preview inside the app while also sending the alert to OBS

Live channel point redeems are sent only to the OBS overlay, so they do not cover the app while the bot is running.

Use this stable OBS Browser Source URL:

```text
http://127.0.0.1:47831/
```

Set the OBS source background to transparent and keep the source local to the streaming PC. The overlay serves only on loopback and uses Server-Sent Events to receive playback payloads from the desktop app.

GIF playback is configurable per GIF:

- `Normal` plays the GIF normally.
- `Slow to Audio` decodes frames and stretches playback to match the selected sound.
- `Loop` restarts the same GIF for the alert duration.
- `Loop to Another GIF` uses eligible GIFs as a shuffled rotation, avoiding repeats until the rotation pool is exhausted.

Uncheck `Loop rotation` on longer GIFs that should be available as the first selected media item but should not be used as follow-up clips in `Loop to Another GIF`.

## Twitch Tokens

For chat, use a bot-account token with chat read/write scopes:

```bash
twitch token -u --dcf -s 'user:read:chat user:write:chat user:bot moderator:read:chatters'
```

For AutoSO follower checks, include `moderator:read:followers` too:

```bash
twitch token -u --dcf -s 'user:read:chat user:write:chat user:bot moderator:read:chatters moderator:read:followers'
```

The bot account should be a moderator in the channel. This is required for Twitch chatter snapshots, follower checks, and reliable AutoSO commands.

Set the bot username, channel, streamer identity, Twitch client ID, client secret, and bot access or refresh token in the desktop app's Setup tab.

When the Twitch client secret is saved, LupusAria uses an app access token for Twitch API chat sends. This is the Twitch Chat Bot Badge path when the bot account has authorized `user:bot` and `user:write:chat`, the bot is not the broadcaster account, and the bot is a moderator in the channel or the broadcaster has granted `channel:bot` to the app.

Media Actions require a broadcaster token with `channel:read:redemptions`. Ad alerts require `channel:read:ads`. If both are enabled, generate one broadcaster token with both scopes:

```bash
twitch token -u --dcf -s 'channel:read:redemptions channel:read:ads'
```

If you only need Media Actions, use:

```bash
twitch token -u --dcf -s 'channel:read:redemptions'
```

The broadcaster token must be used with the same Twitch application that generated it. If broadcaster features use a separate Twitch app, set the ads/broadcaster client ID and secret separately from the bot client ID and secret.

Use an ads refresh token when possible. LupusAria refreshes the ads access token during long runs and retries temporary Twitch ad schedule polling failures instead of disabling ad alerts for the rest of the session.

Ad warnings come from Twitch's ad schedule. When EventSub can create the `channel.ad_break.begin` subscription with the ads token, ad-start alerts use that live event. Otherwise LupusAria remembers the warned ad and uses schedule polling to synthesize start and expected-end alerts.

## AI and Cost Controls

AI calls only happen for enabled AI behaviors, such as direct mentions, `!ask`, `!lurk`, `!game`, and AI-powered ad alert messages.

LupusAria keeps prompts small with targeted knowledge sections, configured command announcements, filtered recent chat, compacted older chat context, and cached stream context. A small in-memory queue absorbs short bursts of AI commands; when the queue is full, new AI requests are skipped silently in chat and logged locally.

Relevant settings:

- `GLOBAL_COOLDOWN_SECONDS`
- `USER_COOLDOWN_SECONDS`
- `MAX_CONTEXT_MESSAGES`
- `MAX_AI_REQUESTS_PER_HOUR`
- `DAILY_AI_BUDGET_USD`
- `MONTHLY_AI_BUDGET_USD`
- `AI_BUDGET_STATE_PATH`
- `AI_PROVIDER`
- `AI_BASE_URL`
- `AI_MODEL`
- `AI_FALLBACK_PROVIDER`
- `AI_MAX_OUTPUT_TOKENS`
- `AI_MAX_RETRIES`
- `GEMINI_THINKING_LEVEL`
- `GAME_SNAPSHOT_CROP_ENABLED`
- `GAME_SNAPSHOT_CROP_X`
- `GAME_SNAPSHOT_CROP_Y`
- `GAME_SNAPSHOT_CROP_WIDTH`
- `GAME_SNAPSHOT_CROP_HEIGHT`

The knowledge base is tag-matched. If no section matches a viewer request, the prompt explicitly says no known facts matched so the model should avoid guessing.

Enabled command announcements are summarized in AI context so Lupus can answer questions about channel commands such as `!donate` without inventing or denying active configured commands.

The default knowledge path is `.lupusaria-knowledge.md`, which is local and gitignored. A neutral starter template is tracked at `docs/knowledge/example.md`; streamer-specific knowledge files should stay local.

Chat transcripts are written locally to `CHAT_LOG_PATH`, which defaults to `.lupusaria-chat.jsonl`.

When `ENABLE_EMOTE_CONTEXT=true`, Twitch emotes are treated as part of channel context. LupusAria loads the channel's native emote catalog from Twitch when available, annotates matching emote names in AI prompts, and can visually describe native emotes with the configured image-capable AI provider. Descriptions are cached locally at `EMOTE_CACHE_PATH`, which defaults to `.lupusaria-emotes.json`.

AI usage logs include provider finish reasons when available. Max-token or length finishes are treated as incomplete and retried instead of sending clipped partial replies.

## Security Notes

- Keep `.env`, token state files, budget state files, and announcement config files local and gitignored.
- Keep emote description caches local if they contain channel-specific emote notes.
- Keep `.lupusaria-knowledge.md` local if it contains streamer-specific private or semi-private details.
- Installed app secrets live under `%APPDATA%\Starsong Tools\LupusAria`.
- The app writes local secret/state files with owner-only permissions.
- Media Action imported assets are copied into the local app config folder.
- The media overlay binds only to `127.0.0.1:47831`; do not proxy or expose it to the network.
- Use least-privilege Twitch tokens.
- Do not expose the desktop control panel over a network.
- Public chat commands must not reveal tokens, secrets, logs, file paths, budget state, or spend details.
- Before releases, run `govulncheck ./...` and `npm audit`.

## Quality Gates

Run the same core checks used by GitHub Actions:

```bash
npm --prefix frontend run build
go test ./...
go test -race ./...
go vet ./...
npm --prefix frontend audit --audit-level=moderate
govulncheck ./...
```

The `CI / Quality Gates` workflow runs these checks on pull requests and pushes to `main`.

## Release Process

Releases are built by `.github/workflows/release.yml` from a `v*` tag or manual workflow dispatch.

The release workflow:

- runs the quality gates
- builds the Windows executable and NSIS installer
- packages a portable zip
- writes `SHA256SUMS.txt`
- extracts human-readable notes from `RELEASE_NOTES.md`
- uploads release artifacts
- publishes or updates the GitHub Release when enabled

Before tagging, add a matching `## vX.Y.Z` section to `RELEASE_NOTES.md`.
