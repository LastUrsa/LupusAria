package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/botrunner"
	"lupusaria/internal/config"
)

const envPath = ".env"

type App struct {
	ctx context.Context

	mu        sync.Mutex
	cancelBot context.CancelFunc
	running   bool
	lastError string
	logs      []string
}

type ControlSettings struct {
	Running bool   `json:"running"`
	Status  string `json:"status"`
	Error   string `json:"error"`

	Channel     string `json:"channel"`
	BotUsername string `json:"botUsername"`

	AIProvider         string  `json:"aiProvider"`
	AIModel            string  `json:"aiModel"`
	GeminiModel        string  `json:"geminiModel"`
	MaxRequestsPerHour int     `json:"maxRequestsPerHour"`
	DailyBudgetUSD     float64 `json:"dailyBudgetUsd"`
	MonthlyBudgetUSD   float64 `json:"monthlyBudgetUsd"`

	EnableMentions bool `json:"enableMentions"`
	EnableAsk      bool `json:"enableAsk"`
	EnableLurk     bool `json:"enableLurk"`
	EnableCommands bool `json:"enableCommands"`
	EnableReset    bool `json:"enableReset"`

	GlobalCooldownSeconds int `json:"globalCooldownSeconds"`
	UserCooldownSeconds   int `json:"userCooldownSeconds"`
	MaxContextMessages    int `json:"maxContextMessages"`

	AutosoEnabled          bool `json:"autosoEnabled"`
	RecentStreamerMinWatch int  `json:"recentStreamerMinWatch"`
	RecentStreamerDays     int  `json:"recentStreamerDays"`
	RecentStreamerPageSize int  `json:"recentStreamerPageSize"`
	RecentStreamerDelay    int  `json:"recentStreamerDelay"`

	AdAlertsEnabled  bool   `json:"adAlertsEnabled"`
	AdWarningMinutes int    `json:"adWarningMinutes"`
	AdPollSeconds    int    `json:"adPollSeconds"`
	AdWarningMessage string `json:"adWarningMessage"`
	AdStartMessage   string `json:"adStartMessage"`
	AdEndMessage     string `json:"adEndMessage"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.StopBot()
}

func (a *App) GetSettings() (ControlSettings, error) {
	cfg, err := config.Load(envPath)
	if err != nil {
		return ControlSettings{}, err
	}
	settings := ControlSettings{
		Channel:     cfg.Twitch.Channel,
		BotUsername: cfg.Twitch.BotUsername,

		AIProvider:         cfg.AI.Provider,
		AIModel:            cfg.AI.Model,
		GeminiModel:        cfg.AI.Model,
		MaxRequestsPerHour: cfg.Bot.MaxRequestsPerHour,
		DailyBudgetUSD:     cfg.Bot.DailyBudgetUSD,
		MonthlyBudgetUSD:   cfg.Bot.MonthlyBudgetUSD,

		EnableMentions: cfg.Bot.EnableMentions,
		EnableAsk:      cfg.Bot.EnableAsk,
		EnableLurk:     cfg.Bot.EnableLurk,
		EnableCommands: cfg.Bot.EnableCommands,
		EnableReset:    cfg.Bot.EnableReset,

		GlobalCooldownSeconds: int(cfg.Bot.GlobalCooldown / time.Second),
		UserCooldownSeconds:   int(cfg.Bot.UserCooldown / time.Second),
		MaxContextMessages:    cfg.Bot.MaxContextMessages,

		AutosoEnabled:          cfg.RecentStreamers.Enabled,
		RecentStreamerMinWatch: int(cfg.RecentStreamers.MinWatch / time.Minute),
		RecentStreamerDays:     int(cfg.RecentStreamers.RecentWindow / (24 * time.Hour)),
		RecentStreamerPageSize: cfg.RecentStreamers.PageSize,
		RecentStreamerDelay:    int(cfg.RecentStreamers.ShoutoutDelay / time.Second),

		AdAlertsEnabled:  cfg.AdAlerts.Enabled,
		AdWarningMinutes: displayMinutes(cfg.AdAlerts.WarningLead),
		AdPollSeconds:    int(cfg.AdAlerts.PollInterval / time.Second),
		AdWarningMessage: cfg.AdAlerts.WarningMessage,
		AdStartMessage:   cfg.AdAlerts.StartMessage,
		AdEndMessage:     cfg.AdAlerts.EndMessage,
	}
	a.mu.Lock()
	settings.Running = a.running
	settings.Error = a.lastError
	if a.running {
		settings.Status = "Running"
	} else {
		settings.Status = "Stopped"
	}
	a.mu.Unlock()
	return settings, nil
}

func (a *App) SaveSettings(settings ControlSettings) error {
	updates := map[string]string{
		"TWITCH_CHANNEL":           settings.Channel,
		"TWITCH_BOT_USERNAME":      settings.BotUsername,
		"AI_PROVIDER":              settings.AIProvider,
		"AI_MODEL":                 settings.AIModel,
		"GEMINI_MODEL":             settings.GeminiModel,
		"MAX_AI_REQUESTS_PER_HOUR": strconv.Itoa(settings.MaxRequestsPerHour),
		"DAILY_AI_BUDGET_USD":      formatFloat(settings.DailyBudgetUSD),
		"MONTHLY_AI_BUDGET_USD":    formatFloat(settings.MonthlyBudgetUSD),

		"ENABLE_MENTION_RESPONSES": boolString(settings.EnableMentions),
		"ENABLE_ASK_COMMAND":       boolString(settings.EnableAsk),
		"ENABLE_LURK_COMMAND":      boolString(settings.EnableLurk),
		"ENABLE_COMMANDS_COMMAND":  boolString(settings.EnableCommands),
		"ENABLE_RESET_COMMAND":     boolString(settings.EnableReset),

		"GLOBAL_COOLDOWN_SECONDS": strconv.Itoa(settings.GlobalCooldownSeconds),
		"USER_COOLDOWN_SECONDS":   strconv.Itoa(settings.UserCooldownSeconds),
		"MAX_CONTEXT_MESSAGES":    strconv.Itoa(settings.MaxContextMessages),

		"AUTOSO_ENABLED":                         boolString(settings.AutosoEnabled),
		"RECENT_STREAMER_MIN_WATCH_MINUTES":      strconv.Itoa(settings.RecentStreamerMinWatch),
		"RECENT_STREAMER_RECENT_DAYS":            strconv.Itoa(settings.RecentStreamerDays),
		"RECENT_STREAMER_PAGE_SIZE":              strconv.Itoa(settings.RecentStreamerPageSize),
		"RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS": strconv.Itoa(settings.RecentStreamerDelay),

		"AD_ALERTS_ENABLED":        boolString(settings.AdAlertsEnabled),
		"AD_ALERT_WARNING_MINUTES": strconv.Itoa(settings.AdWarningMinutes),
		"AD_ALERT_POLL_SECONDS":    strconv.Itoa(settings.AdPollSeconds),
		"AD_ALERT_WARNING_MESSAGE": settings.AdWarningMessage,
		"AD_ALERT_START_MESSAGE":   settings.AdStartMessage,
		"AD_ALERT_END_MESSAGE":     settings.AdEndMessage,
	}
	if err := updateEnvFile(envPath, updates); err != nil {
		return err
	}
	a.appendLog("settings saved")
	return nil
}

func (a *App) StartBot() error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelBot = cancel
	a.running = true
	a.lastError = ""
	a.mu.Unlock()

	logger := slog.New(slog.NewTextHandler(logWriter{app: a}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a.appendLog("starting bot")
	go func() {
		err := botrunner.Run(ctx, envPath, logger)
		a.mu.Lock()
		a.running = false
		a.cancelBot = nil
		if err != nil {
			a.lastError = err.Error()
		}
		a.mu.Unlock()
		if err != nil {
			a.appendLog("bot stopped with error: " + err.Error())
		} else {
			a.appendLog("bot stopped")
		}
	}()
	return nil
}

func (a *App) StopBot() error {
	a.mu.Lock()
	cancel := a.cancelBot
	a.mu.Unlock()
	if cancel != nil {
		a.appendLog("stopping bot")
		cancel()
	}
	return nil
}

func (a *App) GetLogs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.logs == nil {
		return []string{}
	}
	return append([]string{}, a.logs...)
}

func (a *App) appendLog(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	a.logs = append(a.logs, fmt.Sprintf("%s %s", time.Now().Format("15:04:05"), line))
	if len(a.logs) > 200 {
		a.logs = a.logs[len(a.logs)-200:]
	}
}

type logWriter struct {
	app *App
}

func (w logWriter) Write(p []byte) (int, error) {
	w.app.appendLog(string(p))
	return len(p), nil
}

func updateEnvFile(path string, updates map[string]string) error {
	lines, err := readEnvLines(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	seen := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}
		key, _, _ := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		value, ok := updates[key]
		if !ok {
			continue
		}
		lines[i] = key + "=" + encodeEnvValue(value)
		seen[key] = true
	}
	for key, value := range updates {
		if !seen[key] {
			lines = append(lines, key+"="+encodeEnvValue(value))
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func readEnvLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func encodeEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " #\t") {
		return strconv.Quote(value)
	}
	return value
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func displayMinutes(value time.Duration) int {
	if value <= 0 {
		return 0
	}
	minutes := int(value / time.Minute)
	if value%time.Minute != 0 {
		minutes++
	}
	if minutes < 1 {
		return 1
	}
	return minutes
}
