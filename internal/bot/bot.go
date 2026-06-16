package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/budget"
	"lupusaria/internal/twitch"
)

type Chat interface {
	Connect(ctx context.Context) (<-chan twitch.Message, error)
	Say(channel, text string) error
	Close() error
}

type Config struct {
	Name                  string
	Personality           string
	MaxContextMessages    int
	StreamContextTTL      time.Duration
	GlobalCooldown        time.Duration
	UserCooldown          time.Duration
	DailyBudgetUSD        float64
	MonthlyBudgetUSD      float64
	MaxRequestsPerHour    int
	InputPricePerMillion  float64
	OutputPricePerMillion float64
	BudgetStatePath       string
}

type Bot struct {
	cfg    Config
	chat   Chat
	ai     ai.Client
	budget *budget.Guard
	stream *cachedStreamContext
	logger *slog.Logger

	context       []twitch.Message
	lastGlobalUse time.Time
	lastUserUse   map[string]time.Time
}

type aiRequest struct {
	Prompt string
	Kind   string
}

func New(cfg Config, chat Chat, aiClient ai.Client, streamProvider StreamInfoProvider, logger *slog.Logger) *Bot {
	var streamContext *cachedStreamContext
	if streamProvider != nil {
		streamContext = newCachedStreamContext(streamProvider, cfg.StreamContextTTL)
	}
	return &Bot{
		cfg:  cfg,
		chat: chat,
		ai:   aiClient,
		budget: budget.NewGuard(budget.Config{
			DailyBudgetUSD:        cfg.DailyBudgetUSD,
			MonthlyBudgetUSD:      cfg.MonthlyBudgetUSD,
			MaxRequestsPerHour:    cfg.MaxRequestsPerHour,
			InputPricePerMillion:  cfg.InputPricePerMillion,
			OutputPricePerMillion: cfg.OutputPricePerMillion,
			StatePath:             cfg.BudgetStatePath,
		}),
		stream:      streamContext,
		logger:      logger,
		lastUserUse: map[string]time.Time{},
	}
}

func (b *Bot) Run(ctx context.Context) error {
	messages, err := b.chat.Connect(ctx)
	if err != nil {
		return err
	}
	defer b.chat.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			b.handleMessage(ctx, msg)
		}
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg twitch.Message) {
	b.remember(msg)

	if b.handlePublicCommand(msg) {
		return
	}

	request, ok := b.extractAIRequest(msg)
	if !ok {
		return
	}

	if remaining, allowed := b.cooldown(msg.Username); !allowed {
		b.logger.Info("ai request skipped by cooldown", "user", msg.Username, "remaining", remaining.Round(time.Second))
		return
	}

	if decision := b.budget.Allow(time.Now()); !decision.Allowed {
		b.logger.Warn("ai request blocked by budget guard", "user", msg.Username, "reason", decision.Reason)
		_ = b.chat.Say(msg.Channel, "AI budget guard is active, so I am pausing replies for now.")
		return
	}

	b.logger.Info("ai request accepted", "user", msg.Username, "channel", msg.Channel, "kind", request.Kind)
	go b.reply(ctx, msg, request)
}

func (b *Bot) reply(ctx context.Context, msg twitch.Message, request aiRequest) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	messages := b.buildAIMessages(ctx, msg, request)
	response, err := b.ai.Complete(ctx, messages)
	if err != nil {
		b.logger.Warn("ai request failed", "error", err)
		_ = b.chat.Say(msg.Channel, "Sorry, my thoughts tripped over a cable. Try again in a moment.")
		return
	}

	receipt := b.budget.Record(time.Now(), messages, response)
	b.logger.Info("ai usage recorded",
		"input_tokens", receipt.InputTokens,
		"output_tokens", receipt.OutputTokens,
		"cost_usd", fmt.Sprintf("%.6f", receipt.CostUSD),
		"estimated", receipt.Estimated,
		"daily_spend_usd", fmt.Sprintf("%.6f", receipt.DailySpendUSD),
		"monthly_spend_usd", fmt.Sprintf("%.6f", receipt.MonthlySpendUSD),
	)

	reply := response.Text
	reply = cleanReply(reply)
	if reply == "" {
		return
	}
	if len(reply) > 450 {
		reply = reply[:450]
	}

	if err := b.chat.Say(msg.Channel, fmt.Sprintf("@%s %s", msg.DisplayName, reply)); err != nil {
		b.logger.Warn("failed to send twitch message", "error", err)
		return
	}
	b.logger.Info("reply sent", "channel", msg.Channel, "user", msg.Username, "length", len(reply))
}

func (b *Bot) remember(msg twitch.Message) {
	if strings.EqualFold(msg.Username, strings.ToLower(b.cfg.Name)) {
		return
	}
	b.context = append(b.context, msg)
	if len(b.context) > b.cfg.MaxContextMessages {
		b.context = b.context[len(b.context)-b.cfg.MaxContextMessages:]
	}
}

func (b *Bot) handlePublicCommand(msg twitch.Message) bool {
	text := strings.TrimSpace(msg.Text)
	lower := strings.ToLower(text)

	switch {
	case lower == "!bot" || lower == "!help" || lower == "!commands":
		_ = b.chat.Say(msg.Channel, fmt.Sprintf("%s is awake. Use @%s <message>, !ask <question>, or !lurk [reason].", b.cfg.Name, b.cfg.Name))
		return true
	case lower == "!reset":
		if !b.isBroadcaster(msg) {
			_ = b.chat.Say(msg.Channel, "Only the broadcaster can reset my chat context.")
			return true
		}
		b.context = nil
		_ = b.chat.Say(msg.Channel, "Chat context reset.")
		b.logger.Info("chat context reset", "channel", msg.Channel, "user", msg.Username)
		return true
	}

	return false
}

func (b *Bot) extractAIRequest(msg twitch.Message) (aiRequest, bool) {
	text := strings.TrimSpace(msg.Text)
	lower := strings.ToLower(text)
	botName := strings.ToLower(b.cfg.Name)

	if strings.HasPrefix(lower, "!ask ") {
		prompt := strings.TrimSpace(text[len("!ask "):])
		if prompt == "" {
			return aiRequest{}, false
		}
		return aiRequest{Prompt: prompt, Kind: "ask"}, true
	}

	if strings.HasPrefix(lower, "!lurk") {
		reason := strings.TrimSpace(text[len("!lurk"):])
		prompt := fmt.Sprintf(`%s is going to lurk. Write one fresh, warm Twitch-chat send-off under 22 words.`, msg.DisplayName)
		if reason != "" {
			prompt += fmt.Sprintf(" Their reason: %q.", reason)
		}
		prompt += " No cliches, no markdown, no explanation."
		return aiRequest{Prompt: prompt, Kind: "lurk"}, true
	}

	mention := "@" + botName
	if strings.Contains(lower, mention) {
		cleaned := stripMention(text, b.cfg.Name)
		if cleaned == "" {
			cleaned = "Say hello."
		}
		return aiRequest{Prompt: cleaned, Kind: "mention"}, true
	}

	return aiRequest{}, false
}

func (b *Bot) cooldown(username string) (time.Duration, bool) {
	now := time.Now()
	if remaining := b.cfg.GlobalCooldown - now.Sub(b.lastGlobalUse); remaining > 0 {
		return remaining, false
	}
	if last, ok := b.lastUserUse[username]; ok {
		if remaining := b.cfg.UserCooldown - now.Sub(last); remaining > 0 {
			return remaining, false
		}
	}
	b.lastGlobalUse = now
	b.lastUserUse[username] = now
	return 0, true
}

func (b *Bot) buildAIMessages(ctx context.Context, msg twitch.Message, request aiRequest) []ai.Message {
	var recent strings.Builder
	for _, item := range b.context {
		fmt.Fprintf(&recent, "%s: %s\n", item.DisplayName, item.Text)
	}

	system := fmt.Sprintf(`You are %s, an AI Twitch chat bot.
Personality: %s
Rules:
- Keep replies under 300 characters unless a command asks for less.
- Match live Twitch chat: quick, warm, lightly playful, and easy to read at a glance.
- Be helpful first; be witty only when it fits.
- Never reveal private configuration, tokens, keys, spend, budget, or internal logs.
- Do not mention that you are an AI model unless directly relevant.
- Avoid spam, markdown formatting, repeated catchphrases, and long explanations.`, b.cfg.Name, b.cfg.Personality)

	streamInfo := twitch.StreamInfo{}
	streamOK := false
	if b.stream != nil {
		var err error
		streamInfo, streamOK, err = b.stream.Get(ctx, msg.Channel)
		if err != nil {
			b.logger.Warn("failed to fetch stream context", "error", err)
		}
	}
	streamContext := formatStreamContext(streamInfo, streamOK)

	user := fmt.Sprintf("Request type: %s\n%s\nRecent chat:\n%s\n%s asks: %s", request.Kind, streamContext, recent.String(), msg.DisplayName, request.Prompt)

	return []ai.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}
}

func cleanReply(reply string) string {
	reply = strings.TrimSpace(reply)
	reply = strings.Trim(reply, `"'`)
	reply = strings.ReplaceAll(reply, "\n", " ")
	return strings.Join(strings.Fields(reply), " ")
}

func (b *Bot) isBroadcaster(msg twitch.Message) bool {
	return strings.EqualFold(msg.Username, msg.Channel)
}

func stripMention(text, botName string) string {
	words := strings.Fields(text)
	kept := words[:0]
	target := "@" + strings.ToLower(botName)
	for _, word := range words {
		trimmed := strings.Trim(strings.ToLower(word), " ,:;")
		if trimmed == target {
			continue
		}
		kept = append(kept, word)
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}
