package bot

import (
	"context"
	"strings"
	"testing"

	"lupusaria/internal/twitch"
)

type liveSettingsChat struct {
	fakeChat
	messages chan twitch.Message
}

func (c *liveSettingsChat) Connect(context.Context) (<-chan twitch.Message, error) {
	return c.messages, nil
}

func TestSettingsApplyBetweenMessagesOnConnectedChat(t *testing.T) {
	chat := &liveSettingsChat{messages: make(chan twitch.Message)}
	cfg := testConfig()
	b := New(cfg, chat, fakeAI{}, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	updates := make(chan SettingsUpdate)
	stopped := make(chan error, 1)
	go func() {
		stopped <- b.RunWithSettings(ctx, updates, nil, func(context.Context) error {
			next := cfg
			next.EnableAsk = false
			next.LurkPermission = "broadcaster"
			b.SetCommandSettings(next)
			return nil
		})
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	send := func(text string) {
		t.Helper()
		chat.messages <- twitch.Message{Channel: "channel", Username: "viewer", Text: text}
	}
	send("!commands")
	if list := waitForSent(t, &chat.fakeChat, 1)[0]; !strings.Contains(list, "!ask") {
		t.Fatalf("initial command list = %q", list)
	}

	update := SettingsUpdate{Done: make(chan error, 1)}
	updates <- update
	if err := <-update.Done; err != nil {
		t.Fatal(err)
	}
	send("!commands")
	send("!lurk")
	sent := waitForSent(t, &chat.fakeChat, 3)
	if strings.Contains(sent[1], "!ask") || !strings.Contains(sent[2], "broadcaster") {
		t.Fatalf("messages after updating settings = %q", sent[1:])
	}
}
