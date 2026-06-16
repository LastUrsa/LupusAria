package recentstreamers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"lupusaria/internal/twitch"
)

type fakeChat struct {
	sent    []string
	failFor map[string]bool
}

func (f *fakeChat) Say(_ string, text string) error {
	if f.failFor[text] {
		return errors.New("send failed")
	}
	f.sent = append(f.sent, text)
	return nil
}

type fakeHelix struct {
	users       map[string]twitch.UserInfo
	streamedAt  map[string]time.Time
	userCalls   int
	streamCalls int
}

func (f *fakeHelix) GetUsersByLogin(_ context.Context, logins []string) ([]twitch.UserInfo, error) {
	f.userCalls++
	var users []twitch.UserInfo
	for _, login := range logins {
		if user, ok := f.users[login]; ok {
			users = append(users, user)
		}
	}
	return users, nil
}

func (f *fakeHelix) GetRecentStream(_ context.Context, userID string) (time.Time, bool, error) {
	f.streamCalls++
	streamedAt, ok := f.streamedAt[userID]
	return streamedAt, ok, nil
}

func (f *fakeHelix) GetChatters(context.Context, string, string) ([]twitch.Chatter, error) {
	return nil, nil
}

func TestSnapshotAccruesOnlyWhilePresent(t *testing.T) {
	service := testService(&fakeChat{}, &fakeHelix{})
	start := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	service.ApplySnapshot(start, []ViewerIdentity{{Login: "Alice", DisplayName: "Alice"}})
	service.ApplySnapshot(start.Add(10*time.Minute), []ViewerIdentity{{Login: "Alice", DisplayName: "Alice"}})
	service.ApplySnapshot(start.Add(20*time.Minute), nil)
	service.ApplySnapshot(start.Add(30*time.Minute), []ViewerIdentity{{Login: "Alice", DisplayName: "Alice"}})

	candidates := service.viewerCandidates()
	if len(candidates) != 1 {
		t.Fatalf("expected Alice to reach watch threshold, got %d candidates", len(candidates))
	}
	if candidates[0].Watch != 10*time.Minute {
		t.Fatalf("watch = %s, want 10m", candidates[0].Watch)
	}
}

func TestBuildQueueFiltersAndSortsRecentStreamers(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	helix := &fakeHelix{
		users: map[string]twitch.UserInfo{
			"alice": {ID: "1", Login: "alice", DisplayName: "Alice"},
			"bob":   {ID: "2", Login: "bob", DisplayName: "Bob"},
			"cara":  {ID: "3", Login: "cara", DisplayName: "Cara"},
		},
		streamedAt: map[string]time.Time{
			"1": now.Add(-48 * time.Hour),
			"2": now.Add(-1 * time.Hour),
			"3": now.Add(-30 * 24 * time.Hour),
		},
	}
	service := testService(&fakeChat{}, helix)
	service.ApplySnapshot(now.Add(-20*time.Minute), []ViewerIdentity{
		{Login: "alice", DisplayName: "Alice"},
		{Login: "bob", DisplayName: "Bob"},
		{Login: "cara", DisplayName: "Cara"},
	})
	service.ApplySnapshot(now, []ViewerIdentity{
		{Login: "alice", DisplayName: "Alice"},
		{Login: "bob", DisplayName: "Bob"},
		{Login: "cara", DisplayName: "Cara"},
	})

	queue, err := service.buildQueue(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}

	got := []string{}
	for _, candidate := range queue {
		got = append(got, candidate.Login)
	}
	want := []string{"bob", "alice"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue = %#v, want %#v", got, want)
	}
}

func TestSendNextPagesAndMarksOnlySuccessfulShoutouts(t *testing.T) {
	chat := &fakeChat{failFor: map[string]bool{"!so @bob": true}}
	service := testService(chat, &fakeHelix{})
	service.cfg.PageSize = 2
	service.cfg.ShoutoutDelay = 0
	service.queue = []Candidate{
		{Login: "alice"},
		{Login: "bob"},
		{Login: "cara"},
	}

	service.sendNext(context.Background(), "lastursa")

	wantSent := []string{
		"Shouting out 2 streamer(s). 1 left in queue.",
		"!so @alice",
	}
	if !reflect.DeepEqual(chat.sent, wantSent) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, wantSent)
	}
	if !service.shoutedThisRun["alice"] {
		t.Fatal("successful shoutout should be marked")
	}
	if service.shoutedThisRun["bob"] {
		t.Fatal("failed shoutout should not be marked")
	}

	service.sendNext(context.Background(), "lastursa")
	if got := chat.sent[len(chat.sent)-1]; got != "!so @cara" {
		t.Fatalf("next page last message = %q, want !so @cara", got)
	}
}

func TestQueueUsesCacheOnSecondBuild(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	helix := &fakeHelix{
		users: map[string]twitch.UserInfo{
			"alice": {ID: "1", Login: "alice", DisplayName: "Alice"},
		},
		streamedAt: map[string]time.Time{"1": now.Add(-time.Hour)},
	}
	service := testService(&fakeChat{}, helix)
	service.ApplySnapshot(now.Add(-20*time.Minute), []ViewerIdentity{{Login: "alice", DisplayName: "Alice"}})
	service.ApplySnapshot(now, []ViewerIdentity{{Login: "alice", DisplayName: "Alice"}})

	if _, err := service.buildQueue(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := service.buildQueue(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if helix.userCalls != 1 {
		t.Fatalf("user calls = %d, want 1", helix.userCalls)
	}
	if helix.streamCalls != 1 {
		t.Fatalf("stream calls = %d, want 1", helix.streamCalls)
	}
}

func TestRecentStreamersCommandSimulation(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	helix := &fakeHelix{
		users: map[string]twitch.UserInfo{
			"the_polar_pop": {ID: "1", Login: "the_polar_pop", DisplayName: "the_polar_pop"},
			"parfaitfair":   {ID: "2", Login: "parfaitfair", DisplayName: "ParfaitFair"},
			"dozyjinro":     {ID: "3", Login: "dozyjinro", DisplayName: "DozyJinro"},
		},
		streamedAt: map[string]time.Time{
			"1": now.Add(-1 * time.Hour),
			"2": now.Add(-3 * time.Hour),
			"3": now.Add(-2 * time.Hour),
		},
	}
	chat := &fakeChat{}
	service := testService(chat, helix)
	service.cfg.PageSize = 5

	service.ApplySnapshot(now.Add(-20*time.Minute), []ViewerIdentity{
		{Login: "the_polar_pop", DisplayName: "the_polar_pop"},
		{Login: "parfaitfair", DisplayName: "ParfaitFair"},
		{Login: "dozyjinro", DisplayName: "DozyJinro"},
	})
	service.ApplySnapshot(now, []ViewerIdentity{
		{Login: "the_polar_pop", DisplayName: "the_polar_pop"},
		{Login: "parfaitfair", DisplayName: "ParfaitFair"},
		{Login: "dozyjinro", DisplayName: "DozyJinro"},
	})

	queue, err := service.buildQueue(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	service.queue = queue
	service.sendNext(context.Background(), "lastursa")

	want := []string{
		"Shouting out 3 streamer(s). 0 left in queue.",
		"!so @the_polar_pop",
		"!so @dozyjinro",
		"!so @parfaitfair",
	}
	if !reflect.DeepEqual(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func testService(chat *fakeChat, helix *fakeHelix) *Service {
	return New(Config{
		Channel:       "lastursa",
		MinWatch:      10 * time.Minute,
		RecentWindow:  14 * 24 * time.Hour,
		PageSize:      5,
		ShoutoutDelay: 0,
		CacheTTL:      6 * time.Hour,
	}, chat, helix, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
