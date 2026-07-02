package botrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"lupusaria/internal/adalerts"
	"lupusaria/internal/ai"
	"lupusaria/internal/announcements"
	"lupusaria/internal/bot"
	"lupusaria/internal/config"
	"lupusaria/internal/knowledge"
	"lupusaria/internal/recentstreamers"
	"lupusaria/internal/twitch"
)

type Options struct {
	MediaActionRedeem func(context.Context, twitch.ChannelPointRedeemEvent)
}

func Run(ctx context.Context, envPath string, logger *slog.Logger) error {
	return RunWithOptions(ctx, envPath, logger, Options{})
}

func RunWithOptions(ctx context.Context, envPath string, logger *slog.Logger, options Options) error {
	if logger == nil {
		logger = slog.Default()
	}

	cfg, err := config.Load(envPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	aiClient, err := ai.NewClient(cfg.AI)
	if err != nil {
		return fmt.Errorf("initialize ai client: %w", err)
	}

	if err := knowledge.EnsureFile(cfg.Bot.KnowledgePath); err != nil {
		logger.Warn("failed to create default knowledge file; continuing without it", "path", cfg.Bot.KnowledgePath, "error", err)
	}
	knowledgeBase, err := knowledge.Load(cfg.Bot.KnowledgePath)
	if err != nil {
		logger.Warn("failed to load knowledge base; continuing without it", "path", cfg.Bot.KnowledgePath, "error", err)
	} else {
		logger.Info("loaded knowledge base", "path", cfg.Bot.KnowledgePath, "sections", len(knowledgeBase.Sections))
	}

	if cfg.Twitch.RefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     cfg.Twitch.ClientID,
			ClientSecret:                 cfg.Twitch.ClientSecret,
			RefreshToken:                 cfg.Twitch.RefreshToken,
			StatePath:                    cfg.Twitch.TokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.TokenStatePath),
		}).Refresh(ctx)
		if err != nil {
			logger.Warn("failed to refresh twitch token; using configured access token", "error", err)
		} else {
			cfg.Twitch.OAuthToken = "oauth:" + tokenSet.AccessToken
			logger.Info("refreshed twitch access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}

	if (cfg.AdAlerts.Enabled || options.MediaActionRedeem != nil) && cfg.Twitch.AdsRefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:                     cfg.Twitch.AdsClientID,
			ClientSecret:                 cfg.Twitch.AdsClientSecret,
			RefreshToken:                 cfg.Twitch.AdsRefreshToken,
			StatePath:                    cfg.Twitch.AdsTokenStatePath,
			PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.AdsTokenStatePath),
		}).Refresh(ctx)
		if err != nil {
			logger.Warn("failed to refresh twitch ads token; using configured ads access token if available", "error", err)
		} else {
			cfg.Twitch.AdsOAuthToken = "oauth:" + tokenSet.AccessToken
			cfg.Twitch.AdsRefreshToken = tokenSet.RefreshToken
			logger.Info("refreshed twitch broadcaster access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}
	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsOAuthToken == "" {
		cfg.Twitch.AdsOAuthToken = cfg.Twitch.OAuthToken
	}

	var streamProvider bot.StreamInfoProvider
	var helix *twitch.HelixClient
	var recentService *recentstreamers.Service
	var announcementService *announcements.Service
	var adService *adalerts.Service
	var broadcasterID string
	var moderatorID string
	var chatSendToken string
	var channelEmotes []twitch.Emote
	var adBreaks chan twitch.AdBreakEvent
	var redeems chan twitch.ChannelPointRedeemEvent
	if cfg.Twitch.ClientID != "" {
		helix = twitch.NewHelixClient(cfg.Twitch.ClientID, cfg.Twitch.OAuthToken)
		streamProvider = helix
		broadcasterID, moderatorID = resolveRecentStreamerIDs(ctx, helix, cfg.Twitch.Channel, cfg.Twitch.BotUsername, logger)
		chatSendToken = appAccessToken(ctx, cfg, logger)
		if cfg.Bot.EnableEmoteContext && broadcasterID != "" {
			emotes, err := helix.GetChannelEmotes(ctx, broadcasterID)
			if err != nil {
				logger.Warn("failed to load channel emote catalog; continuing with message emote metadata only", "error", err)
			} else {
				channelEmotes = convertChannelEmotes(emotes)
				logger.Info("loaded channel emote catalog", "count", len(channelEmotes))
			}
		}
	}

	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsOAuthToken != "" {
		adBreaks = make(chan twitch.AdBreakEvent, 16)
	}
	if options.MediaActionRedeem != nil {
		redeems = make(chan twitch.ChannelPointRedeemEvent, 32)
	}

	chat := bot.WithChatLogging(newTwitchChat(cfg, broadcasterID, moderatorID, chatSendToken, adBreaks, redeems, logger), cfg.Bot.ChatLogPath, logger, cfg.Bot.Name)

	if helix != nil && cfg.RecentStreamers.Enabled {
		recentService = recentstreamers.New(recentstreamers.Config{
			Channel:              cfg.Twitch.Channel,
			Permission:           cfg.RecentStreamers.Permission,
			SORoulettePermission: cfg.RecentStreamers.SORoulettePermission,
			RouletteStreamers:    cfg.RecentStreamers.RouletteStreamers,
			BroadcasterID:        broadcasterID,
			ModeratorID:          moderatorID,
			MinWatch:             cfg.RecentStreamers.MinWatch,
			RecentWindow:         cfg.RecentStreamers.RecentWindow,
			PageSize:             cfg.RecentStreamers.PageSize,
			ShoutoutDelay:        cfg.RecentStreamers.ShoutoutDelay,
			CacheTTL:             cfg.RecentStreamers.CacheTTL,
			ChatterPollInterval:  cfg.RecentStreamers.ChatterPollInterval,
		}, chat, helix, logger)
	}

	announcementItems, err := announcements.Load(cfg.Announcements.Path)
	if err != nil {
		logger.Warn("failed to load announcements; continuing without them", "path", cfg.Announcements.Path, "error", err)
	} else if cfg.Announcements.Enabled {
		announcementService = announcements.New(announcements.Config{
			Enabled:      cfg.Announcements.Enabled,
			Channel:      cfg.Twitch.Channel,
			PollInterval: cfg.Announcements.PollInterval,
			Items:        announcementItems,
		}, chat, streamProvider, logger)
		logger.Info("loaded announcements", "path", cfg.Announcements.Path, "count", len(announcementItems))
	}

	runner := bot.New(bot.Config{
		Name:                  cfg.Bot.Name,
		Channel:               cfg.Twitch.Channel,
		StreamerName:          cfg.Bot.StreamerName,
		StreamerPronouns:      cfg.Bot.StreamerPronouns,
		Personality:           cfg.Bot.Personality,
		EnableMentions:        cfg.Bot.EnableMentions,
		EnableAsk:             cfg.Bot.EnableAsk,
		EnableLurk:            cfg.Bot.EnableLurk,
		EnableCommands:        cfg.Bot.EnableCommands,
		EnableReset:           cfg.Bot.EnableReset,
		MentionPermission:     cfg.Bot.MentionPermission,
		AskPermission:         cfg.Bot.AskPermission,
		LurkPermission:        cfg.Bot.LurkPermission,
		GamePermission:        cfg.Bot.GamePermission,
		CommandsPermission:    cfg.Bot.CommandsPermission,
		ResetPermission:       cfg.Bot.ResetPermission,
		MaxContextMessages:    cfg.Bot.MaxContextMessages,
		StreamContextTTL:      cfg.Bot.StreamContextTTL,
		GlobalCooldown:        cfg.Bot.GlobalCooldown,
		UserCooldown:          cfg.Bot.UserCooldown,
		DailyBudgetUSD:        cfg.Bot.DailyBudgetUSD,
		MonthlyBudgetUSD:      cfg.Bot.MonthlyBudgetUSD,
		MaxRequestsPerHour:    cfg.Bot.MaxRequestsPerHour,
		InputPricePerMillion:  cfg.AI.InputPricePerMillion,
		OutputPricePerMillion: cfg.AI.OutputPricePerMillion,
		BudgetStatePath:       cfg.Bot.BudgetStatePath,
		ChatLogPath:           cfg.Bot.ChatLogPath,
		EmoteCachePath:        cfg.Bot.EmoteCachePath,
		EnableEmoteContext:    cfg.Bot.EnableEmoteContext,
		ChannelEmotes:         channelEmotes,
		SnapshotCrop: bot.SnapshotCrop{
			Enabled: cfg.Bot.SnapshotCrop.Enabled,
			X:       cfg.Bot.SnapshotCrop.X,
			Y:       cfg.Bot.SnapshotCrop.Y,
			Width:   cfg.Bot.SnapshotCrop.Width,
			Height:  cfg.Bot.SnapshotCrop.Height,
		},
		Knowledge: knowledgeBase,
	}, chat, aiClient, streamProvider, recentService, announcementService, logger)

	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsClientID != "" && cfg.Twitch.AdsOAuthToken != "" && broadcasterID != "" {
		adsHelix := twitch.NewHelixClient(cfg.Twitch.AdsClientID, cfg.Twitch.AdsOAuthToken)
		var adsAuth adTokenRefresher
		if cfg.Twitch.AdsRefreshToken != "" {
			adsAuth = twitch.NewAuthManager(twitch.AuthConfig{
				ClientID:                     cfg.Twitch.AdsClientID,
				ClientSecret:                 cfg.Twitch.AdsClientSecret,
				RefreshToken:                 cfg.Twitch.AdsRefreshToken,
				StatePath:                    cfg.Twitch.AdsTokenStatePath,
				PreferConfiguredRefreshToken: preferConfiguredRefreshToken(envPath, cfg.Twitch.AdsTokenStatePath),
			})
		}
		adService = adalerts.New(adalerts.Config{
			Channel:        cfg.Twitch.Channel,
			BroadcasterID:  broadcasterID,
			Enabled:        cfg.AdAlerts.Enabled,
			WarningLead:    cfg.AdAlerts.WarningLead,
			PollInterval:   cfg.AdAlerts.PollInterval,
			WarningMessage: cfg.AdAlerts.WarningMessage,
			StartMessage:   cfg.AdAlerts.StartMessage,
			EndMessage:     cfg.AdAlerts.EndMessage,
			Composer:       runner,
		}, chat, &twitchAdScheduleProvider{helix: adsHelix, auth: adsAuth, tokenExpiresAt: adsTokenExpiresAt(cfg.Twitch.AdsTokenStatePath), logger: logger}, logger)
	} else if cfg.AdAlerts.Enabled {
		logger.Warn("ad alerts enabled but not started",
			"has_ads_client_id", cfg.Twitch.AdsClientID != "",
			"has_ads_token", cfg.Twitch.AdsOAuthToken != "",
			"has_broadcaster_id", broadcasterID != "",
		)
	}

	runner.SetAdAlerts(adService)
	if adService != nil && adBreaks != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case event := <-adBreaks:
					adService.HandleAdBreakBegin(ctx, adalerts.AdBreakBegin{
						StartedAt: event.StartedAt,
						Duration:  event.Duration,
						Automatic: event.Automatic,
					})
				}
			}
		}()
	}
	if redeems != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case event := <-redeems:
					logger.Info("dispatching media action redeem", "reward", event.RewardTitle, "user", event.UserLogin)
					options.MediaActionRedeem(ctx, event)
				}
			}
		}()
	}

	err = runner.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func appAccessToken(ctx context.Context, cfg config.Config, logger *slog.Logger) string {
	if cfg.Twitch.ClientID == "" || cfg.Twitch.ClientSecret == "" {
		if logger != nil {
			logger.Warn("Twitch app access token unavailable; chat messages will not use the Chat Bot Badge app-token path",
				"has_client_id", cfg.Twitch.ClientID != "",
				"has_client_secret", cfg.Twitch.ClientSecret != "",
			)
		}
		return ""
	}
	tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
		ClientID:     cfg.Twitch.ClientID,
		ClientSecret: cfg.Twitch.ClientSecret,
		StatePath:    cfg.Twitch.AppTokenStatePath,
	}).AppAccessToken(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to get Twitch app access token; chat messages will use the user token", "error", err)
		}
		return ""
	}
	if logger != nil {
		logger.Info("using Twitch app access token for chat message sending", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
	}
	return tokenSet.AccessToken
}

func newTwitchChat(cfg config.Config, broadcasterID, botUserID, sendToken string, adBreaks chan<- twitch.AdBreakEvent, redeems chan<- twitch.ChannelPointRedeemEvent, logger *slog.Logger) bot.Chat {
	if cfg.Twitch.ClientID != "" && cfg.Twitch.OAuthToken != "" && broadcasterID != "" && botUserID != "" {
		if logger != nil {
			logger.Info("configuring EventSub chat transport",
				"redeems_enabled", redeems != nil,
				"has_redeem_token", cfg.Twitch.AdsOAuthToken != "",
				"has_redeem_client_id", cfg.Twitch.AdsClientID != "",
			)
		}
		return twitch.NewEventSubChatClient(twitch.EventSubConfig{
			ClientID:       cfg.Twitch.ClientID,
			Token:          cfg.Twitch.OAuthToken,
			SendToken:      sendToken,
			AdClientID:     cfg.Twitch.AdsClientID,
			AdToken:        cfg.Twitch.AdsOAuthToken,
			RedeemClientID: cfg.Twitch.AdsClientID,
			RedeemToken:    cfg.Twitch.AdsOAuthToken,
			Channel:        cfg.Twitch.Channel,
			BroadcasterID:  broadcasterID,
			UserID:         botUserID,
			AdBreaks:       adBreaks,
			Redeems:        redeems,
		}, logger)
	}
	if logger != nil {
		logger.Warn("using legacy IRC chat transport; EventSub chat requires client ID, access token, broadcaster ID, and bot user ID",
			"has_client_id", cfg.Twitch.ClientID != "",
			"has_token", cfg.Twitch.OAuthToken != "",
			"has_broadcaster_id", broadcasterID != "",
			"has_bot_user_id", botUserID != "",
		)
	}
	return twitch.NewClient(twitch.Config{
		Username: cfg.Twitch.BotUsername,
		Token:    cfg.Twitch.OAuthToken,
		Channel:  cfg.Twitch.Channel,
	}, logger)
}

func convertChannelEmotes(items []twitch.ChannelEmote) []twitch.Emote {
	emotes := make([]twitch.Emote, 0, len(items))
	for _, item := range items {
		if item.ID == "" || item.Name == "" {
			continue
		}
		emotes = append(emotes, twitch.Emote{ID: item.ID, Name: item.Name, Count: 1})
	}
	return emotes
}

type adScheduleHelix interface {
	GetAdSchedule(ctx context.Context, broadcasterID string) (twitch.AdSchedule, error)
	SetAccessToken(accessToken string)
}

type adTokenRefresher interface {
	Refresh(ctx context.Context) (twitch.TokenSet, error)
}

type twitchAdScheduleProvider struct {
	helix          adScheduleHelix
	auth           adTokenRefresher
	tokenExpiresAt time.Time
	logger         *slog.Logger
}

func (p *twitchAdScheduleProvider) GetAdSchedule(ctx context.Context, broadcasterID string) (adalerts.Schedule, error) {
	if err := p.refreshIfNeeded(ctx, false); err != nil {
		return adalerts.Schedule{}, err
	}
	schedule, err := p.helix.GetAdSchedule(ctx, broadcasterID)
	if err != nil {
		if isUnauthorized(err) {
			if refreshErr := p.refreshIfNeeded(ctx, true); refreshErr != nil {
				return adalerts.Schedule{}, refreshErr
			}
			schedule, err = p.helix.GetAdSchedule(ctx, broadcasterID)
			if err == nil {
				return convertAdSchedule(schedule), nil
			}
		}
		return adalerts.Schedule{}, err
	}
	return convertAdSchedule(schedule), nil
}

func (p *twitchAdScheduleProvider) refreshIfNeeded(ctx context.Context, force bool) error {
	if p.auth == nil {
		return nil
	}
	if !force && (p.tokenExpiresAt.IsZero() || time.Now().Before(p.tokenExpiresAt.Add(-2*time.Minute))) {
		return nil
	}
	tokenSet, err := p.auth.Refresh(ctx)
	if err != nil {
		return fmt.Errorf("refresh twitch ads token: %w", err)
	}
	p.helix.SetAccessToken(tokenSet.AccessToken)
	p.tokenExpiresAt = tokenSet.ExpiresAt
	if p.logger != nil {
		p.logger.Info("refreshed twitch ads access token for ad polling", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

func isUnauthorized(err error) bool {
	return strings.Contains(err.Error(), "401 Unauthorized")
}

func adsTokenExpiresAt(path string) time.Time {
	if strings.TrimSpace(path) == "" {
		return time.Time{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	var tokenSet twitch.TokenSet
	if err := json.Unmarshal(data, &tokenSet); err != nil {
		return time.Time{}
	}
	return tokenSet.ExpiresAt
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

func convertAdSchedule(schedule twitch.AdSchedule) adalerts.Schedule {
	return adalerts.Schedule{
		NextAdAt:        schedule.NextAdAt,
		LastAdAt:        schedule.LastAdAt,
		Duration:        schedule.Duration,
		PrerollFreeTime: schedule.PrerollFreeTime,
		SnoozeCount:     schedule.SnoozeCount,
		SnoozeRefreshAt: schedule.SnoozeRefreshAt,
	}
}

type userResolver interface {
	GetUsersByLogin(ctx context.Context, logins []string) ([]twitch.UserInfo, error)
}

func resolveRecentStreamerIDs(ctx context.Context, helix userResolver, channel, botUsername string, logger *slog.Logger) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	users, err := helix.GetUsersByLogin(ctx, []string{channel, botUsername})
	if err != nil {
		logger.Warn("failed to resolve twitch user IDs for recent streamer tracking", "error", err)
		return "", ""
	}

	var broadcasterID, moderatorID string
	for _, user := range users {
		switch strings.ToLower(user.Login) {
		case strings.ToLower(channel):
			broadcasterID = user.ID
		case strings.ToLower(botUsername):
			moderatorID = user.ID
		}
	}
	if broadcasterID == "" || moderatorID == "" {
		logger.Warn("recent streamer chatter polling needs broadcaster and bot user IDs", "broadcaster_found", broadcasterID != "", "bot_found", moderatorID != "")
	}
	return broadcasterID, moderatorID
}
