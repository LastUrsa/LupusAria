package bot

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/twitch"
	"lupusaria/internal/wrapup"
)

type wrapChat struct{ sent []string }

func (c *wrapChat) Connect(context.Context) (<-chan twitch.Message, error) { return nil, nil }
func (c *wrapChat) Say(_ string, text string) error                        { c.sent = append(c.sent, text); return nil }
func (c *wrapChat) Close() error                                           { return nil }

type wrapAI struct{}

func (wrapAI) Complete(context.Context, []ai.Message) (ai.Response, error) { return ai.Response{}, nil }

func TestWrapUpCommandPermissionReadOnlyAndConfirmedReset(t *testing.T) {
	s, _ := wrapup.New(filepath.Join(t.TempDir(), "state.json"), nil)
	_ = s.Online("stream", "b", time.Now())
	_ = s.Record(wrapup.Event{MessageID: "f", Type: "follow", UserID: "u", Name: "Follower", At: time.Now()})
	chat := &wrapChat{}
	b := New(Config{Name: "bot", Channel: "channel", EnableCommands: true, WrapUpPermission: "broadcaster", WrapUp: s, MaxContextMessages: 5}, chat, wrapAI{}, nil, nil, nil, slog.Default())
	viewer := twitch.Message{Channel: "channel", UserID: "viewer", Username: "viewer", Text: "!wrapup stats"}
	if !b.handlePublicCommand(context.Background(), viewer) || !strings.Contains(chat.sent[0], "Only the broadcaster") {
		t.Fatalf("permission not enforced: %#v", chat.sent)
	}
	broadcaster := twitch.Message{Channel: "channel", UserID: "b", Username: "channel", IsBroadcaster: true, Text: "!wrapup"}
	b.handlePublicCommand(context.Background(), broadcaster)
	r, _ := s.Current()
	if r.FollowerCount != 1 || r.Status != "live" {
		t.Fatalf("ordinary wrapup mutated session: %#v", r)
	}
	broadcaster.Text = "!wrapup reset confirm"
	b.handlePublicCommand(context.Background(), broadcaster)
	r, _ = s.Current()
	if r.FollowerCount != 1 {
		t.Fatal("reset happened without first confirmation")
	}
	broadcaster.Text = "!wrapup reset"
	b.handlePublicCommand(context.Background(), broadcaster)
	broadcaster.Text = "!wrapup reset confirm"
	b.handlePublicCommand(context.Background(), broadcaster)
	r, _ = s.Current()
	if r.FollowerCount != 0 || r.Status != "live" {
		t.Fatalf("confirmed reset failed or changed lifecycle: %#v", r)
	}
}

func TestWrapUpCommandRespectsCommandsDisabled(t *testing.T) {
	s, _ := wrapup.New(filepath.Join(t.TempDir(), "s.json"), nil)
	_ = s.Online("s", "b", time.Now())
	chat := &wrapChat{}
	b := New(Config{Name: "bot", EnableCommands: false, WrapUp: s}, chat, wrapAI{}, nil, nil, nil, slog.Default())
	if b.handlePublicCommand(context.Background(), twitch.Message{Channel: "c", Username: "c", IsBroadcaster: true, Text: "!wrapup"}) {
		t.Fatal("disabled command executed")
	}
}

func TestWrapUpConfiguredPermissionAndChatSummaryLength(t *testing.T) {
	s, _ := wrapup.New(filepath.Join(t.TempDir(), "s.json"), nil)
	_ = s.Online("s", "b", time.Now())
	chat := &wrapChat{}
	b := New(Config{Name: "bot", EnableCommands: true, WrapUpPermission: "everyone", WrapUp: s}, chat, wrapAI{}, nil, nil, nil, slog.Default())
	msg := twitch.Message{Channel: "channel", UserID: "viewer", Username: "viewer", Text: "!wrapup preview"}
	if !b.handlePublicCommand(context.Background(), msg) || len(chat.sent) != 1 {
		t.Fatalf("configured permission was not respected: %#v", chat.sent)
	}
	if len(chat.sent[0]) > maxTwitchReplyLength || !strings.Contains(chat.sent[0], "Full wrap-up is ready") {
		t.Fatalf("invalid chat summary (%d chars): %q", len(chat.sent[0]), chat.sent[0])
	}
}
