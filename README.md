# LupusAria

LupusAria is a local-first AI-powered Twitch chat bot with a Wails desktop control panel for setup, live raffles, chat operation, and post-stream wrap-up.

It is intended to be usable from this public repo by streamers who want a local bot they can inspect and run themselves. You provide your own Twitch application credentials, Twitch tokens, and AI source; LupusAria does not include hosted AI access, shared Twitch credentials, or managed infrastructure.

## Features

- Twitch chat over EventSub WebSockets, Twitch API sends, and IRC fallback for incomplete setup.
- AI replies for direct mentions, `!ask`, `!lurk`, and grounded `!game` help.
- Structured rolling chat context, cached Twitch stream context, local transcript logging, and editable streamer knowledge.
- Twitch emote context enrichment with local cached visual descriptions.
- AutoSO tracking from chatters, watch time, recent stream history, and configurable `!soroulette` pools.
- Configurable command and stream-timer announcements with direct link recall, deduplicated ad-pod alerts, and channel point Media Actions with Twitch cooldown visibility for OBS overlays.
- Broadcast-bound raffles with durable entries, cryptographic winner selection, broadcaster-only Pick/Clear commands, and live desktop status. Winners leave the raffle until the next clear.
- Persistent Stream Wrap-Up tracking for follows, subscriptions, gift subscriptions, Bits, and incoming raids, with a live report and copyable thank-you list.
- Reconnecting OBS Media Actions overlay with visual placement, independent entrance and exit animations, styled captions, GIF playback controls, and per-sound volume.
- Global, per-user, hourly, daily, and monthly AI guardrails.
- OpenAI GPT-5.6 Luna, Gemini, local Ollama/OpenAI-compatible, and mock AI providers.
- Local Wails control panel for account setup, permission validation, secrets, feature configuration, knowledge editing, media actions, wrap-up reporting, and runtime controls.

## Install on Windows

Download the installer or portable archive from the [latest GitHub release](https://github.com/LastUrsa/LupusAria/releases/latest). Each release also includes a standalone executable and `SHA256SUMS.txt` for verifying the downloads. The installer uses publisher `Starsong Tools`, installs under `%ProgramFiles%\Starsong Tools\LupusAria`, and stores settings under:

```text
%APPDATA%\Starsong Tools\LupusAria\.env
```

For first-time setup:

1. Open **Setup** and enter the channel, bot username, streamer identity, and Twitch application credentials.
2. Add a bot-account token for chat and a broadcaster-account token for Stream Wrap-Up, Media Actions, and ad alerts. These are separate authorization roles—typically a bot login and the channel-owner login—even when both tokens use the same Twitch application.
3. Save, then use **Check permissions** to verify both tokens and their scopes.
4. Start with the mock AI provider, or configure your own OpenAI, Gemini, or local OpenAI-compatible endpoint.
5. Select **Start bot**.

The exact Twitch CLI commands and required scopes are in [Twitch Tokens](docs/setup.md#twitch-tokens). Authorize the broadcaster token while signed in as the channel owner, and retain its refresh token so LupusAria can renew access.

For hosted GPT-5.6 Luna replies, select the OpenAI-compatible provider, set the model to `gpt-5.6-luna`, and enter your own OpenAI API key. Gemini is supported as a primary or fallback provider and is required for the current `!game` search and image-analysis paths. OpenAI-compatible mode also works with local Ollama models when you provide their model name and endpoint.

On first run, LupusAria creates `.lupusaria-knowledge.md` from a neutral template if the file does not exist. Edit it directly, or use the desktop app's Knowledge tab, to add stable channel facts such as streamer identity, pronouns, recurring chat references, project links, and boundaries.

## Run a Raffle

Open **Features → Raffle** and keep **Enable raffle** on. Save and restart the bot if you change the switch. Once Twitch confirms your live broadcast, the panel shows `ACTIVE` and viewers can enter:

| Command | Who can use it | What it does |
| --- | --- | --- |
| `!rafadd` | Everyone | Enters the calling viewer once using their Twitch user ID. |
| `!rafpick` | Broadcaster | Selects and removes one winner using cryptographic randomness. |
| `!rafclear` | Broadcaster | Clears entries and winners, resets numbering to 1, and restores everyone's eligibility. |

A winner cannot re-enter the current round, including after a bot restart or a failed winner announcement. `!rafclear` starts a fresh round within the same broadcast, so previous winners can enter again. Pick and Clear require the authenticated channel owner's Twitch user ID; moderators cannot use them. These commands do not use AI.

The panel updates the remaining entry count, assigned numbers, and latest winner automatically. Remaining viewers keep their numbers when a winner is removed, so gaps are expected and every remaining viewer still has an equal chance of winning.

Entries survive app restarts and temporary Twitch disconnections. During startup recovery or stream-end confirmation, enrollment and drawing pause while Twitch status is checked. Stream-end cleanup requires two successful offline checks at least 60 seconds apart, or confirmation of a different broadcast. Each new broadcast starts empty.

Raffle state stays in a local SQLite file beside your configuration. To disable the feature while preserving committed state, turn off its switch and restart the bot. See [Raffle storage and recovery](docs/setup.md#raffle-storage-and-recovery) for settings, backup retention, migrations, and rollback.

Before upgrading, close the app and back up your configuration and local state. Version 0.11.0 adds raffle schema v2; if you used an earlier raffle candidate, its recorded current-round winners are removed from eligibility during migration. Clearing the raffle restores their eligibility.

Automated validation includes a local Twitch simulator, full-app DOM tests, and Windows installation, launch, upgrade, and uninstall checks. Live Twitch authorization/delivery and interactive visual UI validation remain unverified; see [validation details](docs/setup.md#simulated-release-candidate-validation).

## Desktop App

To run from source, install Go 1.26.11, Node.js 24, the platform-specific [Wails dependencies](https://wails.io/docs/gettingstarted/installation), and Wails v2.12.0. Then install the frontend packages and start the development app:

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
npm --prefix frontend ci
wails dev
```

Build the executable:

```bash
wails build
```

The headless bot entry point remains available for development and automation. Copy `.env.example` to `.env`, configure it, and run:

```bash
go run ./cmd/lupusaria
```

The app can start and stop the bot, manage account setup and saved secrets, validate Twitch permissions, edit AI and budget settings, configure command permissions, tune `!game` snapshot crop, maintain streamer knowledge, configure announcements and Media Actions, monitor the current raffle under **Features → Raffle**, and display the current or most recently completed report under **Features → Stream Wrap-Up**.

## Key Concepts

- **Bring your own accounts:** you need your own Twitch bot or broadcaster account, Twitch application credentials, scoped Twitch tokens, and an API key or local endpoint for your chosen AI provider.
- **Two Twitch authorization roles:** the bot token handles chat; the broadcaster token is authorized as the channel owner and powers Wrap-Up, redeems, and ad features. The Setup permission checker reports missing credentials or scopes.
- **AI cost controls:** AI calls only happen for enabled AI behaviors and are guarded by cooldowns, request limits, and optional daily/monthly budget caps.
- **Local state:** `.env`, token states, budgets, announcements, media assets, emote caches, knowledge, chat transcripts, and the raffle SQLite database are local and gitignored.
- **Raffles:** each raffle belongs to one verified Twitch broadcast. Picking removes the winner and blocks re-entry until `!rafclear`; clearing restores eligibility without closing the broadcast session.
- **Stream Wrap-Up:** automatically records follows, subscriptions, gift subs, Bits, and incoming raids for each Twitch broadcast, then provides a live preview and complete copyable thank-you list in the desktop app.
- **Media Actions:** channel point redeems can play random local images, GIFs, and sounds, with optional styled text beneath the image, through the reconnecting local OBS Browser Source installed under the app's user-data folder.
- **Ad alerts:** Twitch ad schedule polling provides warnings; EventSub ad-break events provide live starts when available; delayed segments are merged into the current pod to avoid duplicate announcements.

See [Setup and Operations](docs/setup.md) for token scopes, desktop build notes, Media Actions details, cost-control settings, security notes, quality gates, and release steps.

See [Release Notes](RELEASE_NOTES.md) for changes in each published version.

## Docs

- [Setup and Operations](docs/setup.md)
- [Command Reference](docs/commands.md)
- [Personality Guide](docs/personality.md)
- [Streamer Knowledge Template](docs/knowledge/example.md)

## Development

Run the core test suite:

```bash
go test ./...
```

Run the broader local quality gates before release work:

```bash
npm --prefix frontend test
npm --prefix frontend run build
go test ./...
go test -race ./...
go vet ./...
npm --prefix frontend audit --audit-level=moderate
govulncheck ./...
```

## Acknowledgments

LupusAria's Twitch bot design is independently implemented in Go, but several concepts were informed by [ChatSage](https://github.com/detekoi/chatsage) by detekoi, including structured chat context, real-time stream context, Gemini-backed Twitch chat behavior, and the `!game` pattern. ChatSage is licensed under AGPL-3.0; this project does not include ChatSage code.
