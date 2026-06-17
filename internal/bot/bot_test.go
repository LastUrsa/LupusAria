package bot

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/knowledge"
	"lupusaria/internal/twitch"
)

type fakeChat struct {
	sent []string
}

func (f *fakeChat) Connect(context.Context) (<-chan twitch.Message, error) {
	ch := make(chan twitch.Message)
	close(ch)
	return ch, nil
}

func (f *fakeChat) Say(_ string, text string) error {
	f.sent = append(f.sent, text)
	return nil
}

func (f *fakeChat) Close() error {
	return nil
}

type fakeAI struct{}

func (fakeAI) Complete(context.Context, []ai.Message) (ai.Response, error) {
	return ai.Response{Text: "ok"}, nil
}

type fakeAIText struct {
	text string
}

func (f fakeAIText) Complete(context.Context, []ai.Message) (ai.Response, error) {
	return ai.Response{Text: f.text}, nil
}

type fakeAISequence struct {
	responses []string
	calls     int
}

func (f *fakeAISequence) Complete(context.Context, []ai.Message) (ai.Response, error) {
	if f.calls >= len(f.responses) {
		f.calls++
		return ai.Response{Text: ""}, nil
	}
	text := f.responses[f.calls]
	f.calls++
	return ai.Response{Text: text}, nil
}

type fakeStreamProvider struct {
	info twitch.StreamInfo
}

func (f fakeStreamProvider) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return f.info, nil
}

func TestHandleMessageIgnoresOwnMessages(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)

	b.handleMessage(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "lupusaria",
		DisplayName: "LupusAria",
		Text:        "@LupusAria hello from myself",
	})

	if len(chat.sent) != 0 {
		t.Fatalf("bot should not respond to itself, sent %#v", chat.sent)
	}
	if context := b.recentContext(); len(context) != 0 {
		t.Fatalf("bot should not remember its own messages, context %#v", context)
	}
}

func TestBotAndHelpCommandsAreIgnored(t *testing.T) {
	for _, text := range []string{"!bot", "!help"} {
		t.Run(text, func(t *testing.T) {
			chat := &fakeChat{}
			b := testBot(chat)

			handled := b.handlePublicCommand(context.Background(), twitch.Message{
				Channel:     "lastursa",
				Username:    "viewer",
				DisplayName: "Viewer",
				Text:        text,
			})

			if handled {
				t.Fatalf("%s should not be handled", text)
			}
			if len(chat.sent) != 0 {
				t.Fatalf("%s should not send chat responses, sent %#v", text, chat.sent)
			}
		})
	}
}

func TestCommandsShowsPublicCommandList(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)

	handled := b.handlePublicCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!commands",
	})

	if !handled {
		t.Fatal("expected !commands to be handled")
	}
	if len(chat.sent) != 1 {
		t.Fatalf("expected one response, got %#v", chat.sent)
	}
	lower := strings.ToLower(chat.sent[0])
	for _, want := range []string{"!ask", "!lurk", "!autoso"} {
		if !strings.Contains(lower, want) {
			t.Fatalf("command list missing %q: %q", want, chat.sent[0])
		}
	}
	for _, forbidden := range []string{"cost", "budget", "token", "secret", "key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("public command list exposed private term %q in %q", forbidden, chat.sent[0])
		}
	}
}

func TestResetRequiresBroadcaster(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)
	b.remember(twitch.Message{Username: "viewer", Text: "keep me"})

	handled := b.handlePublicCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!reset",
	})

	if !handled {
		t.Fatal("expected !reset to be handled")
	}
	if len(b.recentContext()) != 1 {
		t.Fatal("non-broadcaster reset should not clear context")
	}
	if len(chat.sent) != 1 || !strings.Contains(chat.sent[0], "Only the broadcaster") {
		t.Fatalf("unexpected reset response: %#v", chat.sent)
	}
}

func TestResetAllowsBroadcaster(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)
	b.remember(twitch.Message{Username: "viewer", Text: "clear me"})

	handled := b.handlePublicCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "lastursa",
		DisplayName: "LastUrsa",
		Text:        "!reset",
	})

	if !handled {
		t.Fatal("expected !reset to be handled")
	}
	if len(b.recentContext()) != 0 {
		t.Fatal("broadcaster reset should clear context")
	}
}

func TestCommandTogglesDisablePublicCommands(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Bot)
		message   twitch.Message
	}{
		{
			name: "commands disabled",
			configure: func(b *Bot) {
				b.cfg.EnableCommands = false
			},
			message: twitch.Message{Channel: "lastursa", Username: "viewer", DisplayName: "Viewer", Text: "!commands"},
		},
		{
			name: "reset disabled",
			configure: func(b *Bot) {
				b.cfg.EnableReset = false
			},
			message: twitch.Message{Channel: "lastursa", Username: "lastursa", DisplayName: "LastUrsa", Text: "!reset"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := &fakeChat{}
			b := testBot(chat)
			tt.configure(b)

			if handled := b.handlePublicCommand(context.Background(), tt.message); handled {
				t.Fatal("command should not be handled when disabled")
			}
			if len(chat.sent) != 0 {
				t.Fatalf("disabled command should not send chat, sent %#v", chat.sent)
			}
		})
	}
}

func TestExtractAIRequests(t *testing.T) {
	b := testBot(&fakeChat{})

	tests := []struct {
		name       string
		text       string
		wantKind   string
		wantPrompt string
	}{
		{
			name:       "ask",
			text:       "!ask what game is this?",
			wantKind:   "ask",
			wantPrompt: "what game is this?",
		},
		{
			name:       "mention",
			text:       "@LupusAria say hello",
			wantKind:   "mention",
			wantPrompt: "say hello",
		},
		{
			name:       "lurk",
			text:       "!lurk grabbing coffee",
			wantKind:   "lurk",
			wantPrompt: "grabbing coffee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request, ok := b.extractAIRequest(twitch.Message{
				Channel:     "lastursa",
				Username:    "viewer",
				DisplayName: "Viewer",
				Text:        tt.text,
			})
			if !ok {
				t.Fatal("expected AI request")
			}
			if request.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", request.Kind, tt.wantKind)
			}
			if !strings.Contains(request.Prompt, tt.wantPrompt) {
				t.Fatalf("prompt %q does not contain %q", request.Prompt, tt.wantPrompt)
			}
		})
	}
}

func TestAITogglesDisableRequests(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Bot)
		text      string
	}{
		{
			name: "ask disabled",
			configure: func(b *Bot) {
				b.cfg.EnableAsk = false
			},
			text: "!ask hello?",
		},
		{
			name: "lurk disabled",
			configure: func(b *Bot) {
				b.cfg.EnableLurk = false
			},
			text: "!lurk grabbing water",
		},
		{
			name: "mentions disabled",
			configure: func(b *Bot) {
				b.cfg.EnableMentions = false
			},
			text: "@LupusAria hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := testBot(&fakeChat{})
			tt.configure(b)

			if request, ok := b.extractAIRequest(twitch.Message{
				Channel:     "lastursa",
				Username:    "viewer",
				DisplayName: "Viewer",
				Text:        tt.text,
			}); ok {
				t.Fatalf("disabled AI request should not be extracted: %#v", request)
			}
		})
	}
}

func TestBuildAIMessagesIncludesStreamContext(t *testing.T) {
	chat := &fakeChat{}
	b := New(Config{
		Name:                  "LupusAria",
		Personality:           "test",
		EnableMentions:        true,
		EnableAsk:             true,
		EnableLurk:            true,
		EnableCommands:        true,
		EnableReset:           true,
		MaxContextMessages:    30,
		StreamContextTTL:      time.Minute,
		GlobalCooldown:        time.Second,
		UserCooldown:          time.Second,
		DailyBudgetUSD:        0,
		MonthlyBudgetUSD:      0,
		MaxRequestsPerHour:    0,
		InputPricePerMillion:  0,
		OutputPricePerMillion: 0,
	}, chat, fakeAI{}, fakeStreamProvider{info: twitch.StreamInfo{
		Channel:     "lastursa",
		Title:       "Testing LupusAria",
		GameName:    "Science & Technology",
		ViewerCount: 7,
		Live:        true,
	}}, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	messages := b.buildAIMessages(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!ask hello",
	}, aiRequest{Kind: "ask", Prompt: "hello"})

	if len(messages) != 2 {
		t.Fatalf("expected two AI messages, got %d", len(messages))
	}
	userPrompt := messages[1].Content
	for _, want := range []string{"Stream context: live", "Science & Technology", "Testing LupusAria", "Viewers: 7"} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("prompt missing %q: %s", want, userPrompt)
		}
	}
}

func TestBuildAIMessagesIncludesRelevantKnowledge(t *testing.T) {
	chat := &fakeChat{}
	b := New(Config{
		Name:               "LupusAria",
		Personality:        "test",
		EnableMentions:     true,
		EnableAsk:          true,
		EnableLurk:         true,
		EnableCommands:     true,
		EnableReset:        true,
		MaxContextMessages: 30,
		Knowledge: knowledge.Parse(`## Music
Tags: music, songs
- Ursa makes verified star songs.

## Projects
Tags: project
- Project facts.
`),
	}, chat, fakeAI{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	messages := b.buildAIMessages(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!ask what music does Ursa make?",
	}, aiRequest{Kind: "ask", Prompt: "what music does Ursa make?"})

	userPrompt := messages[1].Content
	if !strings.Contains(userPrompt, "Known facts selected for this request:") {
		t.Fatalf("prompt missing selected knowledge marker: %s", userPrompt)
	}
	if !strings.Contains(userPrompt, "Ursa makes verified star songs") {
		t.Fatalf("prompt missing relevant knowledge: %s", userPrompt)
	}
	if strings.Contains(userPrompt, "Project facts") {
		t.Fatalf("prompt included unrelated knowledge: %s", userPrompt)
	}
}

func TestBuildAIMessagesUsesReplyContextForKnowledge(t *testing.T) {
	chat := &fakeChat{}
	b := New(Config{
		Name:               "LupusAria",
		Personality:        "test",
		EnableMentions:     true,
		EnableAsk:          true,
		EnableLurk:         true,
		EnableCommands:     true,
		EnableReset:        true,
		MaxContextMessages: 30,
		Knowledge: knowledge.Parse(`## Identity
Tags: lastursa, who is lastursa
- LastUrsa is Ursa Starsong's Twitch username.
`),
	}, chat, fakeAI{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	current := twitch.Message{
		Channel:                "lastursa",
		Username:               "ragenowich",
		DisplayName:            "ragenowich",
		Text:                   "@LupusAria who's that?",
		ReplyParentDisplayName: "LupusAria",
		ReplyParentUserLogin:   "lupusaria",
		ReplyParentText:        "@ragenowich check out LastUrsa when you get a chance.",
	}
	b.remember(current)

	messages := b.buildAIMessages(context.Background(), current, aiRequest{Kind: "mention", Prompt: "who's that?"})
	userPrompt := messages[1].Content
	if !strings.Contains(userPrompt, "Reply context: LupusAria said: @ragenowich check out LastUrsa") {
		t.Fatalf("prompt missing reply context: %s", userPrompt)
	}
	if !strings.Contains(userPrompt, "LastUrsa is Ursa Starsong's Twitch username") {
		t.Fatalf("prompt missing reply-selected identity knowledge: %s", userPrompt)
	}
	if strings.Contains(userPrompt, "Recent chat:\nragenowich: @LupusAria who's that?") {
		t.Fatalf("prompt should not duplicate the current message in recent chat: %s", userPrompt)
	}
}

func TestBuildAIMessagesOmitsIrrelevantKnowledge(t *testing.T) {
	chat := &fakeChat{}
	b := New(Config{
		Name:               "LupusAria",
		Personality:        "test",
		EnableMentions:     true,
		EnableAsk:          true,
		EnableLurk:         true,
		EnableCommands:     true,
		EnableReset:        true,
		MaxContextMessages: 30,
		Knowledge: knowledge.Parse(`## Music
Tags: music, songs
- Ursa makes verified star songs.
`),
	}, chat, fakeAI{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	messages := b.buildAIMessages(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!ask any dinner ideas?",
	}, aiRequest{Kind: "ask", Prompt: "any dinner ideas?"})

	userPrompt := messages[1].Content
	if !strings.Contains(userPrompt, "Known facts: none selected for this request.") {
		t.Fatalf("prompt should explicitly omit knowledge: %s", userPrompt)
	}
	if strings.Contains(userPrompt, "Ursa makes verified star songs") {
		t.Fatalf("prompt included irrelevant knowledge: %s", userPrompt)
	}
}

func TestReplyDoesNotSendIncompleteModelSentence(t *testing.T) {
	chat := &fakeChat{}
	b := New(Config{
		Name:                  "LupusAria",
		Personality:           "test",
		MaxContextMessages:    30,
		DailyBudgetUSD:        0,
		MonthlyBudgetUSD:      0,
		MaxRequestsPerHour:    0,
		InputPricePerMillion:  0,
		OutputPricePerMillion: 0,
	}, chat, fakeAIText{text: "I think that might be a question for the"}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	b.reply(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "@LupusAria hello",
	}, aiRequest{Kind: "mention", Prompt: "hello"})

	if len(chat.sent) != 1 {
		t.Fatalf("expected one fallback message, got %#v", chat.sent)
	}
	if strings.Contains(chat.sent[0], "question for the") {
		t.Fatalf("incomplete reply should not be sent: %#v", chat.sent)
	}
	if !strings.Contains(chat.sent[0], "Try again") {
		t.Fatalf("expected retry fallback, got %#v", chat.sent)
	}
}

func TestReplyRetriesIncompleteModelSentence(t *testing.T) {
	chat := &fakeChat{}
	aiClient := &fakeAISequence{responses: []string{
		"I think that might be a question for the",
		"That one is probably for Ursa, but the mystery is funny.",
	}}
	b := New(Config{
		Name:                  "LupusAria",
		Personality:           "test",
		MaxContextMessages:    30,
		DailyBudgetUSD:        0,
		MonthlyBudgetUSD:      0,
		MaxRequestsPerHour:    0,
		InputPricePerMillion:  0,
		OutputPricePerMillion: 0,
	}, chat, aiClient, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	b.reply(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "@LupusAria hello",
	}, aiRequest{Kind: "mention", Prompt: "hello"})

	if aiClient.calls != 2 {
		t.Fatalf("ai calls = %d, want 2", aiClient.calls)
	}
	if len(chat.sent) != 1 || !strings.Contains(chat.sent[0], "mystery is funny") {
		t.Fatalf("expected retried reply, got %#v", chat.sent)
	}
}

func TestCleanReplyAddsTerminalPunctuation(t *testing.T) {
	got := cleanReply(`"just a little stardust"`)
	if got != "just a little stardust." {
		t.Fatalf("cleanReply = %q", got)
	}

	got = cleanReply("already complete!")
	if got != "already complete!" {
		t.Fatalf("cleanReply should preserve punctuation, got %q", got)
	}
}

func TestCleanReplyRemovesMarkdownMetaAndSpeakerLabels(t *testing.T) {
	got := cleanReply("LupusAria: **Ursa** *is* right here")
	if got != "Ursa is right here." {
		t.Fatalf("cleanReply = %q", got)
	}

	got = cleanReply("Reasoning: I should not send this\nActual answer")
	if got != "Actual answer." {
		t.Fatalf("cleanReply meta strip = %q", got)
	}
}

func TestCleanReplyRemovesEmoji(t *testing.T) {
	got := cleanReply("Pull up a star and stay awhile. 🏳️‍🌈")
	if got != "Pull up a star and stay awhile." {
		t.Fatalf("cleanReply emoji strip = %q", got)
	}
}

func TestSmartTruncateAvoidsMidSentenceCuts(t *testing.T) {
	got := smartTruncate("First sentence is good. Second sentence is going to run past the tiny limit.", 28)
	if got != "First sentence is good." {
		t.Fatalf("smartTruncate sentence = %q", got)
	}

	got = smartTruncate("This response has a useful clause, but then keeps going too long for chat.", 48)
	if got != "This response has a useful clause." {
		t.Fatalf("smartTruncate punctuation = %q", got)
	}
}

func TestLooksIncompleteReply(t *testing.T) {
	tests := []struct {
		reply string
		want  bool
	}{
		{reply: "I think that might be a question for the.", want: true},
		{reply: "The Judge definitely has a unique approach to legal.", want: true},
		{reply: "That seems legal.", want: false},
		{reply: "That is a question for Ursa.", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.reply, func(t *testing.T) {
			if got := looksIncompleteReply(tt.reply); got != tt.want {
				t.Fatalf("looksIncompleteReply(%q) = %v, want %v", tt.reply, got, tt.want)
			}
		})
	}
}

func testBot(chat *fakeChat) *Bot {
	return New(Config{
		Name:                  "LupusAria",
		Personality:           "test",
		EnableMentions:        true,
		EnableAsk:             true,
		EnableLurk:            true,
		EnableCommands:        true,
		EnableReset:           true,
		MaxContextMessages:    30,
		StreamContextTTL:      time.Minute,
		GlobalCooldown:        time.Second,
		UserCooldown:          time.Second,
		DailyBudgetUSD:        0,
		MonthlyBudgetUSD:      0,
		MaxRequestsPerHour:    0,
		InputPricePerMillion:  0,
		OutputPricePerMillion: 0,
	}, chat, fakeAI{}, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
