package config

import (
	"os"
	"path/filepath"
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
AUTOSO_ENABLED=on
AD_ALERT_WARNING_MINUTES=8
AD_ALERT_POLL_SECONDS=45
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
	if !cfg.RecentStreamers.Enabled {
		t.Fatal("autoso should be enabled")
	}
	if cfg.AdAlerts.WarningLead != 8*time.Minute || cfg.AdAlerts.PollInterval != 45*time.Second {
		t.Fatalf("ad alerts = %#v", cfg.AdAlerts)
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
`)

	cfg, err := Load(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Model != "gemini-3.1-flash-lite" {
		t.Fatalf("model = %q", cfg.AI.Model)
	}
	if cfg.AI.InputPricePerMillion != 0.25 || cfg.AI.OutputPricePerMillion != 1.50 {
		t.Fatalf("prices = %f/%f", cfg.AI.InputPricePerMillion, cfg.AI.OutputPricePerMillion)
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

func writeTestEnv(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
