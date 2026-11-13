package bot

import (
	"context"
	"log/slog"
	"lupusaria/internal/raffle"
	"lupusaria/internal/twitch"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type raffleStream struct{}

func (raffleStream) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return twitch.StreamInfo{ID: "A", Channel: "channel", Live: true}, nil
}
func TestRaffleCommandsUseAuthenticatedIdentityAndBypassAIHistory(t *testing.T) {
	repo, err := raffle.Open(filepath.Join(t.TempDir(), "raffle.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service, err := raffle.New(repo, "b", raffle.Options{Random: func(int) (int, error) { return 1, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Reconcile(context.Background(), raffleStream{}, "channel", true); err != nil {
		t.Fatal(err)
	}
	chat := &wrapChat{}
	b := New(Config{Name: "bot", BroadcasterID: "b", Channel: "channel", Raffle: service, EnableCommands: true, MaxContextMessages: 5}, chat, wrapAI{}, nil, nil, nil, slog.Default())
	msg := twitch.Message{ID: "a", Channel: "channel", UserID: "u", Username: "viewer", DisplayName: "ViewerA", Text: "!rafadd OtherUser"}
	b.handleMessage(context.Background(), msg)
	if len(chat.sent) != 1 || chat.sent[0] != "ViewerA added to raffle" {
		t.Fatal(chat.sent)
	}
	msg.ID = "again"
	msg.DisplayName = "Changed"
	b.handleMessage(context.Background(), msg)
	if len(chat.sent) != 1 {
		t.Fatal("duplicate replied")
	}
	for _, mod := range []bool{false, true} {
		msg.ID = "denied"
		msg.Text = "!rafclear"
		msg.IsMod = mod
		msg.IsBroadcaster = true
		msg.Username = "channel"
		b.handleMessage(context.Background(), msg)
	}
	snap, _ := service.Snapshot()
	if len(snap.Entries) != 1 {
		t.Fatal("spoofed broadcaster cleared")
	}
	msg.ID = "pick"
	msg.UserID = "b"
	msg.IsBroadcaster = false
	msg.Username = "different-login"
	msg.Text = "!rafpick"
	b.handleMessage(context.Background(), msg)
	if chat.sent[len(chat.sent)-1] != "Congratulations ViewerA you are the winner!" {
		t.Fatal(chat.sent)
	}
	msg.ID = "clear"
	msg.Text = "!rafclear"
	b.handleMessage(context.Background(), msg)
	if chat.sent[len(chat.sent)-1] != "Raffle cleared." {
		t.Fatal(chat.sent)
	}
	snap, _ = service.Snapshot()
	if snap.Winner != nil || len(snap.Entries) != 0 {
		t.Fatal("clear failed")
	}
	if len(b.recentContext()) != 0 {
		t.Fatal("raffle identity retained in AI context")
	}
}
func TestRaffleLoggingRedactsIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	inner := &wrapChat{}
	chat := WithChatLogging(inner, path, slog.Default(), "bot").(*loggingChat)
	// Incoming raffle commands are excluded before write in Connect. Test the
	// classification and the outgoing transport path used by actual commands.
	if !isRaffleCommand("!RAFADD somebody") || isRaffleCommand("!rafaddextra") {
		t.Fatal("classification")
	}
	for _, reply := range []string{"ViewerA added to raffle", "Congratulations ViewerA you are the winner!", "Raffle cleared."} {
		if err := chat.Say("channel", reply); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ViewerA") {
		t.Fatal("participant identity logged")
	}
	if !strings.Contains(string(data), "Raffle cleared.") {
		t.Fatal("ordinary logging broken")
	}
}

type echoRaffleChat struct {
	wrapChat
	incoming chan twitch.Message
}

func (c *echoRaffleChat) Connect(context.Context) (<-chan twitch.Message, error) {
	return c.incoming, nil
}
func TestIncomingRaffleCommandsAndEchoesDoNotEnterTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.jsonl")
	inner := &echoRaffleChat{incoming: make(chan twitch.Message, 4)}
	for _, text := range []string{"!rafadd", "ViewerA added to raffle", "Congratulations ViewerA you are the winner!", "Ordinary chat"} {
		inner.incoming <- twitch.Message{Username: "ViewerA", DisplayName: "ViewerA", Text: text}
	}
	close(inner.incoming)
	chat := WithChatLogging(inner, path, slog.Default(), "bot")
	messages, err := chat.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range messages {
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "added to raffle") || strings.Contains(string(data), "winner!") || strings.Contains(string(data), "!rafadd") {
		t.Fatal("raffle echo or command retained")
	}
	if !strings.Contains(string(data), "Ordinary chat") {
		t.Fatal("unrelated chat missing")
	}
}
