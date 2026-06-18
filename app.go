package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/announcements"
	"lupusaria/internal/botrunner"
	"lupusaria/internal/config"
	"lupusaria/internal/knowledge"
)

const envPathOverride = "LUPUSARIA_ENV_PATH"

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

	Channel           string `json:"channel"`
	BotUsername       string `json:"botUsername"`
	BotName           string `json:"botName"`
	BotPersonality    string `json:"botPersonality"`
	ConfigPath        string `json:"configPath"`
	StreamerName      string `json:"streamerName"`
	StreamerPronouns  string `json:"streamerPronouns"`
	KnowledgePath     string `json:"knowledgePath"`
	KnowledgeExists   bool   `json:"knowledgeExists"`
	KnowledgeSections int    `json:"knowledgeSections"`

	TwitchOAuthToken      string `json:"twitchOAuthToken"`
	TwitchRefreshToken    string `json:"twitchRefreshToken"`
	TwitchClientID        string `json:"twitchClientId"`
	TwitchClientSecret    string `json:"twitchClientSecret"`
	TwitchAdsClientID     string `json:"twitchAdsClientId"`
	TwitchAdsClientSecret string `json:"twitchAdsClientSecret"`
	TwitchAdsOAuthToken   string `json:"twitchAdsOAuthToken"`
	TwitchAdsRefreshToken string `json:"twitchAdsRefreshToken"`

	HasTwitchOAuthToken      bool `json:"hasTwitchOAuthToken"`
	HasTwitchRefreshToken    bool `json:"hasTwitchRefreshToken"`
	HasTwitchClientID        bool `json:"hasTwitchClientId"`
	HasTwitchClientSecret    bool `json:"hasTwitchClientSecret"`
	HasTwitchAdsClientID     bool `json:"hasTwitchAdsClientId"`
	HasTwitchAdsClientSecret bool `json:"hasTwitchAdsClientSecret"`
	HasTwitchAdsOAuthToken   bool `json:"hasTwitchAdsOAuthToken"`
	HasTwitchAdsRefreshToken bool `json:"hasTwitchAdsRefreshToken"`

	AIProvider         string  `json:"aiProvider"`
	AIAPIKey           string  `json:"aiApiKey"`
	GeminiAPIKey       string  `json:"geminiApiKey"`
	AIModel            string  `json:"aiModel"`
	GeminiModel        string  `json:"geminiModel"`
	MaxRequestsPerHour int     `json:"maxRequestsPerHour"`
	DailyBudgetUSD     float64 `json:"dailyBudgetUsd"`
	MonthlyBudgetUSD   float64 `json:"monthlyBudgetUsd"`
	HasAIAPIKey        bool    `json:"hasAiApiKey"`
	HasGeminiAPIKey    bool    `json:"hasGeminiApiKey"`

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

	AnnouncementsEnabled    bool `json:"announcementsEnabled"`
	AnnouncementPollSeconds int  `json:"announcementPollSeconds"`
}

type AnnouncementSettings struct {
	ID            string `json:"id"`
	Enabled       bool   `json:"enabled"`
	Kind          string `json:"kind"`
	Command       string `json:"command"`
	AfterMinutes  int    `json:"afterMinutes"`
	RepeatMinutes int    `json:"repeatMinutes"`
	Message       string `json:"message"`
}

type KnowledgeSettings struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Sections int    `json:"sections"`
	Content  string `json:"content"`
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
	envPath, err := appEnvPath()
	if err != nil {
		return ControlSettings{}, err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return ControlSettings{}, err
	}
	if err := knowledge.EnsureFile(cfg.Bot.KnowledgePath); err != nil {
		return ControlSettings{}, err
	}
	knowledgeExists, knowledgeSections := knowledgeStatus(cfg.Bot.KnowledgePath)
	settings := ControlSettings{
		Channel:           cfg.Twitch.Channel,
		BotUsername:       cfg.Twitch.BotUsername,
		BotName:           cfg.Bot.Name,
		BotPersonality:    cfg.Bot.Personality,
		ConfigPath:        envPath,
		StreamerName:      cfg.Bot.StreamerName,
		StreamerPronouns:  cfg.Bot.StreamerPronouns,
		KnowledgePath:     cfg.Bot.KnowledgePath,
		KnowledgeExists:   knowledgeExists,
		KnowledgeSections: knowledgeSections,

		HasTwitchOAuthToken:      cfg.Twitch.OAuthToken != "",
		HasTwitchRefreshToken:    cfg.Twitch.RefreshToken != "",
		HasTwitchClientID:        cfg.Twitch.ClientID != "",
		HasTwitchClientSecret:    cfg.Twitch.ClientSecret != "",
		HasTwitchAdsClientID:     cfg.Twitch.AdsClientID != "" && cfg.Twitch.AdsClientID != cfg.Twitch.ClientID,
		HasTwitchAdsClientSecret: cfg.Twitch.AdsClientSecret != "" && cfg.Twitch.AdsClientSecret != cfg.Twitch.ClientSecret,
		HasTwitchAdsOAuthToken:   cfg.Twitch.AdsOAuthToken != "",
		HasTwitchAdsRefreshToken: cfg.Twitch.AdsRefreshToken != "",

		AIProvider:         cfg.AI.Provider,
		AIModel:            cfg.AI.Model,
		GeminiModel:        cfg.AI.GeminiModel,
		MaxRequestsPerHour: cfg.Bot.MaxRequestsPerHour,
		DailyBudgetUSD:     cfg.Bot.DailyBudgetUSD,
		MonthlyBudgetUSD:   cfg.Bot.MonthlyBudgetUSD,
		HasAIAPIKey:        cfg.AI.Provider == "openai-compatible" && cfg.AI.APIKey != "",
		HasGeminiAPIKey:    cfg.AI.GeminiAPIKey != "",

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

		AnnouncementsEnabled:    cfg.Announcements.Enabled,
		AnnouncementPollSeconds: int(cfg.Announcements.PollInterval / time.Second),
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
	envPath, err := appEnvPath()
	if err != nil {
		return err
	}
	updates := map[string]string{
		"TWITCH_CHANNEL":           settings.Channel,
		"TWITCH_BOT_USERNAME":      settings.BotUsername,
		"BOT_NAME":                 settings.BotName,
		"BOT_PERSONALITY":          settings.BotPersonality,
		"STREAMER_NAME":            settings.StreamerName,
		"STREAMER_PRONOUNS":        settings.StreamerPronouns,
		"BOT_KNOWLEDGE_PATH":       settings.KnowledgePath,
		"AI_PROVIDER":              settings.AIProvider,
		"AI_BASE_URL":              aiBaseURL(settings),
		"AI_MODEL":                 settings.AIModel,
		"GEMINI_MODEL":             settings.GeminiModel,
		"AI_FALLBACK_PROVIDER":     aiFallbackProvider(settings),
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

		"ANNOUNCEMENTS_ENABLED":     boolString(settings.AnnouncementsEnabled),
		"ANNOUNCEMENT_POLL_SECONDS": strconv.Itoa(settings.AnnouncementPollSeconds),
	}
	addSecretUpdate(updates, "TWITCH_OAUTH_TOKEN", settings.TwitchOAuthToken)
	addSecretUpdate(updates, "TWITCH_REFRESH_TOKEN", settings.TwitchRefreshToken)
	addSecretUpdate(updates, "TWITCH_CLIENT_ID", settings.TwitchClientID)
	addSecretUpdate(updates, "TWITCH_CLIENT_SECRET", settings.TwitchClientSecret)
	addSecretUpdate(updates, "TWITCH_ADS_CLIENT_ID", settings.TwitchAdsClientID)
	addSecretUpdate(updates, "TWITCH_ADS_CLIENT_SECRET", settings.TwitchAdsClientSecret)
	addSecretUpdate(updates, "TWITCH_ADS_OAUTH_TOKEN", settings.TwitchAdsOAuthToken)
	addSecretUpdate(updates, "TWITCH_ADS_REFRESH_TOKEN", settings.TwitchAdsRefreshToken)
	addSecretUpdate(updates, "AI_API_KEY", settings.AIAPIKey)
	addSecretUpdate(updates, "GEMINI_API_KEY", settings.GeminiAPIKey)
	if err := updateEnvFile(envPath, updates); err != nil {
		return err
	}
	a.appendLog("settings saved")
	return nil
}

func (a *App) GetKnowledge() (KnowledgeSettings, error) {
	envPath, err := appEnvPath()
	if err != nil {
		return KnowledgeSettings{}, err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return KnowledgeSettings{}, err
	}
	if err := knowledge.EnsureFile(cfg.Bot.KnowledgePath); err != nil {
		return KnowledgeSettings{}, err
	}
	raw, err := os.ReadFile(cfg.Bot.KnowledgePath)
	if err != nil {
		return KnowledgeSettings{}, err
	}
	base := knowledge.Parse(string(raw))
	return KnowledgeSettings{
		Path:     cfg.Bot.KnowledgePath,
		Exists:   true,
		Sections: len(base.Sections),
		Content:  string(raw),
	}, nil
}

func (a *App) SaveKnowledge(settings KnowledgeSettings) error {
	envPath, err := appEnvPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return err
	}
	path := strings.TrimSpace(settings.Path)
	if path == "" {
		path = cfg.Bot.KnowledgePath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(settings.Content), 0600); err != nil {
		return err
	}
	if path != cfg.Bot.KnowledgePath {
		if err := updateEnvFile(envPath, map[string]string{"BOT_KNOWLEDGE_PATH": path}); err != nil {
			return err
		}
	}
	a.appendLog("knowledge saved")
	return nil
}

func (a *App) ResetKnowledgeTemplate() (KnowledgeSettings, error) {
	envPath, err := appEnvPath()
	if err != nil {
		return KnowledgeSettings{}, err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return KnowledgeSettings{}, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Bot.KnowledgePath), 0700); err != nil {
		return KnowledgeSettings{}, err
	}
	if err := os.WriteFile(cfg.Bot.KnowledgePath, []byte(knowledge.DefaultTemplate), 0600); err != nil {
		return KnowledgeSettings{}, err
	}
	a.appendLog("knowledge reset from template")
	return a.GetKnowledge()
}

func knowledgeStatus(path string) (bool, int) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, 0
	}
	return true, len(knowledge.Parse(string(raw)).Sections)
}

func (a *App) GetAnnouncements() ([]AnnouncementSettings, error) {
	envPath, err := appEnvPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return nil, err
	}
	items, err := announcements.Load(cfg.Announcements.Path)
	if err != nil {
		return nil, err
	}
	settings := make([]AnnouncementSettings, 0, len(items))
	for _, item := range items {
		settings = append(settings, AnnouncementSettings{
			ID:            item.ID,
			Enabled:       item.Enabled,
			Kind:          item.Kind,
			Command:       item.Command,
			AfterMinutes:  item.AfterMinutes,
			RepeatMinutes: item.RepeatMinutes,
			Message:       item.Message,
		})
	}
	return settings, nil
}

func (a *App) SaveAnnouncements(settings []AnnouncementSettings) error {
	envPath, err := appEnvPath()
	if err != nil {
		return err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return err
	}
	items := make([]announcements.Announcement, 0, len(settings))
	for _, item := range settings {
		items = append(items, announcements.Announcement{
			ID:            item.ID,
			Enabled:       item.Enabled,
			Kind:          item.Kind,
			Command:       item.Command,
			AfterMinutes:  item.AfterMinutes,
			RepeatMinutes: item.RepeatMinutes,
			Message:       item.Message,
		})
	}
	if err := announcements.Save(cfg.Announcements.Path, items); err != nil {
		return err
	}
	a.appendLog("announcements saved")
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
	envPath, err := appEnvPath()
	if err != nil {
		cancel()
		a.mu.Lock()
		a.running = false
		a.cancelBot = nil
		a.lastError = err.Error()
		a.mu.Unlock()
		return err
	}
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
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
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

func appEnvPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(envPathOverride)); override != "" {
		return override, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "Starsong Tools", "LupusAria", ".env"), nil
}

func addSecretUpdate(updates map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		updates[key] = value
	}
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

func aiBaseURL(settings ControlSettings) string {
	if settings.AIProvider == "openai-compatible" {
		return "http://localhost:11434/v1"
	}
	return ""
}

func aiFallbackProvider(settings ControlSettings) string {
	if settings.AIProvider == "openai-compatible" && (settings.GeminiAPIKey != "" || settings.HasGeminiAPIKey) {
		return "gemini"
	}
	return ""
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
