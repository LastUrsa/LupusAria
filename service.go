package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const sipAddress = "127.0.0.1:47050"

//go:embed wails.json
var buildConfig []byte

type serviceLog struct {
	At        time.Time `json:"at"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	RequestID string    `json:"requestId,omitempty"`
}

func (a *App) recordServiceLog(level, message, requestID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.serviceLogs = append(a.serviceLogs, serviceLog{time.Now().UTC(), level, message, requestID})
	if len(a.serviceLogs) > 100 {
		a.serviceLogs = a.serviceLogs[len(a.serviceLogs)-100:]
	}
}

type serviceLogHandler struct {
	slog.Handler
	app *App
}

func (h serviceLogHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		message := "Bot reported a runtime warning. See LupusAria diagnostics for details."
		switch {
		case strings.Contains(r.Message, "reconnect"):
			message = "A Twitch connection was interrupted. Check connection status."
		case strings.Contains(r.Message, "raffle"):
			message = "Raffle verification or processing encountered a problem."
		case strings.Contains(r.Message, "ad alert") || strings.Contains(r.Message, "ad break"):
			message = "Ad alerts encountered a Twitch permissions or schedule problem."
		case strings.Contains(r.Message, "announcement"):
			message = "An announcement could not be loaded or sent."
		}
		level := "warn"
		if r.Level >= slog.LevelError {
			level = "error"
		}
		h.app.recordServiceLog(level, message, "")
	}
	return h.Handler.Handle(ctx, r)
}

func recordServiceStartup(message string) {
	path, err := appEnvPath()
	if err != nil {
		return
	}
	dir := filepath.Join(filepath.Dir(path), "logs")
	if os.MkdirAll(dir, 0700) != nil {
		return
	}
	path = filepath.Join(dir, "sip-startup.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 128<<10 {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err == nil {
		defer f.Close()
		_, _ = io.WriteString(f, time.Now().UTC().Format(time.RFC3339)+" "+message+"\n")
	}
}

func runService(app *App, listener net.Listener) {
	recordServiceStartup("LupusAria service starting.")
	app.recordServiceLog("info", "LupusAria service starting.", "")
	app.initialize()
	server := app.sipServer("service")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		recordServiceStartup("LupusAria SIP listener failed.")
	}
	shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	app.shutdown(shutdown)
}

type sipFeature struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Enabled   bool   `json:"enabled"`
	Effective bool   `json:"effective"`
}

var featureControls = []struct {
	id, label, key string
	value          func(ControlSettings) bool
}{
	{"mentions", "Mention responses", "ENABLE_MENTION_RESPONSES", func(s ControlSettings) bool { return s.EnableMentions }},
	{"ask", "!ask", "ENABLE_ASK_COMMAND", func(s ControlSettings) bool { return s.EnableAsk }},
	{"lurk", "!lurk", "ENABLE_LURK_COMMAND", func(s ControlSettings) bool { return s.EnableLurk }},
	{"commands", "!commands", "ENABLE_COMMANDS_COMMAND", func(s ControlSettings) bool { return s.EnableCommands }},
	{"reset", "!reset", "ENABLE_RESET_COMMAND", func(s ControlSettings) bool { return s.EnableReset }},
	{"auto-so", "AutoSO", "AUTOSO_ENABLED", func(s ControlSettings) bool { return s.AutosoEnabled }},
	{"announcements", "Announcements", "ANNOUNCEMENTS_ENABLED", func(s ControlSettings) bool { return s.AnnouncementsEnabled }},
	{"ad-alerts", "Ad alerts", "AD_ALERTS_ENABLED", func(s ControlSettings) bool { return s.AdAlertsEnabled }},
	{"raffle", "Raffle", "RAFFLE_ENABLED", func(s ControlSettings) bool { return s.RaffleEnabled }},
	{"screenshot-crop", "Screenshot crop", "GAME_SNAPSHOT_CROP_ENABLED", func(s ControlSettings) bool { return s.GameSnapshotCropEnabled }},
}

func (a *App) sipFeatures() ([]sipFeature, error) {
	settings, err := a.GetSettings()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	features := make([]sipFeature, 0, len(featureControls))
	for _, f := range featureControls {
		features = append(features, sipFeature{f.id, f.label, f.value(settings), a.botReady && !a.botStopping && a.effectiveFeatures[f.id]})
	}
	return features, nil
}

func (a *App) botStatus() (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case a.botStopping:
		return "stopping", ""
	case a.botReady:
		return "running", ""
	case a.running:
		return "starting", ""
	case a.lastError != "":
		return "failed", safeBotError(a.lastError)
	default:
		return "idle", ""
	}
}

func safeBotError(raw string) string {
	switch {
	case strings.Contains(raw, "load config"):
		return "Bot configuration is incomplete or invalid. Check LupusAria Setup."
	case strings.Contains(raw, "initialize ai"):
		return "The AI provider could not initialize. Check LupusAria AI settings."
	default:
		return "The bot could not connect or stopped unexpectedly. Check Twitch credentials and connection."
	}
}

func (a *App) awaitBot(ctx context.Context, start bool) (string, string) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, message := a.botStatus()
		if start && (state == "running" || state == "failed" || state == "idle") || !start && (state == "idle" || state == "failed") {
			return state, message
		}
		select {
		case <-ctx.Done():
			return state, "The bot is still transitioning. Check its current status before retrying."
		case <-ticker.C:
		}
	}
}

func randomSIPID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var sipRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

func (a *App) sipServer(mode string) *http.Server {
	var metadata struct {
		Info struct{ ProductVersion string }
	}
	_ = json.Unmarshal(buildConfig, &metadata)
	sessionID := randomSIPID()
	startedAt := time.Now().UTC()
	capabilities := []string{"bot-lifecycle", "bot-features", "recent-logs", "diagnostics"}
	return &http.Server{
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      32 * time.Second,
		IdleTimeout:       30 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			rid := r.Header.Get("X-Request-ID")
			if rid == "" {
				rid = randomSIPID()
			}
			w.Header().Set("X-Request-ID", rid)
			send := func(status int, body map[string]any) {
				body["requestId"], body["sessionId"], body["observedAt"] = rid, sessionID, time.Now().UTC()
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(body)
			}
			fail := func(status int, code, message string) {
				send(status, map[string]any{"success": false, "errorCode": code, "message": message, "error": message})
			}
			host, _, _ := net.SplitHostPort(r.Host)
			if (host != "127.0.0.1" && host != "localhost") || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(http.StatusForbidden, "FORBIDDEN", "SIP is available to local app clients only.")
				return
			}
			if !sipRequestID.MatchString(rid) {
				fail(http.StatusBadRequest, "INVALID_REQUEST", "Invalid request ID.")
				return
			}
			if r.Method == http.MethodGet {
				switch r.URL.Path {
				case "/api/v1/app":
					send(200, map[string]any{"appId": "lupusaria", "name": "LupusAria", "mode": mode, "version": metadata.Info.ProductVersion, "protocolVersion": 1, "capabilities": capabilities})
				case "/api/v1/health":
					send(200, map[string]any{"status": "ready"})
				case "/api/v1/capabilities":
					send(200, map[string]any{"protocolVersion": 1, "capabilities": capabilities, "supportsStatusReporting": true})
				case "/api/v1/status", "/api/v1/diagnostics":
					state, message := a.botStatus()
					send(200, map[string]any{"botState": state, "botError": message, "startedAt": startedAt, "version": metadata.Info.ProductVersion, "protocolVersion": 1})
				case "/api/v1/features":
					a.settingsMu.Lock()
					features, err := a.sipFeatures()
					a.settingsMu.Unlock()
					if err != nil {
						fail(503, "UNAVAILABLE", "LupusAria settings could not be read.")
						return
					}
					send(200, map[string]any{"features": features})
				case "/api/v1/logs":
					a.mu.Lock()
					logs := append([]serviceLog{}, a.serviceLogs...)
					a.mu.Unlock()
					send(200, map[string]any{"logs": logs})
				default:
					fail(404, "NOT_FOUND", "Unknown SIP route.")
				}
				return
			}
			if r.Method != http.MethodPost {
				fail(405, "INVALID_REQUEST", "Unsupported SIP method.")
				return
			}
			contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if contentType != "application/json" {
				fail(415, "INVALID_REQUEST", "Use application/json for SIP actions.")
				return
			}
			var input struct {
				ID      string `json:"id"`
				Enabled *bool  `json:"enabled"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
				fail(400, "INVALID_REQUEST", "Invalid action payload.")
				return
			}
			since := time.Now().UTC()
			opID := randomSIPID()
			finish := func(status int, code, message string, body map[string]any) {
				outcome, level := "success", "info"
				if code != "" {
					outcome, level = "failed", "error"
				}
				body["success"], body["errorCode"], body["message"] = code == "", code, message
				if code != "" {
					body["error"] = message
				}
				body["operationId"] = opID
				done := time.Now().UTC()
				body["operation"] = map[string]any{"id": opID, "kind": r.URL.Path, "requestId": rid, "sessionId": sessionID, "startedAt": since, "updatedAt": done, "completedAt": done, "phase": "completed", "outcome": outcome, "errorCode": code, "message": message}
				a.recordServiceLog(level, message, rid)
				send(status, body)
			}
			switch r.URL.Path {
			case "/api/v1/bot/start", "/api/v1/bot/stop":
				if input.ID != "" || input.Enabled != nil {
					fail(400, "INVALID_REQUEST", "Lifecycle actions take an empty object.")
					return
				}
				start := r.URL.Path == "/api/v1/bot/start"
				state, _ := a.botStatus()
				if start && state == "stopping" {
					finish(409, "BUSY", "The bot is stopping. Try again once it is idle.", map[string]any{"botState": state})
					return
				}
				if start {
					_ = a.StartBot()
				} else {
					_ = a.StopBot()
				}
				ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
				defer cancel()
				state, message := a.awaitBot(ctx, start)
				body := map[string]any{"botState": state}
				switch {
				case ctx.Err() != nil:
					finish(504, "TIMEOUT", message, body)
				case start && state != "running":
					if message == "" {
						message = "The bot stopped before chat became ready."
					}
					finish(503, "UNAVAILABLE", message, body)
				case start:
					finish(200, "", "Bot running.", body)
				default:
					finish(200, "", "Bot stopped.", body)
				}
			case "/api/v1/features":
				for _, f := range featureControls {
					if input.ID != f.id || input.Enabled == nil {
						continue
					}
					a.settingsMu.Lock()
					path, err := appEnvPath()
					if err == nil {
						err = updateEnvFile(path, map[string]string{f.key: boolString(*input.Enabled)})
					}
					if err != nil {
						a.settingsMu.Unlock()
						finish(503, "UNAVAILABLE", "Feature preference could not be saved.", map[string]any{})
						return
					}
					applyErr := a.applyLiveSettings()
					features, readErr := a.sipFeatures()
					a.settingsMu.Unlock()
					state, _ := a.botStatus()
					body := map[string]any{"features": features, "botState": state}
					if readErr != nil {
						finish(503, "UNAVAILABLE", "Preference saved. Current settings could not be read.", body)
						return
					}
					for _, feature := range features {
						if feature.ID != f.id {
							continue
						}
						if feature.Enabled != *input.Enabled {
							finish(409, "VALIDATION_FAILED", f.label+" is overridden by the process environment.", body)
							return
						}
						adRemovalFailed := f.id == "ad-alerts" && applyErr != nil &&
							strings.Contains(applyErr.Error(), "Twitch subscription removal failed")
						if state == "running" && (feature.Effective != feature.Enabled || adRemovalFailed) {
							finish(503, "UNAVAILABLE", f.label+" was saved. A live update failed. Check LupusAria setup and Twitch permissions.", body)
							return
						}
					}
					finish(200, "", f.label+" preference saved.", body)
					return
				}
				fail(400, "INVALID_REQUEST", "Choose a supported feature and a boolean value.")
			default:
				fail(404, "NOT_FOUND", "Unknown SIP route.")
			}
		}),
	}
}
