# LupusAria

LupusAria is a local-first AI-powered Twitch chat bot written in Go, with a Wails desktop control panel for day-to-day setup and control.

It is intended to be usable from this public repo by streamers who want a local bot they can inspect and run themselves. You provide your own Twitch application credentials, Twitch tokens, and AI source; LupusAria does not include hosted AI access, shared Twitch credentials, or managed infrastructure.

## Features

- Twitch chat over EventSub WebSockets, Twitch API sends, and IRC fallback for incomplete setup.
- AI replies for direct mentions, `!ask`, `!lurk`, and grounded `!game` help.
- Structured rolling chat context, cached Twitch stream context, local transcript logging, and editable streamer knowledge.
- Twitch emote context enrichment with local cached visual descriptions.
- AutoSO tracking from chatters, watch time, recent stream history, and configurable `!soroulette` pools.
- Configurable command announcements, stream-timer announcements, ad alerts, and channel point Media Actions for OBS overlays.
- Global, per-user, hourly, daily, and monthly AI guardrails.
- Gemini, local Ollama/OpenAI-compatible, and mock AI providers.
- Local Wails control panel for setup, secrets entry, feature configuration, knowledge editing, media actions, and runtime controls.

## Quick Start

1. Copy `.env.example` to `.env`.
2. Fill in your Twitch bot username, OAuth token, channel, and app credentials.
3. Set `STREAMER_NAME`, `STREAMER_PRONOUNS`, and start with `AI_PROVIDER=mock`.
4. Run the bot:

```bash
go run ./cmd/lupusaria
```

Switch to `AI_PROVIDER=gemini` when you are ready to use hosted real AI replies. Use `AI_PROVIDER=openai-compatible` for local Ollama experiments and set `AI_MODEL` to the local model you want.

On first run, LupusAria creates `.lupusaria-knowledge.md` from a neutral template if the file does not exist. Edit it directly, or use the desktop app's Knowledge tab, to add stable channel facts such as streamer identity, pronouns, recurring chat references, project links, and boundaries.

## Desktop App

Run the local control panel:

```bash
/home/don/go/bin/wails dev
```

Build the executable:

```bash
/home/don/go/bin/wails build
```

Windows releases are built through GitHub Actions. The installer uses publisher `Starsong Tools`, installs to `%ProgramFiles%\Starsong Tools\LupusAria`, and stores user settings under:

```text
%APPDATA%\Starsong Tools\LupusAria\.env
```

The app can start and stop the bot, manage account setup and saved secrets, edit AI and budget settings, configure command permissions, tune `!game` snapshot crop, maintain streamer knowledge, configure announcements, and manage Media Actions.

## Key Concepts

- **Bring your own accounts:** you need your own Twitch bot or broadcaster account, Twitch application credentials, scoped Twitch tokens, and either a Gemini key or local OpenAI-compatible endpoint.
- **AI cost controls:** AI calls only happen for enabled AI behaviors and are guarded by cooldowns, request limits, and optional daily/monthly budget caps.
- **Local state:** `.env`, token states, budgets, announcements, media assets, emote caches, knowledge, and chat transcripts are local and gitignored.
- **Media Actions:** channel point redeems can play random local images, GIFs, and sounds through the local OBS Browser Source at `http://127.0.0.1:47831/`.
- **Ad alerts:** Twitch ad schedule polling provides warnings; EventSub ad-break events provide live starts when available; schedule polling can synthesize starts and expected ends as a fallback.

See [Setup and Operations](docs/setup.md) for token scopes, desktop build notes, Media Actions details, cost-control settings, security notes, quality gates, and release steps.

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
npm --prefix frontend run build
go test ./...
go test -race ./...
go vet ./...
npm --prefix frontend audit --audit-level=moderate
govulncheck ./...
```

## Acknowledgments

LupusAria's Twitch bot design is independently implemented in Go, but several concepts were informed by [ChatSage](https://github.com/detekoi/chatsage) by detekoi, including structured chat context, real-time stream context, Gemini-backed Twitch chat behavior, and the `!game` pattern. ChatSage is licensed under AGPL-3.0; this project does not include ChatSage code.
