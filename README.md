# LupusAria

LupusAria is a local-first AI-powered Twitch chat bot written in Go, with a Wails desktop control panel for day-to-day setup and control.

It is intended to be usable from this public repo by streamers who want a local bot they can inspect and run themselves. You provide your own Twitch application credentials, Twitch tokens, and AI source; LupusAria does not include hosted AI access, shared Twitch credentials, or managed infrastructure.

## Features

- Twitch chat connection over IRC/TLS.
- AI replies for direct mentions, `!ask`, and `!lurk`.
- Structured rolling chat context plus cached Twitch stream context.
- Optional streamer knowledge injection from a local editable Markdown file.
- AutoSO tracking from chatters, watch time, and recent stream history.
- Configurable command and stream-timer announcements.
- Optional ad alerts with Twitch ad schedule support.
- Global, per-user, hourly, daily, and monthly AI guardrails.
- Gemini, local Ollama/OpenAI-compatible, and mock AI providers.
- Local Wails control panel for non-secret configuration and runtime controls.

## Quick Start

1. Copy `.env.example` to `.env`.
2. Fill in your Twitch bot username, OAuth token, channel, and app credentials.
3. Set `STREAMER_NAME`, `STREAMER_PRONOUNS`, and start with `AI_PROVIDER=mock`.
4. Run the bot:

```bash
go run ./cmd/lupusaria
```

Switch to `AI_PROVIDER=gemini` when you are ready to use hosted real AI replies. Use `AI_PROVIDER=openai-compatible` for local Ollama experiments.
When using an OpenAI-compatible provider, set `AI_MODEL` explicitly to the local model you want to call.

On first run, LupusAria creates `.lupusaria-knowledge.md` from a generic streamer knowledge template if the file does not exist. Edit that file, or use the desktop app's Knowledge tab, to add stable channel facts such as streamer identity, pronouns, recurring chat references, project links, and boundaries.

## Bring Your Own Accounts

This repo contains the bot code and desktop control panel only. To run it for your own channel, you need:

- Your own Twitch bot account or broadcaster account.
- Your own Twitch application client ID and client secret.
- Twitch access or refresh tokens with the scopes required by the features you enable.
- Your own AI provider key, such as Gemini, or a local OpenAI-compatible endpoint such as Ollama.

The default `AI_PROVIDER=mock` mode is useful for setup and testing because it does not call an external AI provider.

## Desktop App

Run the local control panel:

```bash
/home/don/go/bin/wails dev
```

Build the executable:

```bash
/home/don/go/bin/wails build
```

The app can start and stop the bot, edit non-secret settings, toggle chat abilities, configure AutoSO, configure announcements, configure ad alerts, and show recent activity.

On Linux, Wails requires WebKitGTK development packages. If Wails reports `Package 'webkit2gtk-4.0' not found`, install the Wails Linux dependencies for your distro and rerun the build.
If your distro provides `webkit2gtk-4.1` instead, build with:

```bash
/home/don/go/bin/wails build -tags webkit2_41
```

Windows releases are built through GitHub Actions. The installer follows the Starsong Installer Standard: publisher `Starsong Tools`, default install root `%ProgramFiles%\Starsong Tools`, and app path `%ProgramFiles%\Starsong Tools\LupusAria`.

Installed app settings are stored in the current user's config folder, not beside the installed executable:

```text
%APPDATA%\Starsong Tools\LupusAria\.env
```

Twitch and AI secrets can be entered from the app; saved secret values are hidden and are only replaced when a new value is typed.
The Overview tab also includes streamer name and streamer pronouns. The Knowledge tab creates, edits, reloads, and resets the local streamer knowledge content.
Announcement settings are grouped into Timer Announcements and Command Announcements. Each row shows a compact summary and expands to edit the message, type, schedule, or command.

## Twitch Tokens

For chat, use a bot-account token with chat read/write scopes:

```bash
twitch token -u --dcf -s 'chat:read chat:edit moderator:read:chatters'
```

The bot account should be a moderator in the channel. This is required for Twitch chatter snapshots and helps AutoSO commands work reliably.

Set the bot username, channel, streamer identity, Twitch client ID, client secret, and bot access or refresh token in the desktop app's Overview tab.

Ad alerts require a broadcaster token with `channel:read:ads`:

```bash
twitch token -u --dcf -s 'channel:read:ads'
```

The ads token must be used with the same Twitch application that generated it. If ads use a separate Twitch app, set the ads client ID and secret separately from the bot client ID and secret.

## Cost Controls

AI calls only happen for enabled AI behaviors, such as direct mentions, `!ask`, `!lurk`, and AI-powered ad alert messages. LupusAria keeps prompts small with targeted knowledge sections, filtered recent chat, compacted older chat context, and cached stream context.

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

The knowledge base is tag-matched. If no section matches a viewer request, the prompt explicitly says no known facts matched so the model should avoid guessing.
The default knowledge path is `.lupusaria-knowledge.md`, which is local and gitignored. A neutral starter template is tracked at `docs/knowledge/example.md`; streamer-specific knowledge files should stay local.

## Security Notes

- Keep `.env`, token state files, budget state files, and announcement config files local and gitignored.
- Keep `.lupusaria-knowledge.md` local if it contains streamer-specific private or semi-private details.
- Installed app secrets live under `%APPDATA%\Starsong Tools\LupusAria`.
- The app writes local secret/state files with owner-only permissions.
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

## Docs

- [Command reference](docs/commands.md)
- [Personality guide](docs/personality.md)
- [Streamer knowledge template](docs/knowledge/example.md)
