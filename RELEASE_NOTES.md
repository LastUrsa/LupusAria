# LupusAria Release Notes

## Unreleased

- Store installed-app settings under `%APPDATA%\Starsong Tools\LupusAria` instead of relying on a `.env` beside the executable.
- Add desktop UI fields for Twitch credentials and AI API keys. Saved secret values remain hidden and are only replaced when a new value is entered.
- Support separate Twitch application credentials for ad alerts with `TWITCH_ADS_CLIENT_ID` and `TWITCH_ADS_CLIENT_SECRET`.
- Group announcement settings into separate expandable Timer and Command summary tables in the desktop app.
- Leave the OpenAI-compatible model blank until explicitly configured instead of auto-populating a local model name.
- Use app-rendered select menus in the desktop UI so dropdown colors follow the design palette consistently.

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
- Ursa knowledge base and Lupus Aria personality guidance.

Install notes:

- Windows installer defaults to `C:\Program Files\Starsong Tools\LupusAria`.
- Publisher metadata is `Starsong Tools`.
- Keep `.env` and local token/config state files private and out of source control.
