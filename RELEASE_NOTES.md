# LupusAria Release Notes

## v0.6.1

- Suppresses near-duplicate ad-start alerts when Twitch schedule polling and EventSub both report the same ad break.
- Refines LupusAria's voice guidance to keep harmless wolf flavor while avoiding the specific textual `awoo`.
- Strips `awoo` from generated chat replies as an extra cleanup guard.
- Moves longer setup, token, security, quality-gate, and release-process documentation into `docs/setup.md` while keeping the README focused.

## v0.6.0

- Adds channel point Media Actions for random local images, GIFs, and sounds triggered by Twitch custom rewards.
- Adds a transparent OBS browser overlay at `http://127.0.0.1:47831/` for live redeem playback without taking over the desktop app.
- Adds per-GIF playback controls for normal playback, audio-matched slow playback, same-GIF looping, and shuffled loop-to-another-GIF rotation.
- Adds GIF duration detection, per-GIF rotation inclusion, and no-repeat shuffled rotation before eligible GIFs are reused.
- Splits Twitch EventSub WebSocket sessions so bot chat subscriptions and broadcaster redeem/ad subscriptions use the correct Twitch user tokens.
- Adds broadcaster-token permission checks for `channel:read:redemptions`, plus custom reward loading through Helix.
- Raises the default AI output-token limit, logs provider finish reasons, retries max-token finishes, and includes configured command announcements in AI context.
- Synthesizes ad start and expected-end alerts from schedule polling when Twitch advances to the next scheduled ad before EventSub reports the break.
- Documents Media Actions setup, OBS overlay setup, required Twitch scopes, local media security notes, and updated command/ad behavior.

## v0.5.0

- Migrates Twitch chat from IRC-first handling to EventSub WebSockets with an IRC fallback for incomplete setup.
- Sends Twitch chat messages through Helix Send Chat Message, using an app access token for the Twitch Chat Bot Badge path when available.
- Adds EventSub chat badge and reply metadata support for permission checks, reply cleanup, and richer chat context.
- Adds EventSub `channel.ad_break.begin` handling so ad-start alerts can use live Twitch ad events while schedule polling still provides warnings and fallback starts.
- Adds a Setup tab Twitch permissions check for saved app, bot, and ads credentials.
- Refreshes Twitch app and ads token state handling, documents the EventSub/chat badge setup, and expands tests around Twitch auth, Helix, EventSub, ad alerts, and permission reporting.
- Tunes LupusAria's personality prompts and reply cleanup to reduce repeated names, incomplete endings, and overly technical phrasing.
- Lowers the AutoSO and SO roulette shoutout delay floor from five seconds to one second so shorter saved delays persist.
- Cleans up the sticky save bar and adds an in-place save toast so saving settings is easier to confirm without scrolling.

## v0.4.0

- Adds Twitch emote context enrichment with native emote catalog lookup, image-based descriptions, and a local emote cache.
- Adds `!soroulette` for configurable shoutout roulette pools, sharing shoutout dispatch and per-stream duplicate protection with AutoSO.
- Expands AutoSO with follower eligibility checks, configurable command permissions, safer shoutout pacing, and stream-run state reset.
- Adds configurable permission tiers for public commands and per-command announcement permissions in the desktop app.
- Improves AI command handling with a small in-memory request queue, filtered room context, and clearer command safety boundaries.
- Updates docs and example settings for emote context, shoutout roulette, follower scopes, command permissions, and the five-second shoutout delay floor.

## v0.3.0

- Adds grounded `!game` help with Twitch thumbnail snapshot analysis, optional stream-preview crop controls, and fallback game search context.
- Expands the personality evaluator with scenario suites for grounded game replies, style checks, and safer regression coverage.
- Improves Gemini/OpenAI-compatible response handling and keeps game/image prompts focused on observed stream evidence.
- Makes ad alerts resilient to temporary Twitch ad schedule polling failures instead of disabling alerts for the rest of the session.
- Refreshes Twitch ads access tokens during long streams and retries ad schedule polling once after an unauthorized response.
- Adds clearer ad-alert startup, send failure, and configuration logging, plus docs for long-run ads token refresh.

## v0.2.1

- Adds the LupusAria app icon to the desktop build, Windows executable metadata, and in-app brand lockup.
- Reorganizes the desktop control panel into Overview, Setup, AI & Budget, Features, Knowledge, and Activity.
- Moves editable setup fields out of Overview so the landing page focuses on runtime status and recent activity.
- Consolidates chat, AutoSO, ad alerts, and announcements into full-width collapsible feature panels.
- Keeps installed config and knowledge paths internal to the app while preserving local knowledge editing.

## v0.2.0

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
