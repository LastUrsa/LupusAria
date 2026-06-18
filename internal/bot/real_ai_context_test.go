package bot

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/config"
	"lupusaria/internal/twitch"
)

func TestRealAIUsesPhoenixWrightChatContextForLurk(t *testing.T) {
	if os.Getenv("LUPUSARIA_REAL_AI_CONTEXT_TEST") != "1" {
		t.Skip("set LUPUSARIA_REAL_AI_CONTEXT_TEST=1 to call the configured AI provider")
	}

	cfg, err := config.LoadPartial("../../.env")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider == "" || cfg.AI.Provider == "mock" {
		t.Skip("AI_PROVIDER is mock or unset")
	}
	if cfg.AI.MaxOutputTokens < 512 {
		cfg.AI.MaxOutputTokens = 512
	}

	client, err := ai.NewClient(cfg.AI)
	if err != nil {
		t.Fatal(err)
	}

	b := New(Config{
		Name:                  cfg.Bot.Name,
		Personality:           cfg.Bot.Personality,
		EnableMentions:        true,
		EnableAsk:             true,
		EnableLurk:            true,
		EnableCommands:        true,
		EnableReset:           true,
		MaxContextMessages:    30,
		StreamContextTTL:      time.Minute,
		DailyBudgetUSD:        0,
		MonthlyBudgetUSD:      0,
		MaxRequestsPerHour:    0,
		InputPricePerMillion:  cfg.AI.InputPricePerMillion,
		OutputPricePerMillion: cfg.AI.OutputPricePerMillion,
	}, &fakeChat{}, client, fakeStreamProvider{info: twitch.StreamInfo{
		Channel:     "lastursa",
		Title:       "Phoenix Wright courtroom chaos",
		GameName:    "Phoenix Wright: Ace Attorney Trilogy",
		ViewerCount: 18,
		Live:        true,
	}}, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	now := time.Now()
	chatLines := []struct {
		user string
		text string
	}{
		{"ragenowich", "phoenix's flashback shows him pushing the guy on his back, but the photo shows him laying on his front"},
		{"ragenowich", "the judge just wanted to dd more rules to the game"},
		{"ragenowich", "FOOLISH FOOL WHO FOOLISHLY FOOL AROUND"},
		{"ragenowich", "He said the von Karma word"},
		{"ZorkuAravar", "for a GODDAMN MOMENT"},
		{"ragenowich", "If there is only one witness other than the defendant, it must be the actual culprit"},
		{"ZorkuAravar", "breh"},
		{"ragenowich", "i mean judging is a side hustle to being a clown apparently"},
		{"smirkwiz", "This game mostly bases off of Japan law tho"},
		{"ZorkuAravar", "yeah my major is in classical guitar but my minor is quantum physics"},
		{"ragenowich", `lmao "my girlfriend always tells me the same thing, she always wants me to give her back the symbol of our love"`},
		{"ragenowich", "i might have to bounce for the night actually"},
		{"ragenowich", "it is 4am for me"},
		{"ragenowich", "have a wonderful time you wonderful bolf"},
	}
	for i, line := range chatLines {
		b.context = append(b.context, chatContextEntry{
			Remembered: now.Add(-time.Duration(len(chatLines)-i) * time.Minute),
			Message: twitch.Message{
				Channel:     "lastursa",
				Username:    strings.ToLower(line.user),
				DisplayName: line.user,
				Text:        line.text,
			},
		})
	}

	current := twitch.Message{
		Channel:     "lastursa",
		Username:    "ragenowich",
		DisplayName: "ragenowich",
		Text:        "!lurk it is 4am for me",
	}
	request, ok := b.extractAIRequest(current)
	if !ok {
		t.Fatal("expected lurk request")
	}
	messages := b.buildAIMessages(context.Background(), current, request)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := client.Complete(ctx, messages)
	if err != nil {
		t.Fatal(err)
	}
	reply := cleanReply(response.Text)
	t.Logf("provider=%s model=%s", cfg.AI.Provider, cfg.AI.Model)
	t.Logf("real Lupus reply: @%s %s", current.DisplayName, reply)
	t.Logf("usage: input_tokens=%d output_tokens=%d estimated=%v cost_usd=%.6f", response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.Estimated, response.Usage.CostUSD)
	t.Logf("prompt excerpt:\n%s", messages[1].Content)
}
