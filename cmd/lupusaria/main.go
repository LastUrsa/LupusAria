package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"lupusaria/internal/botrunner"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := botrunner.Run(ctx, ".env", logger); err != nil {
		logger.Error("bot stopped with error", "error", err)
		os.Exit(1)
	}

	fmt.Println("LupusAria stopped.")
}
