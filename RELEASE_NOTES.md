# LupusAria Release Notes

## v0.2.0 - 2026-06-18

- Moves installed-app settings to `%APPDATA%\Starsong Tools\LupusAria` and keeps saved secrets hidden unless replaced.
- Adds desktop setup for Twitch credentials, AI keys, streamer identity, local knowledge, and split ad-alert Twitch credentials.
- Improves announcement management with separate expandable Timer and Command tables.
- Tightens AI chat behavior with structured recent chat, stream-aware `!lurk` replies, fixed Lupus identity, and leaner shared prompts.
- Adds first-run streamer knowledge setup using a local editable file created from a generic public template.
- Leaves OpenAI-compatible models blank until explicitly configured and keeps dropdown styling consistent in the desktop UI.

## v0.1.0

Initial local-first LupusAria release.

Highlights:

- Twitch chat bot with AI replies for mentions, `!ask`, and `!lurk`.
- Gemini, OpenAI-compatible, and mock AI providers.
- Local Wails control panel for runtime controls and non-secret settings.
- AutoSO tracking for eligible recent streamers.
- AI-powered ad alerts with configured fallback messages.
- Static command announcements and stream-timer announcements.
- Local cost, cooldown, and request guardrails.
- Streamer knowledge base and Lupus Aria personality guidance.

Install notes:

- Windows installer defaults to `C:\Program Files\Starsong Tools\LupusAria`.
- Publisher metadata is `Starsong Tools`.
- Keep `.env` and local token/config state files private and out of source control.
