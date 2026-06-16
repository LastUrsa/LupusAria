package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	path := filepath.Join(t.TempDir(), ".env")

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
