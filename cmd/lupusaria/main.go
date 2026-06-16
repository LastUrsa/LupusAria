package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/bot"
	"lupusaria/internal/config"
	"lupusaria/internal/recentstreamers"
	"lupusaria/internal/twitch"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(".env")
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	aiClient, err := ai.NewClient(cfg.AI)
	if err != nil {
		logger.Error("failed to initialize ai client", "error", err)
		os.Exit(1)
	}

	if cfg.Twitch.RefreshToken != "" {
		tokenSet, err := twitch.NewAuthManager(twitch.AuthConfig{
			ClientID:     cfg.Twitch.ClientID,
			ClientSecret: cfg.Twitch.ClientSecret,
			RefreshToken: cfg.Twitch.RefreshToken,
			StatePath:    cfg.Twitch.TokenStatePath,
		}).Refresh(context.Background())
		if err != nil {
			logger.Warn("failed to refresh twitch token; using configured access token", "error", err)
		} else {
			cfg.Twitch.OAuthToken = "oauth:" + tokenSet.AccessToken
			logger.Info("refreshed twitch access token", "expires_at", tokenSet.ExpiresAt.Format(time.RFC3339))
		}
	}

	chat := twitch.NewClient(twitch.Config{
		Username: cfg.Twitch.BotUsername,
		Token:    cfg.Twitch.OAuthToken,
		Channel:  cfg.Twitch.Channel,
	}, logger)

	var streamProvider bot.StreamInfoProvider
	var helix *twitch.HelixClient
	var recentService *recentstreamers.Service
	if cfg.Twitch.ClientID != "" {
		helix = twitch.NewHelixClient(cfg.Twitch.ClientID, cfg.Twitch.OAuthToken)
		streamProvider = helix
		broadcasterID, moderatorID := resolveRecentStreamerIDs(context.Background(), helix, cfg.Twitch.Channel, cfg.Twitch.BotUsername, logger)
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

	runner := bot.New(bot.Config{
		Name:                  cfg.Bot.Name,
		Personality:           cfg.Bot.Personality,
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
	}, chat, aiClient, streamProvider, recentService, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("bot stopped with error", "error", err)
		os.Exit(1)
	}

	fmt.Println("LupusAria stopped.")
}

func resolveRecentStreamerIDs(ctx context.Context, helix *twitch.HelixClient, channel, botUsername string, logger *slog.Logger) (string, string) {
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
