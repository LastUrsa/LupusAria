package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/adalerts"
	"lupusaria/internal/ai"
	"lupusaria/internal/announcements"
	"lupusaria/internal/budget"
	"lupusaria/internal/knowledge"
	"lupusaria/internal/personality"
	"lupusaria/internal/recentstreamers"
	"lupusaria/internal/twitch"
)

type Chat interface {
	Connect(ctx context.Context) (<-chan twitch.Message, error)
	Say(channel, text string) error
	Close() error
}

type Config struct {
	Name                  string
	Channel               string
	Personality           string
	EnableMentions        bool
	EnableAsk             bool
	EnableLurk            bool
	EnableCommands        bool
	EnableReset           bool
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
	Knowledge             knowledge.Base
}

type Bot struct {
	cfg    Config
	chat   Chat
	ai     ai.Client
	budget *budget.Guard
	stream *cachedStreamContext
	recent *recentstreamers.Service
	ann    *announcements.Service
	know   knowledge.Base
	logger *slog.Logger

	contextMu     sync.Mutex
	context       []twitch.Message
	lastGlobalUse time.Time
	lastUserUse   map[string]time.Time
}

type aiRequest struct {
	Prompt string
	Kind   string
}

func New(cfg Config, chat Chat, aiClient ai.Client, streamProvider StreamInfoProvider, recentStreamers *recentstreamers.Service, announcementService *announcements.Service, logger *slog.Logger) *Bot {
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
		recent:      recentStreamers,
		ann:         announcementService,
		know:        cfg.Knowledge,
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
	if b.recent != nil {
		b.recent.StartChatterPolling(ctx)
	}
	if b.ann != nil {
		b.ann.Start(ctx)
	}

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
	if b.isSelf(msg.Username) {
		return
	}

	b.remember(msg)
	if b.recent != nil {
		b.recent.ObserveMessage(time.Now(), msg.Username, msg.DisplayName)
	}

	if b.handlePublicCommand(ctx, msg) {
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
	if looksIncompleteReply(reply) {
		b.logger.Warn("ai reply looked incomplete; retrying once", "reply", reply)
		if decision := b.budget.Allow(time.Now()); !decision.Allowed {
			b.logger.Warn("ai retry blocked by budget guard", "user", msg.Username, "reason", decision.Reason)
			_ = b.chat.Say(msg.Channel, fmt.Sprintf("@%s Sorry, my thoughts tripped over a cable. Try again in a moment.", msg.DisplayName))
			return
		}
		retryResponse, retryErr := b.ai.Complete(ctx, messages)
		if retryErr != nil {
			b.logger.Warn("ai retry failed", "error", retryErr)
			_ = b.chat.Say(msg.Channel, fmt.Sprintf("@%s Sorry, my thoughts tripped over a cable. Try again in a moment.", msg.DisplayName))
			return
		}
		retryReceipt := b.budget.Record(time.Now(), messages, retryResponse)
		b.logger.Info("ai retry usage recorded",
			"input_tokens", retryReceipt.InputTokens,
			"output_tokens", retryReceipt.OutputTokens,
			"cost_usd", fmt.Sprintf("%.6f", retryReceipt.CostUSD),
			"estimated", retryReceipt.Estimated,
		)
		reply = cleanReply(retryResponse.Text)
		if reply == "" || looksIncompleteReply(reply) {
			b.logger.Warn("ai retry reply still looked incomplete", "reply", reply)
			_ = b.chat.Say(msg.Channel, fmt.Sprintf("@%s Sorry, my thoughts tripped over a cable. Try again in a moment.", msg.DisplayName))
			return
		}
	}
	if len(reply) > 450 {
		reply = smartTruncate(reply, 450)
	}

	if err := b.chat.Say(msg.Channel, fmt.Sprintf("@%s %s", msg.DisplayName, reply)); err != nil {
		b.logger.Warn("failed to send twitch message", "error", err)
		return
	}
	b.logger.Info("reply sent", "channel", msg.Channel, "user", msg.Username, "length", len(reply))
}

func (b *Bot) remember(msg twitch.Message) {
	if b.isSelf(msg.Username) {
		return
	}
	b.contextMu.Lock()
	defer b.contextMu.Unlock()
	b.context = append(b.context, msg)
	if len(b.context) > b.cfg.MaxContextMessages {
		b.context = b.context[len(b.context)-b.cfg.MaxContextMessages:]
	}
}

func (b *Bot) handlePublicCommand(ctx context.Context, msg twitch.Message) bool {
	text := strings.TrimSpace(msg.Text)
	lower := strings.ToLower(text)
	if b.recent != nil && b.recent.HandleCommand(ctx, msg) {
		return true
	}
	if b.ann != nil && b.ann.HandleCommand(ctx, msg, b.isBroadcaster(msg)) {
		return true
	}

	switch {
	case b.cfg.EnableCommands && lower == "!commands":
		_ = b.chat.Say(msg.Channel, fmt.Sprintf("Commands: @%s <message>, !ask <question>, !lurk [reason], !autoso, !autoso next, !autoso refresh, !autoso status.", b.cfg.Name))
		return true
	case b.cfg.EnableReset && lower == "!reset":
		if !b.isBroadcaster(msg) {
			_ = b.chat.Say(msg.Channel, "Only the broadcaster can reset my chat context.")
			return true
		}
		b.contextMu.Lock()
		b.context = nil
		b.contextMu.Unlock()
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

	if b.cfg.EnableAsk && strings.HasPrefix(lower, "!ask ") {
		prompt := strings.TrimSpace(text[len("!ask "):])
		if prompt == "" {
			return aiRequest{}, false
		}
		return aiRequest{Prompt: prompt, Kind: "ask"}, true
	}

	if b.cfg.EnableLurk && strings.HasPrefix(lower, "!lurk") {
		reason := strings.TrimSpace(text[len("!lurk"):])
		prompt := fmt.Sprintf(`%s is going to lurk. Write one fresh, warm Twitch-chat send-off under 22 words.`, msg.DisplayName)
		if reason != "" {
			prompt += fmt.Sprintf(" Their reason: %q.", reason)
		}
		prompt += " No cliches, no markdown, no explanation."
		return aiRequest{Prompt: prompt, Kind: "lurk"}, true
	}

	mention := "@" + botName
	if b.cfg.EnableMentions && strings.Contains(lower, mention) {
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
	for _, item := range b.recentContext() {
		if sameChatMessage(item, msg) {
			continue
		}
		fmt.Fprintf(&recent, "%s: %s\n", item.DisplayName, item.Text)
	}

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
	replyContext := formatReplyContext(msg)
	knowledgeQuery := request.Prompt
	if msg.ReplyParentText != "" {
		knowledgeQuery += "\n" + msg.ReplyParentText
	}
	knowledgeContext := knowledge.Format(b.know.Relevant(knowledgeQuery, 3))

	return []ai.Message{
		{Role: "system", Content: personality.SystemInstruction(personality.Config{
			Name:        b.cfg.Name,
			Personality: b.cfg.Personality,
		})},
		{Role: "user", Content: personality.UserPrompt(request.Kind, streamContext, knowledgeContext, replyContext, recent.String(), msg.DisplayName, request.Prompt)},
	}
}

func (b *Bot) ComposeAdAlert(ctx context.Context, event adalerts.Event) (string, error) {
	if decision := b.budget.Allow(time.Now()); !decision.Allowed {
		return "", fmt.Errorf("budget guard blocked ad alert composition: %s", decision.Reason)
	}

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var recent strings.Builder
	for _, item := range b.recentContext() {
		fmt.Fprintf(&recent, "%s: %s\n", item.DisplayName, item.Text)
	}

	streamContext := "Stream context: unavailable."
	if b.stream != nil {
		streamInfo, ok, err := b.stream.Get(ctx, b.cfg.Channel)
		if err != nil {
			b.logger.Warn("failed to fetch stream context for ad alert", "error", err)
		} else {
			streamContext = formatStreamContext(streamInfo, ok)
		}
	}

	prompt := adAlertPrompt(event, streamContext, recent.String())
	messages := []ai.Message{
		{Role: "system", Content: personality.SystemInstruction(personality.Config{
			Name:        b.cfg.Name,
			Personality: b.cfg.Personality,
		})},
		{Role: "user", Content: prompt},
	}
	response, err := b.ai.Complete(ctx, messages)
	if err != nil {
		return "", err
	}

	receipt := b.budget.Record(time.Now(), messages, response)
	b.logger.Info("ad alert ai usage recorded",
		"event", event.Kind,
		"input_tokens", receipt.InputTokens,
		"output_tokens", receipt.OutputTokens,
		"cost_usd", fmt.Sprintf("%.6f", receipt.CostUSD),
		"estimated", receipt.Estimated,
	)

	reply := cleanReply(response.Text)
	if len(reply) > 300 {
		reply = reply[:300]
	}
	return reply, nil
}

func (b *Bot) recentContext() []twitch.Message {
	b.contextMu.Lock()
	defer b.contextMu.Unlock()
	return append([]twitch.Message(nil), b.context...)
}

func adAlertPrompt(event adalerts.Event, streamContext, recentChat string) string {
	var detail string
	switch event.Kind {
	case adalerts.EventWarning:
		detail = fmt.Sprintf("An ad break is scheduled in about %s.", formatDurationForPrompt(event.Lead))
	case adalerts.EventStart:
		detail = fmt.Sprintf("An ad break is starting now and should last about %s.", formatDurationForPrompt(event.Duration))
	case adalerts.EventEnd:
		detail = "The ad break has ended."
	default:
		detail = "An ad alert needs to be sent."
	}
	if strings.TrimSpace(recentChat) == "" {
		recentChat = "No recent chat context."
	}
	return fmt.Sprintf(`Write one Twitch chat message for an ad alert.
Stay in character as Lupus Aria: kind, friendly, lightly playful, subtle space-wolf flavor only if it fits.
Use recent chat context only when it naturally helps. Do not call viewers a pack. Do not use uwu-style speech.
Keep it natural for Ursa's stream, ideally under 200 characters, not overly verbose.
No markdown, no quotes, no @mentions.

Alert: %s
%s
%s

Recent chat:
%s`, event.Kind, detail, streamContext, recentChat)
}

func formatDurationForPrompt(d time.Duration) string {
	if d <= 0 {
		return "a moment"
	}
	if d >= time.Minute {
		minutes := int(d.Round(time.Minute) / time.Minute)
		if minutes == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", minutes)
	}
	seconds := int(d.Round(time.Second) / time.Second)
	if seconds == 1 {
		return "1 second"
	}
	return fmt.Sprintf("%d seconds", seconds)
}

func cleanReply(reply string) string {
	reply = strings.TrimSpace(reply)
	reply = strings.Trim(reply, `"'`)
	reply = stripMetaThoughts(reply)
	reply = stripSpeakerLabel(reply)
	reply = removeMarkdownAsterisks(reply)
	reply = removeEmoji(reply)
	reply = strings.ReplaceAll(reply, "\n", " ")
	reply = strings.Join(strings.Fields(reply), " ")
	if reply == "" || strings.ContainsAny(reply[len(reply)-1:], ".!?") {
		return reply
	}
	return reply + "."
}

func stripMetaThoughts(text string) string {
	lines := strings.Split(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "thinking process:"),
			strings.HasPrefix(lower, "thought process:"),
			strings.HasPrefix(lower, "reasoning:"),
			strings.HasPrefix(lower, "analysis:"),
			strings.HasPrefix(lower, "system prompt:"),
			strings.HasPrefix(lower, "prompt:"),
			strings.HasPrefix(lower, "instructions:"),
			strings.HasPrefix(lower, "instruction:"):
			continue
		default:
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func stripSpeakerLabel(text string) string {
	for _, label := range []string{"LupusAria:", "ModeratorLupusAria:", "Lupus Aria:"} {
		if strings.HasPrefix(strings.TrimSpace(text), label) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), label))
		}
	}
	return text
}

func removeMarkdownAsterisks(text string) string {
	text = strings.ReplaceAll(text, "**", "")
	text = strings.ReplaceAll(text, "*", "")
	return text
}

func removeEmoji(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0x200D || r == 0xFE0F:
			return -1
		case r >= 0x2600 && r <= 0x27BF:
			return -1
		case r >= 0x1F000 && r <= 0x1FAFF:
			return -1
		default:
			return r
		}
	}, text)
}

func smartTruncate(text string, maxLength int) string {
	text = strings.TrimSpace(text)
	if maxLength < 1 || len(text) <= maxLength {
		return text
	}

	truncated := text[:maxLength]
	best := -1
	for _, mark := range []string{". ", "! ", "? "} {
		if index := strings.LastIndex(truncated, mark); index > int(float64(maxLength)*0.7) {
			best = index + 1
		}
	}
	if best > 0 {
		return strings.TrimSpace(text[:best])
	}

	for _, mark := range []string{", ", "; ", ": ", " - "} {
		if index := strings.LastIndex(truncated, mark); index > int(float64(maxLength)*0.6) {
			best = index
		}
	}
	if best > 0 {
		return strings.TrimSpace(text[:best]) + "."
	}

	if index := strings.LastIndex(truncated, " "); index > int(float64(maxLength)*0.8) {
		return strings.TrimSpace(text[:index]) + "."
	}
	return strings.TrimSpace(text[:maxLength-1]) + "."
}

func looksIncompleteReply(reply string) bool {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return false
	}

	trimmed := strings.TrimRight(reply, ".!?")
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}

	lowerTrimmed := strings.ToLower(trimmed)
	if strings.HasSuffix(lowerTrimmed, "to legal") {
		return true
	}

	last := strings.ToLower(strings.Trim(fields[len(fields)-1], `"'()[]{}:;,`))
	switch last {
	case "a", "an", "the", "to", "for", "from", "with", "without", "of", "in", "on", "at", "by", "as", "and", "or", "but", "because", "about", "into", "through":
		return true
	default:
		return false
	}
}

func sameChatMessage(a, b twitch.Message) bool {
	return a.Username == b.Username && a.DisplayName == b.DisplayName && a.Text == b.Text && a.Channel == b.Channel
}

func formatReplyContext(msg twitch.Message) string {
	text := strings.TrimSpace(msg.ReplyParentText)
	if text == "" {
		return ""
	}
	name := strings.TrimSpace(msg.ReplyParentDisplayName)
	if name == "" {
		name = strings.TrimSpace(msg.ReplyParentUserLogin)
	}
	if name == "" {
		name = "previous message"
	}
	return fmt.Sprintf("Reply context: %s said: %s", name, text)
}

func (b *Bot) isBroadcaster(msg twitch.Message) bool {
	return msg.IsBroadcaster || strings.EqualFold(msg.Username, msg.Channel)
}

func (b *Bot) isSelf(username string) bool {
	return strings.EqualFold(username, b.cfg.Name)
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
