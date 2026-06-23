package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Twitch          TwitchConfig
	AI              AIConfig
	Bot             BotConfig
	RecentStreamers RecentStreamersConfig
	AdAlerts        AdAlertsConfig
	Announcements   AnnouncementsConfig
}

type TwitchConfig struct {
	BotUsername       string
	OAuthToken        string
	Channel           string
	ClientID          string
	ClientSecret      string
	RefreshToken      string
	TokenStatePath    string
	AppTokenStatePath string
	AdsClientID       string
	AdsClientSecret   string
	AdsOAuthToken     string
	AdsRefreshToken   string
	AdsTokenStatePath string
}

type AIConfig struct {
	Provider              string
	APIKey                string
	GeminiAPIKey          string
	BaseURL               string
	Model                 string
	GeminiModel           string
	Fallback              *AIConfig
	MaxOutputTokens       int
	GeminiThinkingLevel   string
	MaxRetries            int
	InputPricePerMillion  float64
	OutputPricePerMillion float64
}

type BotConfig struct {
	Name               string
	StreamerName       string
	StreamerPronouns   string
	Personality        string
	KnowledgePath      string
	EnableMentions     bool
	EnableAsk          bool
	EnableLurk         bool
	EnableCommands     bool
	EnableReset        bool
	MentionPermission  string
	AskPermission      string
	LurkPermission     string
	GamePermission     string
	CommandsPermission string
	ResetPermission    string
	MaxContextMessages int
	StreamContextTTL   time.Duration
	GlobalCooldown     time.Duration
	UserCooldown       time.Duration
	DailyBudgetUSD     float64
	MonthlyBudgetUSD   float64
	MaxRequestsPerHour int
	BudgetStatePath    string
	ChatLogPath        string
	EmoteCachePath     string
	EnableEmoteContext bool
	SnapshotCrop       SnapshotCropConfig
}

type SnapshotCropConfig struct {
	Enabled bool
	X       float64
	Y       float64
	Width   float64
	Height  float64
}

type RecentStreamersConfig struct {
	Enabled              bool
	Permission           string
	SORoulettePermission string
	RouletteStreamers    []string
	MinWatch             time.Duration
	RecentWindow         time.Duration
	PageSize             int
	ShoutoutDelay        time.Duration
	CacheTTL             time.Duration
	ChatterPollInterval  time.Duration
}

type AdAlertsConfig struct {
	Enabled        bool
	WarningLead    time.Duration
	PollInterval   time.Duration
	WarningMessage string
	StartMessage   string
	EndMessage     string
}

type AnnouncementsConfig struct {
	Enabled      bool
	Path         string
	PollInterval time.Duration
}

const minRecentStreamerShoutoutDelay = 5 * time.Second

func Load(envPath string) (Config, error) {
	return load(envPath, true)
}

func LoadPartial(envPath string) (Config, error) {
	return load(envPath, false)
}

func load(envPath string, validateRequired bool) (Config, error) {
	values := readEnvironment()

	if _, err := os.Stat(envPath); err == nil {
		fileValues, err := readEnvFile(envPath)
		if err != nil {
			return Config{}, err
		}
		for key, value := range fileValues {
			if _, exists := values[key]; !exists {
				values[key] = value
			}
		}
	}
	baseDir := filepath.Dir(envPath)

	aiProvider := strings.ToLower(get(values, "AI_PROVIDER", "mock"))
	geminiModel := get(values, "GEMINI_MODEL", "gemini-3.1-flash-lite")
	geminiAPIKey := get(values, "GEMINI_API_KEY", "")
	aiModel := get(values, "AI_MODEL", "")
	if aiProvider == "gemini" {
		aiModel = geminiModel
	}
	inputPrice, outputPrice := defaultAIPrices(aiProvider, aiModel)
	fallbackAI := fallbackAIConfig(values, aiProvider, geminiAPIKey, geminiModel)

	cfg := Config{
		Twitch: TwitchConfig{
			BotUsername:       get(values, "TWITCH_BOT_USERNAME", ""),
			OAuthToken:        get(values, "TWITCH_OAUTH_TOKEN", ""),
			Channel:           normalizeChannel(get(values, "TWITCH_CHANNEL", "")),
			ClientID:          get(values, "TWITCH_CLIENT_ID", ""),
			ClientSecret:      get(values, "TWITCH_CLIENT_SECRET", ""),
			RefreshToken:      get(values, "TWITCH_REFRESH_TOKEN", ""),
			TokenStatePath:    resolveLocalPath(baseDir, get(values, "TWITCH_TOKEN_STATE_PATH", ".lupusaria-twitch-token.json")),
			AppTokenStatePath: resolveLocalPath(baseDir, get(values, "TWITCH_APP_TOKEN_STATE_PATH", ".lupusaria-twitch-app-token.json")),
			AdsClientID:       get(values, "TWITCH_ADS_CLIENT_ID", get(values, "TWITCH_CLIENT_ID", "")),
			AdsClientSecret:   get(values, "TWITCH_ADS_CLIENT_SECRET", get(values, "TWITCH_CLIENT_SECRET", "")),
			AdsOAuthToken:     get(values, "TWITCH_ADS_OAUTH_TOKEN", ""),
			AdsRefreshToken:   get(values, "TWITCH_ADS_REFRESH_TOKEN", ""),
			AdsTokenStatePath: resolveLocalPath(baseDir, get(values, "TWITCH_ADS_TOKEN_STATE_PATH", ".lupusaria-twitch-ads-token.json")),
		},
		AI: AIConfig{
			Provider:              aiProvider,
			APIKey:                get(values, "AI_API_KEY", geminiAPIKey),
			GeminiAPIKey:          geminiAPIKey,
			BaseURL:               strings.TrimRight(get(values, "AI_BASE_URL", "http://localhost:11434/v1"), "/"),
			Model:                 aiModel,
			GeminiModel:           geminiModel,
			Fallback:              fallbackAI,
			MaxOutputTokens:       getInt(values, "AI_MAX_OUTPUT_TOKENS", 1024),
			GeminiThinkingLevel:   get(values, "GEMINI_THINKING_LEVEL", "high"),
			MaxRetries:            getInt(values, "AI_MAX_RETRIES", 3),
			InputPricePerMillion:  getFloat(values, "AI_INPUT_PRICE_PER_1M_TOKENS", inputPrice),
			OutputPricePerMillion: getFloat(values, "AI_OUTPUT_PRICE_PER_1M_TOKENS", outputPrice),
		},
		Bot: BotConfig{
			Name:               get(values, "BOT_NAME", "LupusAria"),
			StreamerName:       get(values, "STREAMER_NAME", "the streamer"),
			StreamerPronouns:   get(values, "STREAMER_PRONOUNS", "they/them"),
			Personality:        get(values, "BOT_PERSONALITY", "Relaxed, warm, lightly playful, and useful. You fit into live Twitch chat without dominating it."),
			KnowledgePath:      resolveLocalPath(baseDir, get(values, "BOT_KNOWLEDGE_PATH", ".lupusaria-knowledge.md")),
			EnableMentions:     getBool(values, "ENABLE_MENTION_RESPONSES", true),
			EnableAsk:          getBool(values, "ENABLE_ASK_COMMAND", true),
			EnableLurk:         getBool(values, "ENABLE_LURK_COMMAND", true),
			EnableCommands:     getBool(values, "ENABLE_COMMANDS_COMMAND", true),
			EnableReset:        getBool(values, "ENABLE_RESET_COMMAND", true),
			MentionPermission:  commandPermission(values, "MENTION_PERMISSION", "everyone"),
			AskPermission:      commandPermission(values, "ASK_COMMAND_PERMISSION", "everyone"),
			LurkPermission:     commandPermission(values, "LURK_COMMAND_PERMISSION", "everyone"),
			GamePermission:     commandPermission(values, "GAME_COMMAND_PERMISSION", "everyone"),
			CommandsPermission: commandPermission(values, "COMMANDS_COMMAND_PERMISSION", "everyone"),
			ResetPermission:    commandPermission(values, "RESET_COMMAND_PERMISSION", "broadcaster"),
			MaxContextMessages: getInt(values, "MAX_CONTEXT_MESSAGES", 30),
			StreamContextTTL:   time.Duration(getInt(values, "STREAM_CONTEXT_TTL_SECONDS", 120)) * time.Second,
			GlobalCooldown:     time.Duration(getInt(values, "GLOBAL_COOLDOWN_SECONDS", 6)) * time.Second,
			UserCooldown:       time.Duration(getInt(values, "USER_COOLDOWN_SECONDS", 20)) * time.Second,
			DailyBudgetUSD:     getFloat(values, "DAILY_AI_BUDGET_USD", 0.50),
			MonthlyBudgetUSD:   getFloat(values, "MONTHLY_AI_BUDGET_USD", 5),
			MaxRequestsPerHour: getInt(values, "MAX_AI_REQUESTS_PER_HOUR", 30),
			BudgetStatePath:    resolveLocalPath(baseDir, get(values, "AI_BUDGET_STATE_PATH", ".lupusaria-budget.json")),
			ChatLogPath:        resolveLocalPath(baseDir, get(values, "CHAT_LOG_PATH", ".lupusaria-chat.jsonl")),
			EmoteCachePath:     resolveLocalPath(baseDir, get(values, "EMOTE_CACHE_PATH", ".lupusaria-emotes.json")),
			EnableEmoteContext: getBool(values, "ENABLE_EMOTE_CONTEXT", true),
			SnapshotCrop: SnapshotCropConfig{
				Enabled: getBool(values, "GAME_SNAPSHOT_CROP_ENABLED", true),
				X:       getFloat(values, "GAME_SNAPSHOT_CROP_X", 0.255),
				Y:       getFloat(values, "GAME_SNAPSHOT_CROP_Y", 0.085),
				Width:   getFloat(values, "GAME_SNAPSHOT_CROP_WIDTH", 0.73),
				Height:  getFloat(values, "GAME_SNAPSHOT_CROP_HEIGHT", 0.73),
			},
		},
		RecentStreamers: RecentStreamersConfig{
			Enabled:              getBool(values, "AUTOSO_ENABLED", true),
			Permission:           commandPermission(values, "AUTOSO_COMMAND_PERMISSION", "mods"),
			SORoulettePermission: commandPermission(values, "SO_ROULETTE_COMMAND_PERMISSION", get(values, "AUTOSO_COMMAND_PERMISSION", "mods")),
			RouletteStreamers:    parseLoginList(get(values, "SO_ROULETTE_STREAMERS", "")),
			MinWatch:             time.Duration(getInt(values, "RECENT_STREAMER_MIN_WATCH_MINUTES", 15)) * time.Minute,
			RecentWindow:         time.Duration(getInt(values, "RECENT_STREAMER_RECENT_DAYS", 14)) * 24 * time.Hour,
			PageSize:             getInt(values, "RECENT_STREAMER_PAGE_SIZE", 5),
			ShoutoutDelay:        time.Duration(getInt(values, "RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS", 5)) * time.Second,
			CacheTTL:             time.Duration(getInt(values, "RECENT_STREAMER_CACHE_HOURS", 6)) * time.Hour,
			ChatterPollInterval:  time.Duration(getInt(values, "RECENT_STREAMER_CHATTERS_POLL_SECONDS", 60)) * time.Second,
		},
		AdAlerts: AdAlertsConfig{
			Enabled:        getBool(values, "AD_ALERTS_ENABLED", false),
			WarningLead:    adWarningLead(values),
			PollInterval:   time.Duration(getInt(values, "AD_ALERT_POLL_SECONDS", 30)) * time.Second,
			WarningMessage: get(values, "AD_ALERT_WARNING_MESSAGE", "Heads up: ads are scheduled in about %s."),
			StartMessage:   get(values, "AD_ALERT_START_MESSAGE", "Ad break starting now. Good moment to stretch, hydrate, and rest your eyes."),
			EndMessage:     get(values, "AD_ALERT_END_MESSAGE", "Welcome back. Ads should be done now."),
		},
		Announcements: AnnouncementsConfig{
			Enabled:      getBool(values, "ANNOUNCEMENTS_ENABLED", false),
			Path:         resolveLocalPath(baseDir, get(values, "ANNOUNCEMENTS_PATH", ".lupusaria-announcements.json")),
			PollInterval: time.Duration(getInt(values, "ANNOUNCEMENT_POLL_SECONDS", 30)) * time.Second,
		},
	}

	if validateRequired {
		if err := validate(cfg); err != nil {
			return Config{}, err
		}
	}
	if err := validateRanges(cfg); err != nil {
		return Config{}, err
	}
	cfg = normalizeEffectiveValues(cfg)

	return cfg, nil
}

func normalizeEffectiveValues(cfg Config) Config {
	if cfg.RecentStreamers.ShoutoutDelay < minRecentStreamerShoutoutDelay {
		cfg.RecentStreamers.ShoutoutDelay = minRecentStreamerShoutoutDelay
	}
	return cfg
}

func validate(cfg Config) error {
	var missing []string
	if cfg.Twitch.BotUsername == "" {
		missing = append(missing, "TWITCH_BOT_USERNAME")
	}
	if cfg.Twitch.OAuthToken == "" && cfg.Twitch.RefreshToken == "" {
		missing = append(missing, "TWITCH_OAUTH_TOKEN or TWITCH_REFRESH_TOKEN")
	}
	if cfg.Twitch.Channel == "" {
		missing = append(missing, "TWITCH_CHANNEL")
	}
	if cfg.Twitch.RefreshToken != "" {
		if cfg.Twitch.ClientID == "" {
			missing = append(missing, "TWITCH_CLIENT_ID")
		}
		if cfg.Twitch.ClientSecret == "" {
			missing = append(missing, "TWITCH_CLIENT_SECRET")
		}
	}
	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsOAuthToken == "" && cfg.Twitch.AdsRefreshToken == "" && cfg.Twitch.RefreshToken == "" {
		missing = append(missing, "TWITCH_ADS_OAUTH_TOKEN or TWITCH_ADS_REFRESH_TOKEN")
	}
	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsClientID == "" {
		missing = append(missing, "TWITCH_ADS_CLIENT_ID or TWITCH_CLIENT_ID")
	}
	if cfg.Twitch.AdsRefreshToken != "" {
		if cfg.Twitch.AdsClientID == "" {
			missing = append(missing, "TWITCH_ADS_CLIENT_ID or TWITCH_CLIENT_ID")
		}
		if cfg.Twitch.AdsClientSecret == "" {
			missing = append(missing, "TWITCH_ADS_CLIENT_SECRET or TWITCH_CLIENT_SECRET")
		}
	}
	if cfg.AI.Provider == "openai-compatible" && cfg.AI.APIKey == "" {
		missing = append(missing, "AI_API_KEY")
	}
	if cfg.AI.Provider == "gemini" && cfg.AI.APIKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}
	if cfg.AI.Fallback != nil {
		switch cfg.AI.Fallback.Provider {
		case "openai-compatible":
			if cfg.AI.Fallback.APIKey == "" {
				missing = append(missing, "AI_FALLBACK_API_KEY")
			}
		case "gemini":
			if cfg.AI.Fallback.APIKey == "" {
				missing = append(missing, "GEMINI_API_KEY")
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
	return validateRanges(cfg)
}

func validateRanges(cfg Config) error {
	if cfg.Bot.MaxContextMessages < 1 {
		return errors.New("MAX_CONTEXT_MESSAGES must be greater than zero")
	}
	if cfg.Bot.StreamContextTTL < 0 {
		return errors.New("STREAM_CONTEXT_TTL_SECONDS must be zero or greater")
	}
	if cfg.Bot.DailyBudgetUSD < 0 || cfg.Bot.MonthlyBudgetUSD < 0 {
		return errors.New("AI budget values must be zero or greater")
	}
	if cfg.Bot.MaxRequestsPerHour < 0 {
		return errors.New("MAX_AI_REQUESTS_PER_HOUR must be zero or greater")
	}
	if cfg.AI.MaxOutputTokens < 1 {
		return errors.New("AI_MAX_OUTPUT_TOKENS must be greater than zero")
	}
	if cfg.AI.MaxRetries < 0 {
		return errors.New("AI_MAX_RETRIES must be zero or greater")
	}
	if cfg.Bot.SnapshotCrop.X < 0 || cfg.Bot.SnapshotCrop.Y < 0 || cfg.Bot.SnapshotCrop.Width <= 0 || cfg.Bot.SnapshotCrop.Height <= 0 {
		return errors.New("GAME_SNAPSHOT_CROP values must be non-negative, with width and height greater than zero")
	}
	if cfg.Bot.SnapshotCrop.X+cfg.Bot.SnapshotCrop.Width > 1.01 || cfg.Bot.SnapshotCrop.Y+cfg.Bot.SnapshotCrop.Height > 1.01 {
		return errors.New("GAME_SNAPSHOT_CROP values must fit within the image")
	}
	if cfg.RecentStreamers.MinWatch < 0 {
		return errors.New("RECENT_STREAMER_MIN_WATCH_MINUTES must be zero or greater")
	}
	if cfg.RecentStreamers.RecentWindow < 0 {
		return errors.New("RECENT_STREAMER_RECENT_DAYS must be zero or greater")
	}
	if cfg.RecentStreamers.PageSize < 1 {
		return errors.New("RECENT_STREAMER_PAGE_SIZE must be greater than zero")
	}
	if cfg.RecentStreamers.ShoutoutDelay < 0 {
		return errors.New("RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS must be zero or greater")
	}
	if cfg.RecentStreamers.CacheTTL < 0 {
		return errors.New("RECENT_STREAMER_CACHE_HOURS must be zero or greater")
	}
	if cfg.RecentStreamers.ChatterPollInterval < 0 {
		return errors.New("RECENT_STREAMER_CHATTERS_POLL_SECONDS must be zero or greater")
	}
	if cfg.AdAlerts.WarningLead < 0 {
		return errors.New("AD_ALERT_WARNING_MINUTES must be zero or greater")
	}
	if cfg.AdAlerts.PollInterval < 0 {
		return errors.New("AD_ALERT_POLL_SECONDS must be zero or greater")
	}
	if cfg.Announcements.PollInterval < 0 {
		return errors.New("ANNOUNCEMENT_POLL_SECONDS must be zero or greater")
	}
	return nil
}

func resolveLocalPath(baseDir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}

func commandPermission(values map[string]string, key, fallback string) string {
	return normalizeCommandPermission(get(values, key, fallback), normalizeCommandPermission(fallback, "everyone"))
}

func normalizeCommandPermission(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "everyone", "all", "public", "viewer", "viewers":
		return "everyone"
	case "mods", "mod", "moderator", "moderators", "mod-or-broadcaster", "mods+broadcaster":
		return "mods"
	case "broadcaster", "streamer", "owner":
		return "broadcaster"
	default:
		return fallback
	}
}

func adWarningLead(values map[string]string) time.Duration {
	if _, ok := values["AD_ALERT_WARNING_MINUTES"]; ok {
		return time.Duration(getInt(values, "AD_ALERT_WARNING_MINUTES", 5)) * time.Minute
	}
	return time.Duration(getInt(values, "AD_ALERT_WARNING_SECONDS", 300)) * time.Second
}

func readEnvironment() map[string]string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

func readEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func get(values map[string]string, key, fallback string) string {
	if value, ok := values[key]; ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func getInt(values map[string]string, key string, fallback int) int {
	raw := get(values, key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getFloat(values map[string]string, key string, fallback float64) float64 {
	raw := get(values, key, "")
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

func getBool(values map[string]string, key string, fallback bool) bool {
	raw := strings.ToLower(get(values, key, ""))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func normalizeChannel(channel string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(channel)), "#")
}

func parseLoginList(value string) []string {
	seen := map[string]bool{}
	var out []string
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		login := normalizeChannel(strings.TrimPrefix(strings.TrimSpace(field), "@"))
		if login == "" || seen[login] {
			continue
		}
		seen[login] = true
		out = append(out, login)
	}
	return out
}

func defaultAIPrices(provider, model string) (float64, float64) {
	normalized := strings.ToLower(provider + " " + model)
	switch {
	case strings.Contains(normalized, "mock"):
		return 0, 0
	case strings.Contains(normalized, "deepseek"):
		return 0.07, 0.27
	case strings.Contains(normalized, "flash-lite"):
		return 0.25, 1.50
	case strings.Contains(normalized, "gemini") && strings.Contains(normalized, "flash"):
		return 1.50, 9.00
	default:
		return 0, 0
	}
}

func fallbackAIConfig(values map[string]string, primaryProvider, geminiAPIKey, geminiModel string) *AIConfig {
	provider := strings.ToLower(get(values, "AI_FALLBACK_PROVIDER", ""))
	if provider == "" && primaryProvider == "openai-compatible" && geminiAPIKey != "" {
		provider = "gemini"
	}
	if provider == "" || provider == "mock" || provider == primaryProvider {
		return nil
	}

	model := get(values, "AI_FALLBACK_MODEL", "llama3.1:8b")
	apiKey := get(values, "AI_FALLBACK_API_KEY", "")
	baseURL := strings.TrimRight(get(values, "AI_FALLBACK_BASE_URL", "http://localhost:11434/v1"), "/")
	if provider == "gemini" {
		model = get(values, "AI_FALLBACK_MODEL", geminiModel)
		apiKey = get(values, "AI_FALLBACK_API_KEY", geminiAPIKey)
		baseURL = ""
	}
	inputPrice, outputPrice := defaultAIPrices(provider, model)
	return &AIConfig{
		Provider:              provider,
		APIKey:                apiKey,
		GeminiAPIKey:          geminiAPIKey,
		BaseURL:               baseURL,
		Model:                 model,
		GeminiModel:           geminiModel,
		MaxOutputTokens:       getInt(values, "AI_FALLBACK_MAX_OUTPUT_TOKENS", getInt(values, "AI_MAX_OUTPUT_TOKENS", 1024)),
		GeminiThinkingLevel:   get(values, "GEMINI_THINKING_LEVEL", "high"),
		MaxRetries:            getInt(values, "AI_FALLBACK_MAX_RETRIES", getInt(values, "AI_MAX_RETRIES", 3)),
		InputPricePerMillion:  getFloat(values, "AI_FALLBACK_INPUT_PRICE_PER_1M_TOKENS", inputPrice),
		OutputPricePerMillion: getFloat(values, "AI_FALLBACK_OUTPUT_PRICE_PER_1M_TOKENS", outputPrice),
	}
}
