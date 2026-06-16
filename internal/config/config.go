package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
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
}

type TwitchConfig struct {
	BotUsername       string
	OAuthToken        string
	Channel           string
	ClientID          string
	ClientSecret      string
	RefreshToken      string
	TokenStatePath    string
	AdsOAuthToken     string
	AdsRefreshToken   string
	AdsTokenStatePath string
}

type AIConfig struct {
	Provider              string
	APIKey                string
	BaseURL               string
	Model                 string
	InputPricePerMillion  float64
	OutputPricePerMillion float64
}

type BotConfig struct {
	Name               string
	Personality        string
	KnowledgePath      string
	EnableMentions     bool
	EnableAsk          bool
	EnableLurk         bool
	EnableCommands     bool
	EnableReset        bool
	MaxContextMessages int
	StreamContextTTL   time.Duration
	GlobalCooldown     time.Duration
	UserCooldown       time.Duration
	DailyBudgetUSD     float64
	MonthlyBudgetUSD   float64
	MaxRequestsPerHour int
	BudgetStatePath    string
}

type RecentStreamersConfig struct {
	Enabled             bool
	MinWatch            time.Duration
	RecentWindow        time.Duration
	PageSize            int
	ShoutoutDelay       time.Duration
	CacheTTL            time.Duration
	ChatterPollInterval time.Duration
}

type AdAlertsConfig struct {
	Enabled        bool
	WarningLead    time.Duration
	PollInterval   time.Duration
	WarningMessage string
	StartMessage   string
	EndMessage     string
}

func Load(envPath string) (Config, error) {
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

	aiProvider := strings.ToLower(get(values, "AI_PROVIDER", "mock"))
	aiModel := get(values, "AI_MODEL", "gpt-4.1-mini")
	if aiProvider == "gemini" {
		aiModel = get(values, "GEMINI_MODEL", "gemini-3.1-flash-lite")
	}
	inputPrice, outputPrice := defaultAIPrices(aiProvider, aiModel)

	cfg := Config{
		Twitch: TwitchConfig{
			BotUsername:       get(values, "TWITCH_BOT_USERNAME", ""),
			OAuthToken:        get(values, "TWITCH_OAUTH_TOKEN", ""),
			Channel:           normalizeChannel(get(values, "TWITCH_CHANNEL", "")),
			ClientID:          get(values, "TWITCH_CLIENT_ID", ""),
			ClientSecret:      get(values, "TWITCH_CLIENT_SECRET", ""),
			RefreshToken:      get(values, "TWITCH_REFRESH_TOKEN", ""),
			TokenStatePath:    get(values, "TWITCH_TOKEN_STATE_PATH", ".lupusaria-twitch-token.json"),
			AdsOAuthToken:     get(values, "TWITCH_ADS_OAUTH_TOKEN", ""),
			AdsRefreshToken:   get(values, "TWITCH_ADS_REFRESH_TOKEN", ""),
			AdsTokenStatePath: get(values, "TWITCH_ADS_TOKEN_STATE_PATH", ".lupusaria-twitch-ads-token.json"),
		},
		AI: AIConfig{
			Provider:              aiProvider,
			APIKey:                get(values, "AI_API_KEY", get(values, "GEMINI_API_KEY", "")),
			BaseURL:               strings.TrimRight(get(values, "AI_BASE_URL", "https://api.openai.com/v1"), "/"),
			Model:                 aiModel,
			InputPricePerMillion:  getFloat(values, "AI_INPUT_PRICE_PER_1M_TOKENS", inputPrice),
			OutputPricePerMillion: getFloat(values, "AI_OUTPUT_PRICE_PER_1M_TOKENS", outputPrice),
		},
		Bot: BotConfig{
			Name:               get(values, "BOT_NAME", "LupusAria"),
			Personality:        get(values, "BOT_PERSONALITY", "Warm, steady, lightly playful, and useful. You fit into live Twitch chat without dominating it."),
			KnowledgePath:      get(values, "BOT_KNOWLEDGE_PATH", "docs/knowledge/ursa.md"),
			EnableMentions:     getBool(values, "ENABLE_MENTION_RESPONSES", true),
			EnableAsk:          getBool(values, "ENABLE_ASK_COMMAND", true),
			EnableLurk:         getBool(values, "ENABLE_LURK_COMMAND", true),
			EnableCommands:     getBool(values, "ENABLE_COMMANDS_COMMAND", true),
			EnableReset:        getBool(values, "ENABLE_RESET_COMMAND", true),
			MaxContextMessages: getInt(values, "MAX_CONTEXT_MESSAGES", 30),
			StreamContextTTL:   time.Duration(getInt(values, "STREAM_CONTEXT_TTL_SECONDS", 120)) * time.Second,
			GlobalCooldown:     time.Duration(getInt(values, "GLOBAL_COOLDOWN_SECONDS", 6)) * time.Second,
			UserCooldown:       time.Duration(getInt(values, "USER_COOLDOWN_SECONDS", 20)) * time.Second,
			DailyBudgetUSD:     getFloat(values, "DAILY_AI_BUDGET_USD", 0.50),
			MonthlyBudgetUSD:   getFloat(values, "MONTHLY_AI_BUDGET_USD", 5),
			MaxRequestsPerHour: getInt(values, "MAX_AI_REQUESTS_PER_HOUR", 30),
			BudgetStatePath:    get(values, "AI_BUDGET_STATE_PATH", ".lupusaria-budget.json"),
		},
		RecentStreamers: RecentStreamersConfig{
			Enabled:             getBool(values, "AUTOSO_ENABLED", true),
			MinWatch:            time.Duration(getInt(values, "RECENT_STREAMER_MIN_WATCH_MINUTES", 15)) * time.Minute,
			RecentWindow:        time.Duration(getInt(values, "RECENT_STREAMER_RECENT_DAYS", 14)) * 24 * time.Hour,
			PageSize:            getInt(values, "RECENT_STREAMER_PAGE_SIZE", 5),
			ShoutoutDelay:       time.Duration(getInt(values, "RECENT_STREAMER_SHOUTOUT_DELAY_SECONDS", 2)) * time.Second,
			CacheTTL:            time.Duration(getInt(values, "RECENT_STREAMER_CACHE_HOURS", 6)) * time.Hour,
			ChatterPollInterval: time.Duration(getInt(values, "RECENT_STREAMER_CHATTERS_POLL_SECONDS", 60)) * time.Second,
		},
		AdAlerts: AdAlertsConfig{
			Enabled:        getBool(values, "AD_ALERTS_ENABLED", false),
			WarningLead:    adWarningLead(values),
			PollInterval:   time.Duration(getInt(values, "AD_ALERT_POLL_SECONDS", 30)) * time.Second,
			WarningMessage: get(values, "AD_ALERT_WARNING_MESSAGE", "Heads up: ads are scheduled in about %s."),
			StartMessage:   get(values, "AD_ALERT_START_MESSAGE", "Ad break starting now. Good moment to stretch, hydrate, and rest your eyes."),
			EndMessage:     get(values, "AD_ALERT_END_MESSAGE", "Welcome back. Ads should be done now."),
		},
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
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
	if cfg.Twitch.AdsRefreshToken != "" {
		if cfg.Twitch.ClientID == "" {
			missing = append(missing, "TWITCH_CLIENT_ID")
		}
		if cfg.Twitch.ClientSecret == "" {
			missing = append(missing, "TWITCH_CLIENT_SECRET")
		}
	}
	if cfg.AI.Provider == "openai-compatible" && cfg.AI.APIKey == "" {
		missing = append(missing, "AI_API_KEY")
	}
	if cfg.AI.Provider == "gemini" && cfg.AI.APIKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required configuration: %s", strings.Join(missing, ", "))
	}
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
	return nil
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

func defaultAIPrices(provider, model string) (float64, float64) {
	normalized := strings.ToLower(provider + " " + model)
	switch {
	case strings.Contains(normalized, "mock"):
		return 0, 0
	case strings.Contains(normalized, "flash-lite"):
		return 0.25, 1.50
	case strings.Contains(normalized, "gemini") && strings.Contains(normalized, "flash"):
		return 1.50, 9.00
	default:
		return 0, 0
	}
}
