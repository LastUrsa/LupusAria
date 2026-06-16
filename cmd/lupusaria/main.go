package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/bot"
	"lupusaria/internal/config"
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

	runner := bot.New(bot.Config{
		Name:                  cfg.Bot.Name,
		Personality:           cfg.Bot.Personality,
		MaxContextMessages:    cfg.Bot.MaxContextMessages,
		GlobalCooldown:        cfg.Bot.GlobalCooldown,
		UserCooldown:          cfg.Bot.UserCooldown,
		DailyBudgetUSD:        cfg.Bot.DailyBudgetUSD,
		MonthlyBudgetUSD:      cfg.Bot.MonthlyBudgetUSD,
		MaxRequestsPerHour:    cfg.Bot.MaxRequestsPerHour,
		InputPricePerMillion:  cfg.AI.InputPricePerMillion,
		OutputPricePerMillion: cfg.AI.OutputPricePerMillion,
		BudgetStatePath:       cfg.Bot.BudgetStatePath,
	}, chat, aiClient, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("bot stopped with error", "error", err)
		os.Exit(1)
	}

	fmt.Println("LupusAria stopped.")
}
