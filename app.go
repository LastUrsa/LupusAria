package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"log/slog"
	"math/rand"
	"mime"
	"net"
	"net/http"
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
	"lupusaria/internal/mediaactions"
	"lupusaria/internal/twitch"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	envPathOverride         = "LUPUSARIA_ENV_PATH"
	defaultMediaOverlayPort = 47831
)

type App struct {
	ctx context.Context

	mu        sync.Mutex
	cancelBot context.CancelFunc
	running   bool
	lastError string
	logs      []string

	overlay *overlayServer
}

type ControlSettings struct {
	Running bool   `json:"running"`
	Status  string `json:"status"`
	Error   string `json:"error"`

	Channel          string `json:"channel"`
	BotUsername      string `json:"botUsername"`
	ConfigPath       string `json:"configPath"`
	StreamerName     string `json:"streamerName"`
	StreamerPronouns string `json:"streamerPronouns"`
	KnowledgePath    string `json:"knowledgePath"`
	KnowledgeExists  bool   `json:"knowledgeExists"`

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

	MentionPermission    string `json:"mentionPermission"`
	AskPermission        string `json:"askPermission"`
	LurkPermission       string `json:"lurkPermission"`
	GamePermission       string `json:"gamePermission"`
	CommandsPermission   string `json:"commandsPermission"`
	ResetPermission      string `json:"resetPermission"`
	AutosoPermission     string `json:"autosoPermission"`
	SORoulettePermission string `json:"soRoulettePermission"`

	GlobalCooldownSeconds int `json:"globalCooldownSeconds"`
	UserCooldownSeconds   int `json:"userCooldownSeconds"`
	MaxContextMessages    int `json:"maxContextMessages"`

	GameSnapshotCropEnabled bool    `json:"gameSnapshotCropEnabled"`
	GameSnapshotCropX       float64 `json:"gameSnapshotCropX"`
	GameSnapshotCropY       float64 `json:"gameSnapshotCropY"`
	GameSnapshotCropWidth   float64 `json:"gameSnapshotCropWidth"`
	GameSnapshotCropHeight  float64 `json:"gameSnapshotCropHeight"`

	AutosoEnabled          bool   `json:"autosoEnabled"`
	RecentStreamerMinWatch int    `json:"recentStreamerMinWatch"`
	RecentStreamerDays     int    `json:"recentStreamerDays"`
	RecentStreamerPageSize int    `json:"recentStreamerPageSize"`
	RecentStreamerDelay    int    `json:"recentStreamerDelay"`
	SORouletteStreamers    string `json:"soRouletteStreamers"`

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
	Permission    string `json:"permission"`
	AfterMinutes  int    `json:"afterMinutes"`
	RepeatMinutes int    `json:"repeatMinutes"`
	Message       string `json:"message"`
}

type MediaActionSettings struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Enabled           bool                 `json:"enabled"`
	Trigger           string               `json:"trigger"`
	RewardID          string               `json:"rewardId"`
	RewardTitle       string               `json:"rewardTitle"`
	Media             []MediaAssetSettings `json:"media"`
	Sounds            []MediaAssetSettings `json:"sounds"`
	Duration          int                  `json:"duration"`
	Position          string               `json:"position"`
	Scale             int                  `json:"scale"`
	Animation         string               `json:"animation"`
	MediaPlaybackMode string               `json:"mediaPlaybackMode"`
}

type MediaAssetSettings struct {
	ID                     string `json:"id"`
	Filename               string `json:"filename"`
	Path                   string `json:"path"`
	DurationMS             int    `json:"durationMs"`
	MediaPlaybackMode      string `json:"mediaPlaybackMode"`
	ExcludeFromGifRotation bool   `json:"excludeFromGifRotation"`
}

type ChannelPointRewardSettings struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Prompt  string `json:"prompt"`
	Enabled bool   `json:"enabled"`
}

type MediaActionPlayback struct {
	ActionID           string              `json:"actionId"`
	Name               string              `json:"name"`
	Media              *MediaAssetSettings `json:"media,omitempty"`
	Sound              *MediaAssetSettings `json:"sound,omitempty"`
	MediaDataURL       string              `json:"mediaDataUrl"`
	SoundDataURL       string              `json:"soundDataUrl"`
	Duration           int                 `json:"duration"`
	Position           string              `json:"position"`
	Scale              int                 `json:"scale"`
	Animation          string              `json:"animation"`
	MediaDurationMS    int                 `json:"mediaDurationMs"`
	MediaFrameDataURLs []string            `json:"mediaFrameDataUrls"`
	MediaFrameDelaysMS []int               `json:"mediaFrameDelaysMs"`
	MediaPlaybackMode  string              `json:"mediaPlaybackMode"`
	MediaClips         []MediaPlaybackClip `json:"mediaClips"`
}

type overlayServer struct {
	server  *http.Server
	url     string
	mu      sync.Mutex
	clients map[chan []byte]bool
}

type MediaPlaybackClip struct {
	Media              MediaAssetSettings `json:"media"`
	MediaDataURL       string             `json:"mediaDataUrl"`
	MediaDurationMS    int                `json:"mediaDurationMs"`
	MediaFrameDataURLs []string           `json:"mediaFrameDataUrls"`
	MediaFrameDelaysMS []int              `json:"mediaFrameDelaysMs"`
}

type KnowledgeSettings struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Content string `json:"content"`
}

type TwitchPermissionCheck struct {
	CheckedAt string                 `json:"checkedAt"`
	Overall   string                 `json:"overall"`
	Items     []TwitchPermissionItem `json:"items"`
}

type TwitchPermissionItem struct {
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	Detail        string   `json:"detail"`
	MissingScopes []string `json:"missingScopes"`
}

type twitchTokenValidation struct {
	ClientID  string   `json:"client_id"`
	Login     string   `json:"login"`
	Scopes    []string `json:"scopes"`
	UserID    string   `json:"user_id"`
	ExpiresIn int      `json:"expires_in"`
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	overlay, err := newOverlayServer()
	if err != nil {
		a.appendLog("media overlay unavailable: " + err.Error())
		return
	}
	a.overlay = overlay
	a.appendLog("media overlay listening at " + overlay.URL())
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.StopBot()
	if a.overlay != nil {
		_ = a.overlay.Close(ctx)
	}
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
	knowledgeExists := knowledgeExists(cfg.Bot.KnowledgePath)
	settings := ControlSettings{
		Channel:          cfg.Twitch.Channel,
		BotUsername:      cfg.Twitch.BotUsername,
		ConfigPath:       envPath,
		StreamerName:     cfg.Bot.StreamerName,
		StreamerPronouns: cfg.Bot.StreamerPronouns,
		KnowledgePath:    cfg.Bot.KnowledgePath,
		KnowledgeExists:  knowledgeExists,

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

		MentionPermission:    cfg.Bot.MentionPermission,
		AskPermission:        cfg.Bot.AskPermission,
		LurkPermission:       cfg.Bot.LurkPermission,
		GamePermission:       cfg.Bot.GamePermission,
		CommandsPermission:   cfg.Bot.CommandsPermission,
		ResetPermission:      cfg.Bot.ResetPermission,
		AutosoPermission:     cfg.RecentStreamers.Permission,
		SORoulettePermission: cfg.RecentStreamers.SORoulettePermission,

		GlobalCooldownSeconds: int(cfg.Bot.GlobalCooldown / time.Second),
		UserCooldownSeconds:   int(cfg.Bot.UserCooldown / time.Second),
		MaxContextMessages:    cfg.Bot.MaxContextMessages,

		GameSnapshotCropEnabled: cfg.Bot.SnapshotCrop.Enabled,
		GameSnapshotCropX:       cfg.Bot.SnapshotCrop.X,
		GameSnapshotCropY:       cfg.Bot.SnapshotCrop.Y,
		GameSnapshotCropWidth:   cfg.Bot.SnapshotCrop.Width,
		GameSnapshotCropHeight:  cfg.Bot.SnapshotCrop.Height,

		AutosoEnabled:          cfg.RecentStreamers.Enabled,
		RecentStreamerMinWatch: int(cfg.RecentStreamers.MinWatch / time.Minute),
		RecentStreamerDays:     int(cfg.RecentStreamers.RecentWindow / (24 * time.Hour)),
		RecentStreamerPageSize: cfg.RecentStreamers.PageSize,
		RecentStreamerDelay:    int(cfg.RecentStreamers.ShoutoutDelay / time.Second),
		SORouletteStreamers:    strings.Join(cfg.RecentStreamers.RouletteStreamers, "\n"),

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
		"STREAMER_NAME":            settings.StreamerName,
		"STREAMER_PRONOUNS":        settings.StreamerPronouns,
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

		"MENTION_PERMISSION":             settings.MentionPermission,
		"ASK_COMMAND_PERMISSION":         settings.AskPermission,
		"LURK_COMMAND_PERMISSION":        settings.LurkPermission,
		"GAME_COMMAND_PERMISSION":        settings.GamePermission,
		"COMMANDS_COMMAND_PERMISSION":    settings.CommandsPermission,
		"RESET_COMMAND_PERMISSION":       settings.ResetPermission,
		"AUTOSO_COMMAND_PERMISSION":      settings.AutosoPermission,
		"SO_ROULETTE_COMMAND_PERMISSION": settings.SORoulettePermission,

		"GLOBAL_COOLDOWN_SECONDS": strconv.Itoa(settings.GlobalCooldownSeconds),
		"USER_COOLDOWN_SECONDS":   strconv.Itoa(settings.UserCooldownSeconds),
		"MAX_CONTEXT_MESSAGES":    strconv.Itoa(settings.MaxContextMessages),

		"GAME_SNAPSHOT_CROP_ENABLED": boolString(settings.GameSnapshotCropEnabled),
		"GAME_SNAPSHOT_CROP_X":       formatFloat(settings.GameSnapshotCropX),
		"GAME_SNAPSHOT_CROP_Y":       formatFloat(settings.GameSnapshotCropY),
		"GAME_SNAPSHOT_CROP_WIDTH":   formatFloat(settings.GameSnapshotCropWidth),
		"GAME_SNAPSHOT_CROP_HEIGHT":  formatFloat(settings.GameSnapshotCropHeight),

		"AUTOSO_ENABLED":                         boolString(settings.AutosoEnabled),
		"RECENT_STREAMER_MIN_WATCH_MINUTES":      strconv.Itoa(settings.RecentStreamerMinWatch),
		"RECENT_STREAMER_RECENT_DAYS":            strconv.Itoa(settings.RecentStreamerDays),
		"RECENT_STREAMER_PAGE_SIZE":              strconv.Itoa(settings.RecentStreamerPageSize),
		"RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS": strconv.Itoa(minInt(settings.RecentStreamerDelay, 1)),
		"SO_ROULETTE_STREAMERS":                  formatLoginList(settings.SORouletteStreamers),

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

func (a *App) CheckTwitchPermissions() (TwitchPermissionCheck, error) {
	envPath, err := appEnvPath()
	if err != nil {
		return TwitchPermissionCheck{}, err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return TwitchPermissionCheck{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	check := TwitchPermissionCheck{
		CheckedAt: time.Now().Format(time.RFC3339),
		Overall:   "ok",
	}
	check.Items = append(check.Items, checkTwitchAppCredentials(ctx, cfg))
	check.Items = append(check.Items, checkTwitchUserToken(ctx, "Bot token", cfg.Twitch.ClientID, cfg.Twitch.ClientSecret, cfg.Twitch.OAuthToken, cfg.Twitch.RefreshToken, cfg.Twitch.TokenStatePath, preferConfiguredRefreshToken(envPath, cfg.Twitch.TokenStatePath), cfg.Twitch.BotUsername, []string{
		"user:read:chat",
		"user:write:chat",
		"user:bot",
		"moderator:read:chatters",
		"moderator:read:followers",
	}))
	if cfg.AdAlerts.Enabled || cfg.Twitch.AdsOAuthToken != "" || cfg.Twitch.AdsRefreshToken != "" {
		broadcasterScopes := []string{"channel:read:redemptions"}
		if cfg.AdAlerts.Enabled {
			broadcasterScopes = append(broadcasterScopes, "channel:read:ads")
		}
		check.Items = append(check.Items, checkTwitchUserToken(ctx, "Broadcaster token", cfg.Twitch.AdsClientID, cfg.Twitch.AdsClientSecret, cfg.Twitch.AdsOAuthToken, cfg.Twitch.AdsRefreshToken, cfg.Twitch.AdsTokenStatePath, preferConfiguredRefreshToken(envPath, cfg.Twitch.AdsTokenStatePath), cfg.Twitch.Channel, broadcasterScopes))
	} else {
		check.Items = append(check.Items, TwitchPermissionItem{
			Name:   "Broadcaster token",
			Status: "warning",
			Detail: "No broadcaster token is saved. Media Actions need a channel token with channel:read:redemptions.",
		})
	}
	check.Overall = overallPermissionStatus(check.Items)
	for i := range check.Items {
		if check.Items[i].MissingScopes == nil {
			check.Items[i].MissingScopes = []string{}
		}
	}
	a.appendLog("twitch permissions checked: " + check.Overall)
	return check, nil
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
	return KnowledgeSettings{
		Path:    cfg.Bot.KnowledgePath,
		Exists:  true,
		Content: string(raw),
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
	path := cfg.Bot.KnowledgePath
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(settings.Content), 0600); err != nil {
		return err
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

func checkTwitchAppCredentials(ctx context.Context, cfg config.Config) TwitchPermissionItem {
	if strings.TrimSpace(cfg.Twitch.ClientID) == "" {
		return TwitchPermissionItem{Name: "Twitch app", Status: "error", Detail: "Missing Twitch client ID."}
	}
	if strings.TrimSpace(cfg.Twitch.ClientSecret) == "" {
		return TwitchPermissionItem{Name: "Twitch app", Status: "error", Detail: "Missing Twitch client secret."}
	}
	_, err := twitch.NewAuthManager(twitch.AuthConfig{
		ClientID:     cfg.Twitch.ClientID,
		ClientSecret: cfg.Twitch.ClientSecret,
		StatePath:    cfg.Twitch.AppTokenStatePath,
	}).AppAccessToken(ctx)
	if err != nil {
		return TwitchPermissionItem{Name: "Twitch app", Status: "error", Detail: "Could not get a Twitch app access token: " + safePermissionError(err)}
	}
	return TwitchPermissionItem{Name: "Twitch app", Status: "ok", Detail: "Client ID and secret can mint an app access token for chat sends."}
}

func checkTwitchUserToken(ctx context.Context, name, clientID, clientSecret, accessToken, refreshToken, statePath string, preferConfiguredRefresh bool, expectedLogin string, requiredScopes []string) TwitchPermissionItem {
	token := strings.TrimSpace(strings.TrimPrefix(accessToken, "oauth:"))
	refreshed := false
	if strings.TrimSpace(refreshToken) != "" && strings.TrimSpace(clientID) != "" && strings.TrimSpace(clientSecret) != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     clientID,
			ClientSecret:                 clientSecret,
			RefreshToken:                 refreshToken,
			StatePath:                    statePath,
			PreferConfiguredRefreshToken: preferConfiguredRefresh,
		}).Refresh(ctx)
		if err == nil {
			token = tokenSet.AccessToken
			refreshed = true
		} else if token == "" {
			return TwitchPermissionItem{Name: name, Status: "error", Detail: "Refresh failed and no saved access token is available: " + safePermissionError(err)}
		}
	}
	if token == "" {
		return TwitchPermissionItem{Name: name, Status: "error", Detail: "Missing access token or refresh token."}
	}

	validation, err := validateTwitchToken(ctx, http.DefaultClient, token)
	if err != nil {
		return TwitchPermissionItem{Name: name, Status: "error", Detail: "Token validation failed: " + safePermissionError(err)}
	}

	missing := missingScopes(validation.Scopes, requiredScopes)
	if len(missing) > 0 {
		return TwitchPermissionItem{
			Name:          name,
			Status:        "error",
			Detail:        fmt.Sprintf("Validated as %s, but required scopes are missing.", displayLogin(validation.Login)),
			MissingScopes: missing,
		}
	}

	if expected := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(expectedLogin, "@"))); expected != "" && validation.Login != "" && !strings.EqualFold(expected, validation.Login) {
		return TwitchPermissionItem{
			Name:   name,
			Status: "warning",
			Detail: fmt.Sprintf("Validated as %s; expected %s.", displayLogin(validation.Login), expected),
		}
	}

	source := "access token"
	if refreshed {
		source = "refresh token"
	}
	return TwitchPermissionItem{
		Name:   name,
		Status: "ok",
		Detail: fmt.Sprintf("Validated %s for %s with all required scopes.", source, displayLogin(validation.Login)),
	}
}

func preferConfiguredRefreshToken(configPath, statePath string) bool {
	if strings.TrimSpace(configPath) == "" || strings.TrimSpace(statePath) == "" {
		return false
	}
	configInfo, err := os.Stat(configPath)
	if err != nil {
		return false
	}
	stateInfo, err := os.Stat(statePath)
	if err != nil {
		return true
	}
	return configInfo.ModTime().After(stateInfo.ModTime())
}

func mediaActionsTwitchToken(ctx context.Context, envPath string, cfg config.Config) (string, string, error) {
	clientID := firstNonEmptyString(cfg.Twitch.AdsClientID, cfg.Twitch.ClientID)
	clientSecret := firstNonEmptyString(cfg.Twitch.AdsClientSecret, cfg.Twitch.ClientSecret)
	token := strings.TrimSpace(cfg.Twitch.AdsOAuthToken)
	if cfg.Twitch.AdsRefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     clientID,
			ClientSecret:                 clientSecret,
			RefreshToken:                 cfg.Twitch.AdsRefreshToken,
			StatePath:                    cfg.Twitch.AdsTokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.AdsTokenStatePath),
		}).Refresh(ctx)
		if err == nil {
			return clientID, "oauth:" + tokenSet.AccessToken, nil
		}
		if token == "" {
			return "", "", fmt.Errorf("refresh broadcaster token: %w", err)
		}
	}
	if token != "" {
		return clientID, token, nil
	}

	token = strings.TrimSpace(cfg.Twitch.OAuthToken)
	if cfg.Twitch.RefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     cfg.Twitch.ClientID,
			ClientSecret:                 cfg.Twitch.ClientSecret,
			RefreshToken:                 cfg.Twitch.RefreshToken,
			StatePath:                    cfg.Twitch.TokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.TokenStatePath),
		}).Refresh(ctx)
		if err == nil {
			return cfg.Twitch.ClientID, "oauth:" + tokenSet.AccessToken, nil
		}
		if token == "" {
			return "", "", fmt.Errorf("refresh twitch token: %w", err)
		}
	}
	if token == "" {
		return "", "", errors.New("missing broadcaster access token or refresh token")
	}
	return cfg.Twitch.ClientID, token, nil
}

func validateTwitchToken(ctx context.Context, client *http.Client, token string) (twitchTokenValidation, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://id.twitch.tv/oauth2/validate", nil)
	if err != nil {
		return twitchTokenValidation{}, err
	}
	req.Header.Set("Authorization", "OAuth "+strings.TrimSpace(strings.TrimPrefix(token, "oauth:")))
	resp, err := client.Do(req)
	if err != nil {
		return twitchTokenValidation{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Message != "" {
			return twitchTokenValidation{}, errors.New(apiErr.Message)
		}
		return twitchTokenValidation{}, fmt.Errorf("validate failed with status %s", resp.Status)
	}
	var validation twitchTokenValidation
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		return twitchTokenValidation{}, err
	}
	return validation, nil
}

func missingScopes(have, required []string) []string {
	haveSet := map[string]bool{}
	for _, scope := range have {
		haveSet[scope] = true
	}
	var missing []string
	for _, scope := range required {
		if !haveSet[scope] {
			missing = append(missing, scope)
		}
	}
	return missing
}

func overallPermissionStatus(items []TwitchPermissionItem) string {
	overall := "ok"
	for _, item := range items {
		if item.Status == "error" {
			return "error"
		}
		if item.Status == "warning" {
			overall = "warning"
		}
	}
	return overall
}

func displayLogin(login string) string {
	login = strings.TrimSpace(login)
	if login == "" {
		return "the authorized account"
	}
	return "@" + login
}

func safePermissionError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "unknown error"
	}
	return msg
}

func knowledgeExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
			Permission:    item.Permission,
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
			Permission:    item.Permission,
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

func (a *App) GetMediaActions() ([]MediaActionSettings, error) {
	path, err := mediaActionsPath()
	if err != nil {
		return nil, err
	}
	actions, err := mediaactions.Load(path)
	if err != nil {
		return nil, err
	}
	return mediaActionSettingsFromActions(actions), nil
}

func (a *App) SaveMediaActions(settings []MediaActionSettings) error {
	path, err := mediaActionsPath()
	if err != nil {
		return err
	}
	actions := mediaActionsFromSettings(settings)
	if err := mediaactions.Save(path, actions); err != nil {
		return err
	}
	a.appendLog("media actions saved")
	return nil
}

func (a *App) ImportMediaActionAssets(action MediaActionSettings, kind string) ([]MediaAssetSettings, error) {
	if a.ctx == nil {
		return nil, errors.New("app is not ready")
	}
	extensions := mediaactions.SupportedExtensions(kind)
	patterns := make([]string, 0, len(extensions))
	for _, ext := range extensions {
		patterns = append(patterns, "*"+ext)
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Add " + kind,
		Filters: []runtime.FileFilter{{
			DisplayName: strings.Title(kind) + " files",
			Pattern:     strings.Join(patterns, ";"),
		}},
	})
	if err != nil {
		return nil, err
	}
	root, err := mediaActionsRoot()
	if err != nil {
		return nil, err
	}
	imported, err := mediaactions.ImportAssets(root, mediaActionFromSettings(action), kind, paths)
	if err != nil {
		return nil, err
	}
	return mediaAssetSettingsFromAssets(imported), nil
}

func (a *App) GetChannelPointRewards() ([]ChannelPointRewardSettings, error) {
	envPath, err := appEnvPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadPartial(envPath)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Twitch.ClientID) == "" {
		return nil, errors.New("missing Twitch client ID")
	}
	clientID, token, err := mediaActionsTwitchToken(context.Background(), envPath, cfg)
	if err != nil {
		return nil, err
	}
	helix := twitch.NewHelixClient(clientID, token)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	users, err := helix.GetUsersByLogin(ctx, []string{cfg.Twitch.Channel})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, errors.New("could not find Twitch channel")
	}
	rewards, err := helix.GetCustomRewards(ctx, users[0].ID)
	if err != nil {
		return nil, err
	}
	out := make([]ChannelPointRewardSettings, 0, len(rewards))
	for _, reward := range rewards {
		out = append(out, ChannelPointRewardSettings{
			ID:      reward.ID,
			Title:   reward.Title,
			Prompt:  reward.Prompt,
			Enabled: reward.Enabled,
		})
	}
	return out, nil
}

func (a *App) GetMediaAssetDataURL(path string) (string, error) {
	root, err := mediaActionsRoot()
	if err != nil {
		return "", err
	}
	return mediaAssetDataURL(root, path)
}

func (a *App) PreviewMediaAction(action MediaActionSettings) error {
	root, err := mediaActionsRoot()
	if err != nil {
		return err
	}
	playback, ok := mediaactions.SelectPlayback(mediaActionFromSettings(action), nil)
	if !ok {
		return errors.New("add at least one media or sound asset before previewing")
	}
	payload, err := mediaPlaybackFromPlayback(root, playback)
	if err != nil {
		return err
	}
	a.emitMediaActionPreview(payload)
	a.appendLog("previewed media action: " + action.Name)
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
	mediaOptions := botrunner.Options{}
	actions, actionErr := loadEnabledMediaActions()
	if actionErr != nil {
		a.appendLog("media actions disabled: " + actionErr.Error())
	} else if len(actions) > 0 {
		mediaOptions.MediaActionRedeem = a.mediaActionRedeemHandler(ctx, actions)
		a.appendLog(fmt.Sprintf("loaded media actions: %d; redeem listener enabled", len(actions)))
	}
	go func() {
		err := botrunner.RunWithOptions(ctx, envPath, logger, mediaOptions)
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

func (a *App) GetMediaOverlayURL() string {
	if a.overlay == nil {
		return ""
	}
	return a.overlay.URL()
}

func (a *App) emitMediaActionPreview(playback MediaActionPlayback) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "media-action-playback", playback)
	}
	a.broadcastMediaActionPlayback(playback)
}

func (a *App) broadcastMediaActionPlayback(playback MediaActionPlayback) {
	if a.overlay != nil {
		a.overlay.Broadcast(playback)
	}
}

func (a *App) mediaActionRedeemHandler(ctx context.Context, actions []mediaactions.Action) func(context.Context, twitch.ChannelPointRedeemEvent) {
	root, err := mediaActionsRoot()
	if err != nil {
		a.appendLog("media action storage unavailable: " + err.Error())
		return nil
	}
	byReward := map[string]mediaactions.Action{}
	byRewardTitle := map[string]mediaactions.Action{}
	for _, action := range actions {
		if action.Enabled && action.RewardID != "" {
			byReward[action.RewardID] = action
		}
		if action.Enabled && action.RewardTitle != "" {
			byRewardTitle[normalizeMediaActionRewardTitle(action.RewardTitle)] = action
		}
	}
	queue := make(chan MediaActionPlayback, 32)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case playback := <-queue:
				a.broadcastMediaActionPlayback(playback)
				wait := time.Duration(playback.Duration) * time.Second
				if wait <= 0 {
					wait = 5 * time.Second
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
		}
	}()
	return func(ctx context.Context, event twitch.ChannelPointRedeemEvent) {
		a.appendLog(fmt.Sprintf("received channel point redeem: %q", firstNonEmptyString(event.RewardTitle, event.RewardID)))
		action, ok := byReward[event.RewardID]
		if !ok && event.RewardTitle != "" {
			action, ok = byRewardTitle[normalizeMediaActionRewardTitle(event.RewardTitle)]
		}
		if !ok {
			a.appendLog(fmt.Sprintf("received redeem %q but no enabled media action matched it", firstNonEmptyString(event.RewardTitle, event.RewardID)))
			return
		}
		playback, ok := mediaactions.SelectPlayback(action, rng)
		if !ok {
			a.appendLog(fmt.Sprintf("media action %q has no playable assets", action.Name))
			return
		}
		payload, err := mediaPlaybackFromPlayback(root, playback)
		if err != nil {
			a.appendLog("media action failed: " + err.Error())
			return
		}
		select {
		case queue <- payload:
			a.appendLog(fmt.Sprintf("queued media action %q for redeem %q", action.Name, firstNonEmptyString(event.RewardTitle, action.RewardTitle)))
		case <-ctx.Done():
		default:
			a.appendLog("media action queue is full; redeem skipped")
		}
	}
}

func normalizeMediaActionRewardTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
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

func newOverlayServer() (*overlayServer, error) {
	return newOverlayServerAtAddress(defaultMediaOverlayAddress())
}

func defaultMediaOverlayAddress() string {
	return fmt.Sprintf("127.0.0.1:%d", defaultMediaOverlayPort)
}

func newOverlayServerAtAddress(address string) (*overlayServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	address = listener.Addr().String()
	overlay := &overlayServer{
		url:     "http://" + address + "/",
		clients: map[chan []byte]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", overlay.handleIndex)
	mux.HandleFunc("/events", overlay.handleEvents)
	overlay.server = &http.Server{Handler: mux}
	go func() {
		if err := overlay.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Default().Warn("media overlay server stopped", "error", err)
		}
	}()
	return overlay, nil
}

func (s *overlayServer) URL() string {
	return s.url
}

func (s *overlayServer) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *overlayServer) Broadcast(playback MediaActionPlayback) {
	data, err := json.Marshal(playback)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for client := range s.clients {
		select {
		case client <- data:
		default:
		}
	}
}

func (s *overlayServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(mediaOverlayHTML))
}

func (s *overlayServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := make(chan []byte, 4)
	s.mu.Lock()
	s.clients[client] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, client)
		s.mu.Unlock()
		close(client)
	}()

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-client:
			_, _ = fmt.Fprintf(w, "event: playback\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

const mediaOverlayHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>LupusAria Media Overlay</title>
  <style>
    html, body {
      margin: 0;
      width: 100%;
      height: 100%;
      overflow: hidden;
      background: transparent;
    }
    body {
      display: grid;
      font-family: system-ui, sans-serif;
    }
    #stage {
      pointer-events: none;
      position: fixed;
      inset: 0;
      display: grid;
      padding: 32px;
      opacity: 0;
      transition: opacity 160ms ease-out;
    }
    #stage.visible {
      opacity: 1;
    }
    #stage.center { place-items: center; }
    #stage.top-left { place-items: start; }
    #stage.top-right { place-items: start end; }
    #stage.bottom-left { place-items: end start; }
    #stage.bottom-right { place-items: end; }
    img {
      max-width: min(82vw, 1200px);
      max-height: min(82vh, 900px);
      object-fit: contain;
      transform-origin: center;
    }
  </style>
</head>
<body>
  <div id="stage" class="center"><img id="media" alt=""></div>
  <script>
    const stage = document.getElementById('stage');
    const media = document.getElementById('media');
    let hideTimer = null;
    let frameTimer = null;
    function clearFrameTimer() {
      if (frameTimer) {
        clearTimeout(frameTimer);
        frameTimer = null;
      }
    }
    function cacheBustedDataUrl(url, token) {
      if (!url) {
        return '';
      }
      return url + '#clip-' + token + '-' + Date.now();
    }
    function animateFrames(event, audio) {
      clearFrameTimer();
      if (event.mediaPlaybackMode === 'loop_next' && event.mediaClips && event.mediaClips.length) {
        const actionDuration = Math.max(1, event.duration || 5) * 1000;
        const startedAt = Date.now();
        let clipIndex = 0;
        const playClip = () => {
          const clip = event.mediaClips[clipIndex] || event.mediaClips[0];
          media.src = cacheBustedDataUrl(clip.mediaDataUrl || event.mediaDataUrl, clipIndex);
          const delay = Math.max(100, clip.mediaDurationMs || 1000);
          clipIndex = (clipIndex + 1) % event.mediaClips.length;
          if (Date.now() - startedAt + delay >= actionDuration) {
            return;
          }
          frameTimer = setTimeout(playClip, delay);
        };
        playClip();
        return;
      }
      if (event.mediaPlaybackMode !== 'match_audio') {
        media.src = cacheBustedDataUrl(event.mediaDataUrl || '', 0);
        return;
      }
      const clips = event.mediaPlaybackMode === 'loop_next' && event.mediaClips && event.mediaClips.length
        ? event.mediaClips
        : [{
            mediaDataUrl: event.mediaDataUrl,
            mediaDurationMs: event.mediaDurationMs,
            mediaFrameDataUrls: event.mediaFrameDataUrls,
            mediaFrameDelaysMs: event.mediaFrameDelaysMs
          }];
      const firstClip = clips[0] || {};
      const firstDelays = firstClip.mediaFrameDelaysMs || [];
      const baseDuration = Math.max(1, firstClip.mediaDurationMs || firstDelays.reduce((total, delay) => total + Math.max(10, delay || 100), 0));
      const actionDuration = Math.max(1, event.duration || 5) * 1000;
      const startedAt = Date.now();
      const start = (targetDuration) => {
        const scale = event.mediaPlaybackMode === 'match_audio' ? Math.max(0.1, targetDuration / baseDuration) : 1;
        let clipIndex = 0;
        let frameIndex = 0;
        const tick = () => {
          const clip = clips[clipIndex] || firstClip;
          const frames = clip.mediaFrameDataUrls || [];
          const delays = clip.mediaFrameDelaysMs || [];
          media.src = frames[frameIndex] || clip.mediaDataUrl || event.mediaDataUrl || '';
          const delay = Math.max(10, delays[frameIndex] || 100) * scale;
          frameIndex += 1;
          if (frameIndex >= Math.max(1, frames.length)) {
            if (event.mediaPlaybackMode === 'loop_next') {
              clipIndex = (clipIndex + 1) % clips.length;
              frameIndex = 0;
              if (Date.now() - startedAt >= actionDuration) {
                return;
              }
            } else if (event.mediaPlaybackMode === 'loop') {
              frameIndex = 0;
              if (Date.now() - startedAt >= actionDuration) {
                return;
              }
            } else {
              return;
            }
          }
          frameTimer = setTimeout(tick, delay);
        };
        tick();
      };
      if (event.mediaPlaybackMode === 'match_audio' && audio) {
        if (Number.isFinite(audio.duration) && audio.duration > 0) {
          start(audio.duration * 1000);
        } else {
          audio.addEventListener('loadedmetadata', () => start(Math.max(100, audio.duration * 1000)), { once: true });
        }
      } else {
        start(baseDuration);
      }
    }
    function play(event) {
      clearTimeout(hideTimer);
      clearFrameTimer();
      stage.className = event.position || 'center';
      let audio = null;
      if (event.soundDataUrl) {
        audio = new Audio(event.soundDataUrl);
      }
      if (event.mediaDataUrl) {
        media.style.transform = 'scale(' + ((event.scale || 100) / 100) + ')';
        animateFrames(event, audio);
        stage.classList.add('visible');
      } else {
        media.removeAttribute('src');
        stage.classList.remove('visible');
      }
      if (audio) {
        audio.play().catch(() => {});
      }
      const hideAfter = event.mediaPlaybackMode === 'match_audio' && audio && Number.isFinite(audio.duration) && audio.duration > 0
        ? Math.max(Math.max(1, event.duration || 5) * 1000, audio.duration * 1000)
        : Math.max(1, event.duration || 5) * 1000;
      hideTimer = setTimeout(() => {
        clearFrameTimer();
        stage.classList.remove('visible');
      }, hideAfter);
      if (event.mediaPlaybackMode === 'match_audio' && audio) {
        audio.addEventListener('loadedmetadata', () => {
          if (Number.isFinite(audio.duration) && audio.duration > 0) {
            clearTimeout(hideTimer);
            hideTimer = setTimeout(() => {
              clearFrameTimer();
              stage.classList.remove('visible');
            }, Math.max(Math.max(1, event.duration || 5) * 1000, audio.duration * 1000));
          }
        }, { once: true });
      }
    }
    const source = new EventSource('/events');
    source.addEventListener('playback', (message) => {
      try {
        play(JSON.parse(message.data));
      } catch (_) {}
    });
  </script>
</body>
</html>`

func loadEnabledMediaActions() ([]mediaactions.Action, error) {
	path, err := mediaActionsPath()
	if err != nil {
		return nil, err
	}
	actions, err := mediaactions.Load(path)
	if err != nil {
		return nil, err
	}
	out := make([]mediaactions.Action, 0, len(actions))
	for _, action := range actions {
		if action.Enabled {
			out = append(out, action)
		}
	}
	return out, nil
}

func mediaActionsPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "Starsong Tools", "LupusAria", "media-actions.json"), nil
}

func mediaActionsRoot() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "Starsong Tools", "LupusAria", "MediaActions"), nil
}

func mediaActionSettingsFromActions(actions []mediaactions.Action) []MediaActionSettings {
	out := make([]MediaActionSettings, 0, len(actions))
	for _, action := range actions {
		out = append(out, mediaActionSettingsFromAction(action))
	}
	return out
}

func mediaActionSettingsFromAction(action mediaactions.Action) MediaActionSettings {
	action = mediaactions.Normalize(action)
	return MediaActionSettings{
		ID:                action.ID,
		Name:              action.Name,
		Enabled:           action.Enabled,
		Trigger:           action.Trigger,
		RewardID:          action.RewardID,
		RewardTitle:       action.RewardTitle,
		Media:             mediaAssetSettingsFromAssets(action.Media),
		Sounds:            mediaAssetSettingsFromAssets(action.Sounds),
		Duration:          action.Duration,
		Position:          action.Position,
		Scale:             action.Scale,
		Animation:         action.Animation,
		MediaPlaybackMode: action.MediaPlaybackMode,
	}
}

func mediaActionsFromSettings(settings []MediaActionSettings) []mediaactions.Action {
	out := make([]mediaactions.Action, 0, len(settings))
	for _, action := range settings {
		out = append(out, mediaActionFromSettings(action))
	}
	return out
}

func mediaActionFromSettings(action MediaActionSettings) mediaactions.Action {
	return mediaactions.Normalize(mediaactions.Action{
		ID:                action.ID,
		Name:              action.Name,
		Enabled:           action.Enabled,
		Trigger:           action.Trigger,
		RewardID:          action.RewardID,
		RewardTitle:       action.RewardTitle,
		Media:             mediaAssetsFromSettings(action.Media),
		Sounds:            mediaAssetsFromSettings(action.Sounds),
		Duration:          action.Duration,
		Position:          action.Position,
		Scale:             action.Scale,
		Animation:         action.Animation,
		MediaPlaybackMode: action.MediaPlaybackMode,
	})
}

func mediaAssetSettingsFromAssets(assets []mediaactions.Asset) []MediaAssetSettings {
	out := make([]MediaAssetSettings, 0, len(assets))
	for _, asset := range assets {
		durationMS := asset.DurationMS
		if durationMS == 0 && strings.EqualFold(filepath.Ext(asset.Path), ".gif") {
			if detected, err := mediaactions.GIFDurationMS(asset.Path); err == nil {
				durationMS = detected
			}
		}
		out = append(out, MediaAssetSettings{
			ID:                     asset.ID,
			Filename:               asset.Filename,
			Path:                   asset.Path,
			DurationMS:             durationMS,
			MediaPlaybackMode:      asset.MediaPlaybackMode,
			ExcludeFromGifRotation: asset.ExcludeFromGifRotation,
		})
	}
	return out
}

func mediaAssetsFromSettings(settings []MediaAssetSettings) []mediaactions.Asset {
	out := make([]mediaactions.Asset, 0, len(settings))
	for _, asset := range settings {
		out = append(out, mediaactions.Asset{
			ID:                     asset.ID,
			Filename:               asset.Filename,
			Path:                   asset.Path,
			DurationMS:             asset.DurationMS,
			MediaPlaybackMode:      asset.MediaPlaybackMode,
			ExcludeFromGifRotation: asset.ExcludeFromGifRotation,
		})
	}
	return out
}

func mediaPlaybackFromPlayback(root string, playback mediaactions.Playback) (MediaActionPlayback, error) {
	payload := MediaActionPlayback{
		ActionID:          playback.ActionID,
		Name:              playback.Name,
		Duration:          playback.Duration,
		Position:          playback.Position,
		Scale:             playback.Scale,
		Animation:         playback.Animation,
		MediaPlaybackMode: playback.MediaPlaybackMode,
	}
	if playback.Media != nil {
		clip, err := mediaPlaybackClipFromAsset(root, *playback.Media, playback.MediaPlaybackMode == mediaactions.PlaybackMatchAudio)
		if err != nil {
			return MediaActionPlayback{}, err
		}
		payload.Media = &clip.Media
		payload.MediaDataURL = clip.MediaDataURL
		payload.MediaDurationMS = clip.MediaDurationMS
		payload.MediaFrameDataURLs = clip.MediaFrameDataURLs
		payload.MediaFrameDelaysMS = clip.MediaFrameDelaysMS
	}
	if len(playback.MediaSequence) > 0 {
		payload.MediaClips = make([]MediaPlaybackClip, 0, len(playback.MediaSequence))
		for _, media := range playback.MediaSequence {
			clip, err := mediaPlaybackClipFromAsset(root, media, false)
			if err != nil {
				return MediaActionPlayback{}, err
			}
			payload.MediaClips = append(payload.MediaClips, clip)
		}
	}
	if playback.Sound != nil {
		asset := MediaAssetSettings{ID: playback.Sound.ID, Filename: playback.Sound.Filename, Path: playback.Sound.Path, DurationMS: playback.Sound.DurationMS, MediaPlaybackMode: playback.Sound.MediaPlaybackMode, ExcludeFromGifRotation: playback.Sound.ExcludeFromGifRotation}
		payload.Sound = &asset
		dataURL, err := mediaAssetDataURL(root, playback.Sound.Path)
		if err != nil {
			return MediaActionPlayback{}, err
		}
		payload.SoundDataURL = dataURL
	}
	return payload, nil
}

func mediaPlaybackClipFromAsset(root string, media mediaactions.Asset, includeFrames bool) (MediaPlaybackClip, error) {
	asset := MediaAssetSettings{ID: media.ID, Filename: media.Filename, Path: media.Path, DurationMS: media.DurationMS, MediaPlaybackMode: media.MediaPlaybackMode, ExcludeFromGifRotation: media.ExcludeFromGifRotation}
	dataURL, err := mediaAssetDataURL(root, media.Path)
	if err != nil {
		return MediaPlaybackClip{}, err
	}
	clip := MediaPlaybackClip{
		Media:           asset,
		MediaDataURL:    dataURL,
		MediaDurationMS: media.DurationMS,
	}
	if includeFrames && strings.EqualFold(filepath.Ext(media.Path), ".gif") {
		frames, delays, duration, err := mediaGIFFramesDataURLs(root, media.Path)
		if err != nil {
			return MediaPlaybackClip{}, err
		}
		clip.MediaFrameDataURLs = frames
		clip.MediaFrameDelaysMS = delays
		clip.MediaDurationMS = duration
		clip.Media.DurationMS = duration
	}
	return clip, nil
}

func mediaGIFFramesDataURLs(root, path string) ([]string, []int, int, error) {
	if !mediaactions.IsAssetUnderRoot(root, path) {
		return nil, nil, 0, errors.New("asset is outside the media actions folder")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, err
	}
	defer file.Close()
	decoded, err := gif.DecodeAll(file)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(decoded.Image) == 0 {
		return nil, nil, 0, errors.New("gif has no frames")
	}

	bounds := image.Rect(0, 0, decoded.Config.Width, decoded.Config.Height)
	canvas := image.NewRGBA(bounds)
	frames := make([]string, 0, len(decoded.Image))
	delays := make([]int, 0, len(decoded.Image))
	duration := 0
	for index, frame := range decoded.Image {
		draw.Draw(canvas, frame.Bounds(), frame, image.Point{}, draw.Over)
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, canvas); err != nil {
			return nil, nil, 0, err
		}
		delay := 100
		if index < len(decoded.Delay) && decoded.Delay[index] > 0 {
			delay = decoded.Delay[index] * 10
		}
		frames = append(frames, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(buffer.Bytes()))
		delays = append(delays, delay)
		duration += delay
	}
	return frames, delays, duration, nil
}

func mediaAssetDataURL(root, path string) (string, error) {
	if !mediaactions.IsAssetUnderRoot(root, path) {
		return "", errors.New("asset is outside the media actions folder")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

func minInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func formatLoginList(value string) string {
	seen := map[string]bool{}
	var out []string
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		login := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(field, "@")))
		if login == "" || seen[login] {
			continue
		}
		seen[login] = true
		out = append(out, login)
	}
	return strings.Join(out, ",")
}
