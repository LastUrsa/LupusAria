package announcements

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lupusaria/internal/twitch"
)

type fakeChat struct {
	sent []string
}

func (f *fakeChat) Say(_ string, text string) error {
	f.sent = append(f.sent, text)
	return nil
}

type fakeStream struct {
	info twitch.StreamInfo
	err  error
}

func (f fakeStream) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return f.info, f.err
}

func TestSaveAndLoadRoundTripWithOwnerOnlyMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "announcements.json")
	items := []Announcement{{
		ID:      "music",
		Enabled: true,
		Kind:    KindCommand,
		Command: " !Music ",
		Message: "Music links are in chat.",
	}}

	if err := Save(path, items); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Command != "!music" {
		t.Fatalf("loaded = %#v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestHandleCommandSendsBroadcasterAnnouncement(t *testing.T) {
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Channel: "lastursa",
		Items: []Announcement{{
			ID:      "music",
			Enabled: true,
			Kind:    KindCommand,
			Command: "!music",
			Message: "Ursa's music is on Bandcamp.",
		}},
	}, chat, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	handled := service.HandleCommand(context.Background(), twitch.Message{Channel: "lastursa", Text: "!music"}, true)

	if !handled {
		t.Fatal("expected command to be handled")
	}
	if len(chat.sent) != 1 || chat.sent[0] != "Ursa's music is on Bandcamp." {
		t.Fatalf("sent = %#v", chat.sent)
	}
}

func TestHandleCommandRejectsNonBroadcaster(t *testing.T) {
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Items:   []Announcement{{Enabled: true, Kind: KindCommand, Command: "!music", Message: "Music."}},
	}, chat, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	handled := service.HandleCommand(context.Background(), twitch.Message{Channel: "lastursa", Text: "!music"}, false)

	if !handled {
		t.Fatal("expected command to be handled")
	}
	if len(chat.sent) != 1 || chat.sent[0] != "Only the broadcaster can use announcement commands." {
		t.Fatalf("sent = %#v", chat.sent)
	}
}

func TestCheckTimersUsesStreamStartAndSendsOnce(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Channel: "lastursa",
		Items: []Announcement{{
			ID:           "hydrate",
			Enabled:      true,
			Kind:         KindTimer,
			AfterMinutes: 15,
			Message:      "Hydrate check.",
		}},
	}, chat, fakeStream{info: twitch.StreamInfo{Live: true, StartedAt: started}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.started = started
	service.now = func() time.Time { return started.Add(20 * time.Minute) }

	service.checkTimers(context.Background())
	service.checkTimers(context.Background())

	if len(chat.sent) != 1 || chat.sent[0] != "Hydrate check." {
		t.Fatalf("sent = %#v", chat.sent)
	}
}

func TestCheckTimersRepeatsOnInterval(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Channel: "lastursa",
		Items: []Announcement{{
			ID:            "hydrate",
			Enabled:       true,
			Kind:          KindTimer,
			AfterMinutes:  30,
			RepeatMinutes: 30,
			Message:       "Hydrate check.",
		}},
	}, chat, fakeStream{info: twitch.StreamInfo{Live: true, StartedAt: started}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.started = started

	service.now = func() time.Time { return started.Add(35 * time.Minute) }
	service.checkTimers(context.Background())
	service.checkTimers(context.Background())
	service.now = func() time.Time { return started.Add(65 * time.Minute) }
	service.checkTimers(context.Background())

	if len(chat.sent) != 2 {
		t.Fatalf("sent = %#v, want 2 messages", chat.sent)
	}
}

func TestCheckTimersDoesNotCatchUpMissedRepeatSlots(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Channel: "lastursa",
		Items: []Announcement{{
			ID:            "hydrate",
			Enabled:       true,
			Kind:          KindTimer,
			AfterMinutes:  30,
			RepeatMinutes: 30,
			Message:       "Hydrate check.",
		}},
	}, chat, fakeStream{info: twitch.StreamInfo{Live: true, StartedAt: started}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.started = started.Add(75 * time.Minute)
	service.now = func() time.Time { return started.Add(80 * time.Minute) }

	service.checkTimers(context.Background())

	if len(chat.sent) != 0 {
		t.Fatalf("sent = %#v", chat.sent)
	}

	service.now = func() time.Time { return started.Add(95 * time.Minute) }
	service.checkTimers(context.Background())

	if len(chat.sent) != 1 {
		t.Fatalf("sent after next live slot = %#v, want one message", chat.sent)
	}
}

func TestCheckTimersDoesNotCatchUpMissedAnnouncements(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Channel: "lastursa",
		Items: []Announcement{{
			ID:           "missed",
			Enabled:      true,
			Kind:         KindTimer,
			AfterMinutes: 15,
			Message:      "Missed.",
		}},
	}, chat, fakeStream{info: twitch.StreamInfo{Live: true, StartedAt: started}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.started = started.Add(30 * time.Minute)
	service.now = func() time.Time { return started.Add(40 * time.Minute) }

	service.checkTimers(context.Background())
	service.checkTimers(context.Background())

	if len(chat.sent) != 0 {
		t.Fatalf("sent = %#v", chat.sent)
	}
}

func TestCheckTimersWaitsUntilDue(t *testing.T) {
	started := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	chat := &fakeChat{}
	service := New(Config{
		Enabled: true,
		Channel: "lastursa",
		Items:   []Announcement{{ID: "later", Enabled: true, Kind: KindTimer, AfterMinutes: 30, Message: "Later."}},
	}, chat, fakeStream{info: twitch.StreamInfo{Live: true, StartedAt: started}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.started = started
	service.now = func() time.Time { return started.Add(20 * time.Minute) }

	service.checkTimers(context.Background())

	if len(chat.sent) != 0 {
		t.Fatalf("sent = %#v", chat.sent)
	}
}
