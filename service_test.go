package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"lupusaria/internal/botrunner"
	"lupusaria/internal/config"
)

func TestSIPBotLifecycleAndLivePreferences(t *testing.T) {
	t.Setenv(envPathOverride, filepath.Join(t.TempDir(), ".env"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("APPDATA", t.TempDir())

	app := NewApp()
	var starts atomic.Int32
	app.runBot = func(ctx context.Context, path string, _ *slog.Logger, options botrunner.Options) error {
		starts.Add(1)
		publish := func() error {
			settings, err := config.LoadPartial(path)
			if err == nil {
				options.FeaturesChanged(map[string]bool{"ask": settings.Bot.EnableAsk})
			}
			return err
		}
		if err := publish(); err != nil {
			return err
		}
		options.Ready()
		for {
			select {
			case <-ctx.Done():
				return nil
			case update := <-options.SettingsUpdates:
				update.Done <- publish()
			}
		}
	}

	server := httptest.NewServer(app.sipServer("service").Handler)
	defer server.Close()
	type response struct {
		State    string       `json:"botState"`
		Success  bool         `json:"success"`
		Features []sipFeature `json:"features"`
		Logs     []serviceLog `json:"logs"`
	}
	call := func(path, payload string, status int) response {
		t.Helper()
		var res *http.Response
		var err error
		if payload == "" {
			res, err = server.Client().Get(server.URL + "/api/v1/" + path)
		} else {
			res, err = server.Client().Post(server.URL+"/api/v1/"+path, "application/json", strings.NewReader(payload))
		}
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("%s returned %d, want %d", path, res.StatusCode, status)
		}
		var out response
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if state := call("status", "", 200).State; state != "idle" {
		t.Fatalf("initial bot state = %q", state)
	}
	for range 2 {
		if state := call("bot/start", "{}", 200).State; state != "running" {
			t.Fatalf("started bot state = %q", state)
		}
	}
	if count := starts.Load(); count != 1 {
		t.Fatalf("duplicate start launched %d runners", count)
	}
	for _, enabled := range []bool{false, true} {
		value, _ := json.Marshal(map[string]any{"id": "ask", "enabled": enabled})
		out := call("features", string(value), 200)
		found := false
		for _, feature := range out.Features {
			if feature.ID == "ask" {
				found = feature.Enabled == enabled && feature.Effective == enabled
			}
		}
		if !found {
			t.Fatalf("ask preference did not apply: %+v", out.Features)
		}
	}
	for range 2 {
		if state := call("bot/stop", "{}", 200).State; state != "idle" {
			t.Fatalf("stopped bot state = %q", state)
		}
	}
	if out := call("logs", "", 200); len(out.Logs) == 0 {
		t.Fatal("service lost logs after stopping the bot")
	}
}
