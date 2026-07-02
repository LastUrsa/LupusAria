package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lupusaria/internal/mediaactions"
	"lupusaria/internal/twitch"
)

func TestUpdateEnvFileUpdatesPreservesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	initial := strings.Join([]string{
		"# existing comment",
		"TWITCH_CHANNEL=oldchannel",
		"UNRELATED=value",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	err := updateEnvFile(path, map[string]string{
		"TWITCH_CHANNEL":           "new channel",
		"AD_ALERT_WARNING_MESSAGE": "Heads up soon.",
	})
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"# existing comment",
		"TWITCH_CHANNEL=\"new channel\"",
		"UNRELATED=value",
		"AD_ALERT_WARNING_MESSAGE=\"Heads up soon.\"",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("updated env missing %q:\n%s", want, got)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestUpdateEnvFileCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".env")

	if err := updateEnvFile(path, map[string]string{"TWITCH_CHANNEL": "lastursa"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != "TWITCH_CHANNEL=lastursa\n" {
		t.Fatalf("env = %q", got)
	}
}

func TestAppEnvPathUsesOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv(envPathOverride, path)

	got, err := appEnvPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("appEnvPath = %q, want %q", got, path)
	}
}

func TestGetSettingsWorksBeforeEnvExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv(envPathOverride, path)

	settings, err := NewApp().GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ConfigPath != path {
		t.Fatalf("config path = %q, want %q", settings.ConfigPath, path)
	}
	if settings.Channel != "" || settings.BotUsername != "" {
		t.Fatalf("first-run twitch settings should be empty: %#v", settings)
	}
	if settings.KnowledgePath == "" || !settings.KnowledgeExists {
		t.Fatalf("first-run knowledge should be created: %#v", settings)
	}
	if !settings.GameSnapshotCropEnabled || settings.GameSnapshotCropX != 0.255 || settings.GameSnapshotCropY != 0.085 || settings.GameSnapshotCropWidth != 0.73 || settings.GameSnapshotCropHeight != 0.73 {
		t.Fatalf("first-run snapshot crop defaults = %#v", settings)
	}
}

func TestCheckTwitchPermissionsReportsMissingFirstRunCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv(envPathOverride, path)

	check, err := NewApp().CheckTwitchPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if check.Overall != "error" {
		t.Fatalf("overall = %q, want error", check.Overall)
	}
	if len(check.Items) != 3 {
		t.Fatalf("items = %#v", check.Items)
	}
	if check.Items[0].Name != "Twitch app" || check.Items[0].Status != "error" {
		t.Fatalf("app item = %#v", check.Items[0])
	}
	if check.Items[1].Name != "Bot token" || check.Items[1].Status != "error" {
		t.Fatalf("bot item = %#v", check.Items[1])
	}
	if check.Items[2].Name != "Broadcaster token" || check.Items[2].Status != "warning" {
		t.Fatalf("broadcaster item = %#v", check.Items[2])
	}
}

func TestPermissionStatusAndMissingScopes(t *testing.T) {
	missing := missingScopes([]string{"user:read:chat", "user:bot"}, []string{"user:read:chat", "user:write:chat", "user:bot"})
	if got, want := strings.Join(missing, ","), "user:write:chat"; got != want {
		t.Fatalf("missing scopes = %q, want %q", got, want)
	}

	overall := overallPermissionStatus([]TwitchPermissionItem{
		{Name: "ok", Status: "ok"},
		{Name: "warning", Status: "warning"},
	})
	if overall != "warning" {
		t.Fatalf("overall = %q, want warning", overall)
	}
	overall = overallPermissionStatus([]TwitchPermissionItem{
		{Name: "ok", Status: "ok"},
		{Name: "error", Status: "error"},
		{Name: "warning", Status: "warning"},
	})
	if overall != "error" {
		t.Fatalf("overall = %q, want error", overall)
	}
}

func TestSaveSettingsWritesProvidedSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv(envPathOverride, path)

	app := NewApp()
	settings, err := app.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Channel = "lastursa"
	settings.BotUsername = "LupusAria"
	settings.StreamerName = "Ursa Starsong"
	settings.StreamerPronouns = "he/him"
	settings.TwitchClientID = "client-id"
	settings.TwitchClientSecret = "client-secret"
	settings.TwitchAdsClientID = "ads-client-id"
	settings.TwitchAdsClientSecret = "ads-client-secret"
	settings.TwitchRefreshToken = "refresh-token"
	settings.AIProvider = "gemini"
	settings.AIModel = "llama3.1:8b"
	settings.GeminiAPIKey = "gemini-key"
	settings.GameSnapshotCropEnabled = false
	settings.GameSnapshotCropX = 0.25
	settings.GameSnapshotCropY = 0.1
	settings.GameSnapshotCropWidth = 0.7
	settings.GameSnapshotCropHeight = 0.75
	settings.RecentStreamerDelay = 2

	if err := app.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	for _, want := range []string{
		"TWITCH_CHANNEL=lastursa",
		"TWITCH_BOT_USERNAME=LupusAria",
		"STREAMER_NAME=\"Ursa Starsong\"",
		"STREAMER_PRONOUNS=he/him",
		"TWITCH_CLIENT_ID=client-id",
		"TWITCH_CLIENT_SECRET=client-secret",
		"TWITCH_ADS_CLIENT_ID=ads-client-id",
		"TWITCH_ADS_CLIENT_SECRET=ads-client-secret",
		"TWITCH_REFRESH_TOKEN=refresh-token",
		"AI_PROVIDER=gemini",
		"AI_BASE_URL=",
		"AI_MODEL=llama3.1:8b",
		"AI_FALLBACK_PROVIDER=",
		"GEMINI_API_KEY=gemini-key",
		"GAME_SNAPSHOT_CROP_ENABLED=false",
		"GAME_SNAPSHOT_CROP_X=0.25",
		"GAME_SNAPSHOT_CROP_Y=0.1",
		"GAME_SNAPSHOT_CROP_WIDTH=0.7",
		"GAME_SNAPSHOT_CROP_HEIGHT=0.75",
		"RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS=2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("saved env missing %q:\n%s", want, got)
		}
	}
}

func TestKnowledgeLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	t.Setenv(envPathOverride, path)

	app := NewApp()
	settings, err := app.GetKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Exists || !strings.Contains(settings.Content, "Streamer Identity") {
		t.Fatalf("default knowledge = %#v", settings)
	}

	settings.Content = "# Custom\n\n## Identity\nTags: streamer\n\n- The streamer is Test.\n"
	if err := app.SaveKnowledge(settings); err != nil {
		t.Fatal(err)
	}
	saved, err := app.GetKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved.Content, "The streamer is Test.") {
		t.Fatalf("saved knowledge = %#v", saved)
	}

	reset, err := app.ResetKnowledgeTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reset.Content, "Streamer Identity") {
		t.Fatalf("reset knowledge = %#v", reset)
	}
}

func TestSaveKnowledgeIgnoresFrontendPath(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	t.Setenv(envPathOverride, envPath)

	app := NewApp()
	settings, err := app.GetKnowledge()
	if err != nil {
		t.Fatal(err)
	}
	defaultPath := settings.Path
	otherPath := filepath.Join(dir, "other.md")
	settings.Path = otherPath
	settings.Content = "# Custom\n\n## Identity\nTags: streamer\n\n- The streamer is Test.\n"
	if err := app.SaveKnowledge(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
		t.Fatalf("SaveKnowledge wrote frontend-provided path, stat err = %v", err)
	}
	raw, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "The streamer is Test.") {
		t.Fatalf("default knowledge path was not updated:\n%s", string(raw))
	}
	envRaw, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(envRaw), "BOT_KNOWLEDGE_PATH") {
		t.Fatalf("SaveKnowledge should not persist knowledge path override:\n%s", string(envRaw))
	}
}

func TestGetLogsReturnsEmptyAndDefensiveCopy(t *testing.T) {
	app := NewApp()
	if logs := app.GetLogs(); logs == nil || len(logs) != 0 {
		t.Fatalf("empty logs = %#v", logs)
	}

	app.appendLog(" first ")
	logs := app.GetLogs()
	if len(logs) != 1 || !strings.Contains(logs[0], "first") {
		t.Fatalf("logs = %#v", logs)
	}
	logs[0] = "mutated"
	if got := app.GetLogs()[0]; got == "mutated" {
		t.Fatal("GetLogs should return a defensive copy")
	}
}

func TestAppendLogCapsHistory(t *testing.T) {
	app := NewApp()
	for i := 0; i < 205; i++ {
		app.appendLog("line")
	}
	if got := len(app.GetLogs()); got != 200 {
		t.Fatalf("log count = %d, want 200", got)
	}
}

func TestDefaultMediaOverlayAddressIsStable(t *testing.T) {
	if got, want := defaultMediaOverlayAddress(), "127.0.0.1:47831"; got != want {
		t.Fatalf("defaultMediaOverlayAddress = %q, want %q", got, want)
	}
}

func TestOverlayServerBroadcastsPlaybackEvents(t *testing.T) {
	server, err := newOverlayServerAtAddress("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())

	req, err := http.NewRequest(http.MethodGet, server.URL()+"events", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	waitForLine(t, lines, ": connected")
	server.Broadcast(MediaActionPlayback{
		ActionID: "action-1",
		Name:     "Alert",
		Duration: 1,
	})
	waitForLine(t, lines, "event: playback")
	data := waitForLinePrefix(t, lines, "data: ")
	var playback MediaActionPlayback
	if err := json.Unmarshal([]byte(strings.TrimPrefix(data, "data: ")), &playback); err != nil {
		t.Fatal(err)
	}
	if playback.ActionID != "action-1" || playback.Name != "Alert" {
		t.Fatalf("playback = %#v", playback)
	}
}

func TestMediaActionRedeemHandlerMatchesRewardTitleFallback(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	root := filepath.Join(configHome, "Starsong Tools", "LupusAria", "MediaActions")
	assetPath := filepath.Join(root, "Its-A-Gundam", "Sounds", "beam.mp3")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("fake mp3 data"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := NewApp()
	handler := app.mediaActionRedeemHandler(ctx, []mediaactions.Action{{
		ID:          "action-1",
		Name:        "Its A Gundam",
		Enabled:     true,
		Trigger:     mediaactions.TriggerChannelPointRedeem,
		RewardID:    "saved-reward-id",
		RewardTitle: "ITS A GUNDAM!",
		Sounds: []mediaactions.Asset{{
			ID:       "asset-1",
			Filename: "beam.mp3",
			Path:     assetPath,
		}},
		Duration: 1,
	}})
	if handler == nil {
		t.Fatal("handler is nil")
	}

	handler(ctx, twitch.ChannelPointRedeemEvent{
		RewardID:    "different-reward-id",
		RewardTitle: "its a gundam!",
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(strings.Join(app.GetLogs(), "\n"), `queued media action "Its A Gundam"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("media action was not queued, logs = %#v", app.GetLogs())
}

func TestMediaActionRedeemHandlerBroadcastsToOverlay(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	root := filepath.Join(configHome, "Starsong Tools", "LupusAria", "MediaActions")
	assetPath := filepath.Join(root, "Reward", "Sounds", "beam.mp3")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("fake mp3 data"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := make(chan []byte, 1)
	app := NewApp()
	app.overlay = &overlayServer{clients: map[chan []byte]bool{client: true}}
	handler := app.mediaActionRedeemHandler(ctx, []mediaactions.Action{{
		ID:       "action-1",
		Name:     "Reward",
		Enabled:  true,
		Trigger:  mediaactions.TriggerChannelPointRedeem,
		RewardID: "reward-1",
		Sounds: []mediaactions.Asset{{
			ID:       "asset-1",
			Filename: "beam.mp3",
			Path:     assetPath,
		}},
		Duration: 1,
	}})
	if handler == nil {
		t.Fatal("handler is nil")
	}

	handler(ctx, twitch.ChannelPointRedeemEvent{RewardID: "reward-1", RewardTitle: "Reward"})
	select {
	case raw := <-client:
		var playback MediaActionPlayback
		if err := json.Unmarshal(raw, &playback); err != nil {
			t.Fatal(err)
		}
		if playback.ActionID != "action-1" || playback.SoundDataURL == "" {
			t.Fatalf("playback = %#v", playback)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for overlay playback, logs = %#v", app.GetLogs())
	}
}

func TestDisplayMinutesRoundsUp(t *testing.T) {
	tests := map[time.Duration]int{
		0:                           0,
		10 * time.Second:            1,
		time.Minute:                 1,
		time.Minute + time.Second:   2,
		5*time.Minute + time.Second: 6,
	}
	for duration, want := range tests {
		if got := displayMinutes(duration); got != want {
			t.Fatalf("displayMinutes(%s) = %d, want %d", duration, got, want)
		}
	}
}

func waitForLine(t *testing.T, lines <-chan string, want string) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before %q", want)
			}
			if line == want {
				return line
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func waitForLinePrefix(t *testing.T, lines <-chan string, prefix string) string {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before prefix %q", prefix)
			}
			if strings.HasPrefix(line, prefix) {
				return line
			}
		case <-deadline:
			t.Fatalf("timed out waiting for prefix %q", prefix)
		}
	}
}

func TestEncodeEnvValue(t *testing.T) {
	tests := map[string]string{
		"":               "",
		"simple":         "simple",
		"two words":      `"two words"`,
		"has#comment":    `"has#comment"`,
		" spaced value ": `"spaced value"`,
	}
	for input, want := range tests {
		if got := encodeEnvValue(input); got != want {
			t.Fatalf("encodeEnvValue(%q) = %q, want %q", input, got, want)
		}
	}
}
