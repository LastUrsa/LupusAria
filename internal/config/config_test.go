package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdWarningLeadPrefersMinutes(t *testing.T) {
	values := map[string]string{
		"AD_ALERT_WARNING_MINUTES": "7",
		"AD_ALERT_WARNING_SECONDS": "300",
	}

	if got := adWarningLead(values); got != 7*time.Minute {
		t.Fatalf("adWarningLead = %s, want 7m", got)
	}
}

func TestAdWarningLeadSupportsLegacySeconds(t *testing.T) {
	values := map[string]string{
		"AD_ALERT_WARNING_SECONDS": "90",
	}

	if got := adWarningLead(values); got != 90*time.Second {
		t.Fatalf("adWarningLead = %s, want 90s", got)
	}
}

func TestAdWarningLeadDefaultsToFiveMinutes(t *testing.T) {
	if got := adWarningLead(nil); got != 5*time.Minute {
		t.Fatalf("adWarningLead = %s, want 5m", got)
	}
}

func TestLoadNormalizesChannelAndParsesFeatureToggles(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=#LastUrsa
ENABLE_MENTION_RESPONSES=off
ENABLE_ASK_COMMAND=no
ENABLE_LURK_COMMAND=0
ENABLE_COMMANDS_COMMAND=false
ENABLE_RESET_COMMAND=true
MENTION_PERMISSION=mods
ASK_COMMAND_PERMISSION=everyone
LURK_COMMAND_PERMISSION=mods
GAME_COMMAND_PERMISSION=everyone
COMMANDS_COMMAND_PERMISSION=everyone
RESET_COMMAND_PERMISSION=broadcaster
ENABLE_EMOTE_CONTEXT=true
EMOTE_CACHE_PATH=.custom-emotes.json
AUTOSO_ENABLED=on
AUTOSO_COMMAND_PERMISSION=mods
SO_ROULETTE_COMMAND_PERMISSION=broadcaster
SO_ROULETTE_STREAMERS=@Alice,bob Alice
AD_ALERT_WARNING_MINUTES=8
AD_ALERT_POLL_SECONDS=45
ANNOUNCEMENTS_ENABLED=true
ANNOUNCEMENTS_PATH=.custom-announcements.json
ANNOUNCEMENT_POLL_SECONDS=20
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Twitch.Channel != "lastursa" {
		t.Fatalf("channel = %q, want lastursa", cfg.Twitch.Channel)
	}
	if cfg.Bot.EnableMentions || cfg.Bot.EnableAsk || cfg.Bot.EnableLurk || cfg.Bot.EnableCommands {
		t.Fatalf("feature toggles = %#v", cfg.Bot)
	}
	if !cfg.Bot.EnableReset {
		t.Fatal("reset should remain enabled")
	}
	if cfg.Bot.MentionPermission != "mods" || cfg.Bot.AskPermission != "everyone" || cfg.Bot.LurkPermission != "mods" || cfg.Bot.GamePermission != "everyone" || cfg.Bot.CommandsPermission != "everyone" || cfg.Bot.ResetPermission != "broadcaster" {
		t.Fatalf("command permissions = %#v", cfg.Bot)
	}
	wantEmoteCachePath := filepath.Join(filepath.Dir(envPath), ".custom-emotes.json")
	if !cfg.Bot.EnableEmoteContext || cfg.Bot.EmoteCachePath != wantEmoteCachePath {
		t.Fatalf("emote context config = enabled %v path %q, want enabled path %q", cfg.Bot.EnableEmoteContext, cfg.Bot.EmoteCachePath, wantEmoteCachePath)
	}
	if !cfg.RecentStreamers.Enabled {
		t.Fatal("autoso should be enabled")
	}
	if cfg.RecentStreamers.Permission != "mods" {
		t.Fatalf("autoso permission = %q", cfg.RecentStreamers.Permission)
	}
	if cfg.RecentStreamers.SORoulettePermission != "broadcaster" {
		t.Fatalf("so roulette permission = %q", cfg.RecentStreamers.SORoulettePermission)
	}
	if got, want := strings.Join(cfg.RecentStreamers.RouletteStreamers, ","), "alice,bob"; got != want {
		t.Fatalf("roulette streamers = %q, want %q", got, want)
	}
	if cfg.RecentStreamers.ShoutoutDelay != 5*time.Second {
		t.Fatalf("default shoutout delay = %s, want 5s", cfg.RecentStreamers.ShoutoutDelay)
	}
	if cfg.AdAlerts.WarningLead != 8*time.Minute || cfg.AdAlerts.PollInterval != 45*time.Second {
		t.Fatalf("ad alerts = %#v", cfg.AdAlerts)
	}
	wantAnnouncementsPath := filepath.Join(filepath.Dir(envPath), ".custom-announcements.json")
	if !cfg.Announcements.Enabled || cfg.Announcements.Path != wantAnnouncementsPath || cfg.Announcements.PollInterval != 20*time.Second {
		t.Fatalf("announcements = %#v", cfg.Announcements)
	}
}

func TestLoadSelectsGeminiModelAndPrices(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
AI_PROVIDER=gemini
GEMINI_API_KEY=test-key
GEMINI_MODEL=gemini-3.1-flash-lite
GEMINI_THINKING_LEVEL=high
AI_MAX_OUTPUT_TOKENS=640
AI_MAX_RETRIES=2
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Model != "gemini-3.1-flash-lite" {
		t.Fatalf("model = %q", cfg.AI.Model)
	}
	if cfg.AI.GeminiThinkingLevel != "high" || cfg.AI.MaxOutputTokens != 640 || cfg.AI.MaxRetries != 2 {
		t.Fatalf("ai controls = %#v", cfg.AI)
	}
	if cfg.AI.InputPricePerMillion != 0.25 || cfg.AI.OutputPricePerMillion != 1.50 {
		t.Fatalf("prices = %f/%f", cfg.AI.InputPricePerMillion, cfg.AI.OutputPricePerMillion)
	}
}

func TestLoadRaisesSubsecondAutoSODelayToMinimum(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS=0
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecentStreamers.ShoutoutDelay != time.Second {
		t.Fatalf("shoutout delay = %s, want 1s", cfg.RecentStreamers.ShoutoutDelay)
	}
}

func TestLoadAllowsTwoSecondAutoSODelay(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS=2
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RecentStreamers.ShoutoutDelay != 2*time.Second {
		t.Fatalf("shoutout delay = %s, want 2s", cfg.RecentStreamers.ShoutoutDelay)
	}
}

func TestLoadUsesLocalOllamaBaseURLWithoutModelDefault(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
AI_PROVIDER=openai-compatible
AI_API_KEY=ollama
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("base url = %q, want local ollama", cfg.AI.BaseURL)
	}
	if cfg.AI.Model != "" {
		t.Fatalf("model = %q, want empty", cfg.AI.Model)
	}
	if cfg.AI.InputPricePerMillion != 0 || cfg.AI.OutputPricePerMillion != 0 {
		t.Fatalf("prices = %f/%f", cfg.AI.InputPricePerMillion, cfg.AI.OutputPricePerMillion)
	}
}

func TestLoadAddsGeminiFallbackForOpenAICompatibleWhenKeyExists(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
AI_PROVIDER=openai-compatible
AI_API_KEY=ollama
GEMINI_API_KEY=gemini-key
GEMINI_MODEL=gemini-3.1-flash-lite
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Fallback == nil {
		t.Fatal("expected Gemini fallback")
	}
	if cfg.AI.Fallback.Provider != "gemini" || cfg.AI.Fallback.APIKey != "gemini-key" || cfg.AI.Fallback.Model != "gemini-3.1-flash-lite" {
		t.Fatalf("fallback = %#v", cfg.AI.Fallback)
	}
}

func TestLoadUsesAIResilienceDefaults(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
AI_PROVIDER=gemini
GEMINI_API_KEY=test-key
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.GeminiThinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want high", cfg.AI.GeminiThinkingLevel)
	}
	if cfg.AI.MaxOutputTokens != 1024 {
		t.Fatalf("max output tokens = %d, want 1024", cfg.AI.MaxOutputTokens)
	}
	if cfg.AI.MaxRetries != 3 {
		t.Fatalf("max retries = %d, want 3", cfg.AI.MaxRetries)
	}
}

func TestLoadReportsMissingRequiredConfig(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `TWITCH_CHANNEL=lastursa`)

	_, err := Load(envPath)
	if err == nil {
		t.Fatal("expected missing config error")
	}
}

func TestLoadPartialAllowsMissingRequiredConfig(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `TWITCH_CHANNEL=lastursa`)

	cfg, err := LoadPartial(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Twitch.Channel != "lastursa" {
		t.Fatalf("channel = %q, want lastursa", cfg.Twitch.Channel)
	}
}

func TestLoadAdsCredentialsFallbackToMainTwitchApp(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
TWITCH_CLIENT_ID=main-client
TWITCH_CLIENT_SECRET=main-secret
TWITCH_ADS_REFRESH_TOKEN=ads-refresh
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Twitch.AdsClientID != "main-client" || cfg.Twitch.AdsClientSecret != "main-secret" {
		t.Fatalf("ads client fallback = %q/%q", cfg.Twitch.AdsClientID, cfg.Twitch.AdsClientSecret)
	}
}

func TestLoadAdsCredentialsCanUseSeparateTwitchApp(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
TWITCH_CLIENT_ID=main-client
TWITCH_CLIENT_SECRET=main-secret
TWITCH_ADS_CLIENT_ID=ads-client
TWITCH_ADS_CLIENT_SECRET=ads-secret
TWITCH_ADS_REFRESH_TOKEN=ads-refresh
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Twitch.AdsClientID != "ads-client" || cfg.Twitch.AdsClientSecret != "ads-secret" {
		t.Fatalf("ads client override = %q/%q", cfg.Twitch.AdsClientID, cfg.Twitch.AdsClientSecret)
	}
}

func TestLoadRequiresClientIDWhenAdAlertsEnabled(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
AD_ALERTS_ENABLED=true
TWITCH_ADS_OAUTH_TOKEN=oauth:ads-token
`)

	_, err := Load(envPath)
	if err == nil {
		t.Fatal("expected missing client ID error")
	}
	if got := err.Error(); !strings.Contains(got, "TWITCH_ADS_CLIENT_ID or TWITCH_CLIENT_ID") {
		t.Fatalf("error = %q", got)
	}
}

func TestLoadResolvesLocalStatePathsBesideEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".lupusaria-twitch-token.json")
	if cfg.Twitch.TokenStatePath != want {
		t.Fatalf("token state path = %q, want %q", cfg.Twitch.TokenStatePath, want)
	}
	wantKnowledge := filepath.Join(dir, ".lupusaria-knowledge.md")
	if cfg.Bot.KnowledgePath != wantKnowledge {
		t.Fatalf("knowledge path = %q, want %q", cfg.Bot.KnowledgePath, wantKnowledge)
	}
	wantChatLog := filepath.Join(dir, ".lupusaria-chat.jsonl")
	if cfg.Bot.ChatLogPath != wantChatLog {
		t.Fatalf("chat log path = %q, want %q", cfg.Bot.ChatLogPath, wantChatLog)
	}
	if !cfg.Bot.SnapshotCrop.Enabled {
		t.Fatal("snapshot crop should default enabled")
	}
	if cfg.Bot.SnapshotCrop.X != 0.255 || cfg.Bot.SnapshotCrop.Y != 0.085 || cfg.Bot.SnapshotCrop.Width != 0.73 || cfg.Bot.SnapshotCrop.Height != 0.73 {
		t.Fatalf("snapshot crop defaults = %#v", cfg.Bot.SnapshotCrop)
	}
}

func TestLoadParsesStreamerIdentity(t *testing.T) {
	envPath := filepath.Join(t.TempDir(), ".env")
	writeTestEnv(t, envPath, `
TWITCH_BOT_USERNAME=LupusAria
TWITCH_OAUTH_TOKEN=oauth:test
TWITCH_CHANNEL=lastursa
STREAMER_NAME=Ursa Starsong
STREAMER_PRONOUNS=he/him
BOT_KNOWLEDGE_PATH=custom-knowledge.md
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Bot.StreamerName != "Ursa Starsong" || cfg.Bot.StreamerPronouns != "he/him" {
		t.Fatalf("streamer identity = %q/%q", cfg.Bot.StreamerName, cfg.Bot.StreamerPronouns)
	}
	if want := filepath.Join(filepath.Dir(envPath), "custom-knowledge.md"); cfg.Bot.KnowledgePath != want {
		t.Fatalf("knowledge path = %q, want %q", cfg.Bot.KnowledgePath, want)
	}
}

func writeTestEnv(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
