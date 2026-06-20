package recentstreamers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
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
	following   map[string]bool
	streamInfo  twitch.StreamInfo
	userCalls   int
	streamCalls int
	followCalls int
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

func (f *fakeHelix) IsChannelFollower(_ context.Context, _ string, userID string) (bool, error) {
	f.followCalls++
	if f.following == nil {
		return true, nil
	}
	return f.following[userID], nil
}

func (f *fakeHelix) GetChatters(context.Context, string, string) ([]twitch.Chatter, error) {
	return nil, nil
}

func (f *fakeHelix) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return f.streamInfo, nil
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

func TestNewDefaultsShoutoutDelayToFiveSeconds(t *testing.T) {
	service := testService(&fakeChat{}, &fakeHelix{})

	if service.cfg.ShoutoutDelay != 5*time.Second {
		t.Fatalf("shoutout delay = %s, want 5s", service.cfg.ShoutoutDelay)
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
	if !slices.Equal(got, want) {
		t.Fatalf("queue = %#v, want %#v", got, want)
	}
}

func TestBuildQueueExcludesChannelOwner(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	helix := &fakeHelix{
		users: map[string]twitch.UserInfo{
			"lastursa": {ID: "owner", Login: "lastursa", DisplayName: "LastUrsa"},
			"alice":    {ID: "1", Login: "alice", DisplayName: "Alice"},
		},
		streamedAt: map[string]time.Time{
			"owner": now.Add(-30 * time.Minute),
			"1":     now.Add(-1 * time.Hour),
		},
	}
	service := testService(&fakeChat{}, helix)
	service.ApplySnapshot(now.Add(-20*time.Minute), []ViewerIdentity{
		{Login: "lastursa", DisplayName: "LastUrsa"},
		{Login: "alice", DisplayName: "Alice"},
	})
	service.ApplySnapshot(now, []ViewerIdentity{
		{Login: "lastursa", DisplayName: "LastUrsa"},
		{Login: "alice", DisplayName: "Alice"},
	})

	queue, err := service.buildQueue(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}

	if len(queue) != 1 {
		t.Fatalf("queue = %#v, want one candidate", queue)
	}
	if queue[0].Login != "alice" {
		t.Fatalf("queue included channel owner or wrong candidate: %#v", queue)
	}
}

func TestBuildQueueRequiresFollowers(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	helix := &fakeHelix{
		users: map[string]twitch.UserInfo{
			"alice": {ID: "1", Login: "alice", DisplayName: "Alice"},
			"bob":   {ID: "2", Login: "bob", DisplayName: "Bob"},
		},
		streamedAt: map[string]time.Time{
			"1": now.Add(-1 * time.Hour),
			"2": now.Add(-2 * time.Hour),
		},
		following: map[string]bool{
			"1": true,
			"2": false,
		},
	}
	service := testService(&fakeChat{}, helix)
	service.ApplySnapshot(now.Add(-20*time.Minute), []ViewerIdentity{
		{Login: "alice", DisplayName: "Alice"},
		{Login: "bob", DisplayName: "Bob"},
	})
	service.ApplySnapshot(now, []ViewerIdentity{
		{Login: "alice", DisplayName: "Alice"},
		{Login: "bob", DisplayName: "Bob"},
	})

	queue, err := service.buildQueue(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}

	if len(queue) != 1 || queue[0].Login != "alice" {
		t.Fatalf("queue = %#v, want only follower alice", queue)
	}
	if helix.followCalls != 2 {
		t.Fatalf("follower checks = %d, want 2", helix.followCalls)
	}
	if helix.streamCalls != 1 {
		t.Fatalf("stream checks = %d, want only follower stream checked", helix.streamCalls)
	}
}

func TestStatusExcludesChannelOwnerFromWatchedCount(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	service := testService(&fakeChat{}, &fakeHelix{})
	service.ApplySnapshot(now.Add(-20*time.Minute), []ViewerIdentity{
		{Login: "lastursa", DisplayName: "LastUrsa"},
		{Login: "alice", DisplayName: "Alice"},
	})
	service.ApplySnapshot(now, []ViewerIdentity{
		{Login: "lastursa", DisplayName: "LastUrsa"},
		{Login: "alice", DisplayName: "Alice"},
	})

	got := service.status()
	if !strings.Contains(got, "1 viewers over") {
		t.Fatalf("status should count only non-owner viewers, got %q", got)
	}
}

func TestHandleCommandAllowsAuthorizedStatus(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.Permission = "mods"

	handled := service.HandleCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "modfriend",
		DisplayName: "ModFriend",
		Text:        "!autoso status",
		IsMod:       true,
	})

	if !handled {
		t.Fatal("expected !autoso status to be handled")
	}
	if len(chat.sent) != 1 || !strings.Contains(chat.sent[0], "Streamer tracker:") {
		t.Fatalf("sent = %#v", chat.sent)
	}
}

func TestHandleCommandRejectsUnauthorizedStatus(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.Permission = "mods"

	handled := service.HandleCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!autoso status",
	})

	if !handled {
		t.Fatal("expected !autoso status to be handled")
	}
	if len(chat.sent) != 1 || chat.sent[0] != "Only mods or the broadcaster can run !autoso." {
		t.Fatalf("sent = %#v", chat.sent)
	}
}

func TestHandleCommandUsesSeparateSORoulettePermission(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.Permission = "broadcaster"
	service.cfg.SORoulettePermission = "everyone"
	service.cfg.ShoutoutDelay = 0
	service.cfg.RouletteStreamers = []string{"alice", "bob", "cara", "dane", "evie"}

	handled := service.HandleCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!soroulette",
	})

	if !handled {
		t.Fatal("expected !soroulette to be handled")
	}
	waitForSent(t, chat, 6)
	if len(chat.sent) != 6 {
		t.Fatalf("sent = %#v, want roulette summary plus five shoutouts", chat.sent)
	}
}

func TestHandleCommandRejectsUnauthorizedSORoulette(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.Permission = "everyone"
	service.cfg.SORoulettePermission = "broadcaster"

	handled := service.HandleCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "viewer",
		DisplayName: "Viewer",
		Text:        "!soroulette",
	})

	if !handled {
		t.Fatal("expected !soroulette to be handled")
	}
	if len(chat.sent) != 1 || chat.sent[0] != "Only the broadcaster can run !soroulette." {
		t.Fatalf("sent = %#v", chat.sent)
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
	if !slices.Equal(chat.sent, wantSent) {
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

func TestSendNextSkipsAlreadyShoutedStreamer(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.ShoutoutDelay = 0
	service.queue = []Candidate{
		{Login: "alice"},
		{Login: "bob"},
	}
	service.markShouted("alice")

	service.sendNext(context.Background(), "lastursa")

	want := []string{
		"Shouting out 1 streamer(s). 0 left in queue.",
		"!so @bob",
	}
	if !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func TestSendNextBackfillsAlreadyShoutedStreamer(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.PageSize = 5
	service.cfg.ShoutoutDelay = 0
	service.queue = []Candidate{
		{Login: "alice"},
		{Login: "bob"},
		{Login: "cara"},
		{Login: "dane"},
		{Login: "evie"},
		{Login: "finn"},
	}
	service.markShouted("cara")

	service.sendNext(context.Background(), "lastursa")

	want := []string{
		"Shouting out 5 streamer(s). 0 left in queue.",
		"!so @alice",
		"!so @bob",
		"!so @dane",
		"!so @evie",
		"!so @finn",
	}
	if !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func TestSORouletteSelectsConfiguredUnshoutedStreamers(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.ShoutoutDelay = 0
	service.cfg.RouletteStreamers = []string{"alice", "bob", "cara", "dane", "evie", "finn"}
	service.markShouted("bob")

	service.sendRoulette(context.Background(), "lastursa")

	if len(chat.sent) != 6 {
		t.Fatalf("sent = %#v, want summary plus five shoutouts", chat.sent)
	}
	if chat.sent[0] != "Roulette picked 5 streamer(s)." {
		t.Fatalf("summary = %q", chat.sent[0])
	}
	seen := map[string]bool{}
	for _, text := range chat.sent[1:] {
		if text == "!so @bob" {
			t.Fatalf("roulette included already shouted streamer: %#v", chat.sent)
		}
		if seen[text] {
			t.Fatalf("duplicate roulette shoutout %q in %#v", text, chat.sent)
		}
		seen[text] = true
	}
}

func TestSORouletteBackfillsAlreadyShoutedStreamers(t *testing.T) {
	chat := &fakeChat{}
	service := testService(chat, &fakeHelix{})
	service.cfg.ShoutoutDelay = 0
	service.cfg.RouletteStreamers = []string{"alice", "bob", "cara", "dane", "evie", "finn", "gail"}
	service.markShouted("alice")
	service.markShouted("dane")

	service.sendRoulette(context.Background(), "lastursa")

	if len(chat.sent) != 6 {
		t.Fatalf("sent = %#v, want summary plus five shoutouts", chat.sent)
	}
	if chat.sent[0] != "Roulette picked 5 streamer(s)." {
		t.Fatalf("summary = %q", chat.sent[0])
	}
	for _, text := range chat.sent[1:] {
		if text == "!so @alice" || text == "!so @dane" {
			t.Fatalf("roulette included already shouted streamer: %#v", chat.sent)
		}
	}
}

func TestShoutoutLedgerResetsForNewStream(t *testing.T) {
	started := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
	chat := &fakeChat{}
	helix := &fakeHelix{streamInfo: twitch.StreamInfo{Live: true, StartedAt: started}}
	service := testService(chat, helix)
	service.cfg.ShoutoutDelay = 0
	service.queue = []Candidate{{Login: "alice"}}

	service.sendNext(context.Background(), "lastursa")
	if !service.shoutedThisRun["alice"] {
		t.Fatal("alice should be marked shouted")
	}

	helix.streamInfo = twitch.StreamInfo{Live: true, StartedAt: started.Add(24 * time.Hour)}
	if err := service.syncStreamRun(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.queue = []Candidate{{Login: "alice"}}
	service.sendNext(context.Background(), "lastursa")

	wantLast := "!so @alice"
	if got := chat.sent[len(chat.sent)-1]; got != wantLast {
		t.Fatalf("last sent = %q, want %q after new stream reset", got, wantLast)
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
	service.cfg.ShoutoutDelay = 0

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
	if !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func testService(chat *fakeChat, helix *fakeHelix) *Service {
	return New(Config{
		Channel:       "lastursa",
		BroadcasterID: "broadcaster",
		MinWatch:      10 * time.Minute,
		RecentWindow:  14 * 24 * time.Hour,
		PageSize:      5,
		ShoutoutDelay: 0,
		CacheTTL:      6 * time.Hour,
	}, chat, helix, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func waitForSent(t *testing.T, chat *fakeChat, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(chat.sent) >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
