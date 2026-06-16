package bot

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"lupusaria/internal/ai"
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

type fakeStreamProvider struct {
	info twitch.StreamInfo
}

func (f fakeStreamProvider) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return f.info, nil
}

func TestPublicBotCommandDoesNotExposeCost(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)

	handled := b.handlePublicCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!bot",
	})

	if !handled {
		t.Fatal("expected !bot to be handled as a public command")
	}
	if len(chat.sent) != 1 {
		t.Fatalf("expected one chat response, got %d", len(chat.sent))
	}
	lower := strings.ToLower(chat.sent[0])
	for _, forbidden := range []string{"cost", "budget", "token", "secret", "key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("public help exposed private term %q in %q", forbidden, chat.sent[0])
		}
	}
}

func TestResetRequiresBroadcaster(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)
	b.context = []twitch.Message{{Text: "keep me"}}

	handled := b.handlePublicCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!reset",
	})

	if !handled {
		t.Fatal("expected !reset to be handled")
	}
	if len(b.context) != 1 {
		t.Fatal("non-broadcaster reset should not clear context")
	}
	if len(chat.sent) != 1 || !strings.Contains(chat.sent[0], "Only the broadcaster") {
		t.Fatalf("unexpected reset response: %#v", chat.sent)
	}
}

func TestResetAllowsBroadcaster(t *testing.T) {
	chat := &fakeChat{}
	b := testBot(chat)
	b.context = []twitch.Message{{Text: "clear me"}}

	handled := b.handlePublicCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "lastursa",
		DisplayName: "LastUrsa",
		Text:        "!reset",
	})

	if !handled {
		t.Fatal("expected !reset to be handled")
	}
	if len(b.context) != 0 {
		t.Fatal("broadcaster reset should clear context")
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

func TestBuildAIMessagesIncludesStreamContext(t *testing.T) {
	chat := &fakeChat{}
	b := New(Config{
		Name:                  "LupusAria",
		Personality:           "test",
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
	}}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

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

func testBot(chat *fakeChat) *Bot {
	return New(Config{
		Name:                  "LupusAria",
		Personality:           "test",
		MaxContextMessages:    30,
		StreamContextTTL:      time.Minute,
		GlobalCooldown:        time.Second,
		UserCooldown:          time.Second,
		DailyBudgetUSD:        0,
		MonthlyBudgetUSD:      0,
		MaxRequestsPerHour:    0,
		InputPricePerMillion:  0,
		OutputPricePerMillion: 0,
	}, chat, fakeAI{}, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
