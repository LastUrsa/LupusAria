package botrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func Run(ctx context.Context, envPath string, logger *slog.Logger) error {
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
			ClientID:     cfg.Twitch.ClientID,
			ClientSecret: cfg.Twitch.ClientSecret,
			RefreshToken: cfg.Twitch.RefreshToken,
			StatePath:    cfg.Twitch.TokenStatePath,
		}).Refresh(ctx)
		if err != nil {
			logger.Warn("failed to refresh twitch token; using configured access token", "error", err)
		} else {
			cfg.Twitch.OAuthToken = "oauth:" + tokenSet.AccessToken
			logger.Info("refreshed twitch access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}

	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsRefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:     cfg.Twitch.AdsClientID,
			ClientSecret: cfg.Twitch.AdsClientSecret,
			RefreshToken: cfg.Twitch.AdsRefreshToken,
			StatePath:    cfg.Twitch.AdsTokenStatePath,
		}).Refresh(ctx)
		if err != nil {
			logger.Warn("failed to refresh twitch ads token; using configured ads access token if available", "error", err)
		} else {
			cfg.Twitch.AdsOAuthToken = "oauth:" + tokenSet.AccessToken
			logger.Info("refreshed twitch ads access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}
	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsOAuthToken == "" {
		cfg.Twitch.AdsOAuthToken = cfg.Twitch.OAuthToken
	}

	chat := twitch.NewClient(twitch.Config{
		Username: cfg.Twitch.BotUsername,
		Token:    cfg.Twitch.OAuthToken,
		Channel:  cfg.Twitch.Channel,
	}, logger)

	var streamProvider bot.StreamInfoProvider
	var helix *twitch.HelixClient
	var recentService *recentstreamers.Service
	var announcementService *announcements.Service
	var adService *adalerts.Service
	var broadcasterID string
	if cfg.Twitch.ClientID != "" {
		helix = twitch.NewHelixClient(cfg.Twitch.ClientID, cfg.Twitch.OAuthToken)
		streamProvider = helix
		var moderatorID string
		broadcasterID, moderatorID = resolveRecentStreamerIDs(ctx, helix, cfg.Twitch.Channel, cfg.Twitch.BotUsername, logger)
		if cfg.RecentStreamers.Enabled {
			recentService = recentstreamers.New(recentstreamers.Config{
				Channel:             cfg.Twitch.Channel,
				BroadcasterID:       broadcasterID,
				ModeratorID:         moderatorID,
				MinWatch:            cfg.RecentStreamers.MinWatch,
				RecentWindow:        cfg.RecentStreamers.RecentWindow,
				PageSize:            cfg.RecentStreamers.PageSize,
				ShoutoutDelay:       cfg.RecentStreamers.ShoutoutDelay,
				CacheTTL:            cfg.RecentStreamers.CacheTTL,
				ChatterPollInterval: cfg.RecentStreamers.ChatterPollInterval,
			}, chat, helix, logger)
		}
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
		Knowledge:             knowledgeBase,
	}, chat, aiClient, streamProvider, recentService, announcementService, logger)

	if cfg.AdAlerts.Enabled && cfg.Twitch.AdsClientID != "" {
		adsHelix := twitch.NewHelixClient(cfg.Twitch.AdsClientID, cfg.Twitch.AdsOAuthToken)
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
		}, chat, twitchAdScheduleProvider{helix: adsHelix}, logger)
	}

	if adService != nil {
		adService.Start(ctx)
	}

	err = runner.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type twitchAdScheduleProvider struct {
	helix *twitch.HelixClient
}

func (p twitchAdScheduleProvider) GetAdSchedule(ctx context.Context, broadcasterID string) (adalerts.Schedule, error) {
	schedule, err := p.helix.GetAdSchedule(ctx, broadcasterID)
	if err != nil {
		return adalerts.Schedule{}, err
	}
	return convertAdSchedule(schedule), nil
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
