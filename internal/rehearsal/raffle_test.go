package rehearsal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/bot"
	"lupusaria/internal/raffle"
	"lupusaria/internal/twitch"
)

type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *testClock) advance(d time.Duration) { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

type runtime struct {
	service             *raffle.Service
	repo                *raffle.Repository
	cancel              context.CancelFunc
	done, lifecycleDone chan struct{}
}

func startRuntime(t *testing.T, path string, clock *testClock) *runtime {
	t.Helper()
	repo, err := raffle.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := raffle.New(repo, "owner", raffle.Options{Now: clock.now, Random: func(int) (int, error) { return 1, nil }})
	if err != nil {
		t.Fatal(err)
	}
	provider := twitch.NewHelixClient("rehearsal-client", "rehearsal-token")
	// An unavailable startup is deliberately recoverable rather than fatal.
	_ = service.Reconcile(context.Background(), provider, "rehearsal", true)
	signals := make(chan twitch.StreamSignal, 32)
	chat := twitch.NewEventSubChatClient(twitch.EventSubConfig{ClientID: "rehearsal-client", Token: "rehearsal-token", Channel: "rehearsal", BroadcasterID: "owner", UserID: "bot", StreamSignals: signals}, nil)
	chat.SetMessageObserver(func(msg *twitch.Message) {
		f, _ := service.Capture()
		msg.RaffleSession = f.SessionID
		msg.RaffleGeneration = f.Generation
		msg.RaffleFenced = true
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instance := bot.New(bot.Config{Name: "bot", Channel: "rehearsal", BroadcasterID: "owner", Raffle: service, EnableCommands: true, ChatLogPath: filepath.Join(filepath.Dir(path), "chat.jsonl")}, bot.WithChatLogging(chat, filepath.Join(filepath.Dir(path), "chat.jsonl"), logger, "bot"), ai.MockClient{}, provider, nil, nil, logger)
	ctx, cancel := context.WithCancel(context.Background())
	r := &runtime{service: service, repo: repo, cancel: cancel, done: make(chan struct{}), lifecycleDone: make(chan struct{})}
	go func() {
		defer close(r.lifecycleDone)
		service.Run(ctx, provider, "rehearsal", signals, time.Minute, func(error) {})
	}()
	go func() {
		defer close(r.done)
		err := instance.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("bot stopped: %v", err)
		}
	}()
	return r
}
func (r *runtime) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	for _, done := range []chan struct{}{r.done, r.lifecycleDone} {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("rehearsal shutdown timed out")
		}
	}
	if err := r.repo.Close(); err != nil {
		t.Fatal(err)
	}
}
func eventually(t *testing.T, description string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out: " + description)
}
func snapshot(t *testing.T, r *runtime) raffle.Snapshot {
	t.Helper()
	v, err := r.service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func barrier(t *testing.T, s *twitchSimulator, id string) {
	t.Helper()
	before, _ := s.outputs()
	s.command(t, "barrier-"+id, "observer", "!commands", false)
	eventually(t, "chat command barrier", func() bool {
		sent, _ := s.outputs()
		return len(sent) > len(before) && strings.HasPrefix(sent[len(sent)-1], "Commands:")
	})
}
func waitConnected(t *testing.T, s *twitchSimulator) {
	t.Helper()
	eventually(t, "three EventSub subscriptions", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		kinds := map[string]bool{}
		for _, p := range s.peers {
			for kind := range p.types {
				kinds[kind] = true
			}
		}
		return kinds["channel.chat.message"] && kinds["stream.online"] && kinds["stream.offline"]
	})
}
func event(t *testing.T, s *twitchSimulator, r *runtime, kind, id string) {
	t.Helper()
	_, before := s.counts()
	s.notify(t, kind, id, map[string]any{"id": "obsolete-event-stream", "broadcaster_user_id": "owner"})
	eventually(t, "fresh Twitch verification", func() bool { _, polls := s.counts(); return polls > before })
	_ = snapshot(t, r)
}

type traceStep struct {
	Label    string          `json:"label"`
	Snapshot raffle.Snapshot `json:"snapshot"`
}

func TestRaffleRehearsal(t *testing.T) {
	s := newSimulator(t)
	clock := &testClock{at: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "isolated-raffle.sqlite")
	r := startRuntime(t, path, clock)
	defer func() {
		if r != nil {
			r.stop(t)
		}
	}()
	waitConnected(t, s)
	var trace []traceStep
	record := func(label string) { trace = append(trace, traceStep{label, snapshot(t, r)}); t.Log(label) }
	record("inactive")
	s.command(t, "offline-entry", "Viewer1", "!rafadd", false)
	barrier(t, s, "offline")
	if len(snapshot(t, r).Entries) != 0 {
		t.Fatal("offline enrollment")
	}
	s.configure("broadcast-A", "")
	event(t, s, r, "stream.online", "online-A")
	record("active-empty")
	for i := 1; i <= 5; i++ {
		s.command(t, fmt.Sprint(i), fmt.Sprintf("Viewer%d", i), "!rafadd SomeoneElse", false)
	}
	barrier(t, s, "five")
	if len(snapshot(t, r).Entries) != 5 {
		t.Fatal("five entries missing")
	}
	record("five-entries")
	s.command(t, "1", "Viewer1", "!rafadd", false)
	s.command(t, "duplicate-viewer", "Viewer1", "!rafadd", false)
	s.command(t, "denied-clear", "Viewer2", "!rafclear", true)
	s.command(t, "denied-pick", "Viewer3", "!rafpick", false)
	barrier(t, s, "permissions")
	if len(snapshot(t, r).Entries) != 5 || snapshot(t, r).Winner != nil {
		t.Fatal("duplicate or permission failure")
	}
	s.command(t, "draw-1", "owner", "!rafpick", false)
	s.command(t, "draw-1", "owner", "!rafpick", false)
	barrier(t, s, "draw")
	sent, _ := s.outputs()
	wins := 0
	for _, text := range sent {
		if text == "Congratulations Viewer1 you are the winner!" {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("duplicate draw delivery: %d", wins)
	}
	record("winner")
	s.mu.Lock()
	s.failedSend = true
	s.mu.Unlock()
	s.command(t, "failed-draw", "owner", "!rafpick", false)
	barrier(t, s, "send-failed")
	if snapshot(t, r).Winner.Announcement != "failed" {
		t.Fatal("failed announcement not preserved")
	}
	record("failed-announcement")
	s.command(t, "failed-draw", "owner", "!rafpick", false)
	barrier(t, s, "no-reroll")
	_, attempted := s.outputs()
	attempts := 0
	for _, text := range attempted {
		if strings.HasPrefix(text, "Congratulations ") {
			attempts++
		}
	}
	if attempts != 2 {
		t.Fatal("failed send caused reroll")
	}
	// Both Twitch-directed migration and an unexpected transport loss use real sockets.
	subscriptions, polls := s.counts()
	s.reconnect(t, true)
	eventually(t, "directed reconnect verification", func() bool { _, n := s.counts(); return n > polls })
	if n, _ := s.counts(); n != subscriptions {
		t.Fatal("migration recreated transferred subscriptions")
	}
	barrier(t, s, "migrated")
	s.reconnect(t, false)
	eventually(t, "fresh reconnect subscription recreation", func() bool { n, _ := s.counts(); return n > subscriptions })
	barrier(t, s, "fresh-reconnect")
	if len(snapshot(t, r).Entries) != 3 {
		t.Fatal("reconnect lost entrants")
	}
	record("reconnected")
	for _, mode := range []string{"malformed", "http-error", "timeout", "rate-limit"} {
		s.configure("broadcast-A", mode)
		provider := twitch.NewHelixClient("rehearsal-client", "rehearsal-token")
		if err := r.service.Reconcile(context.Background(), provider, "rehearsal", false); err == nil {
			t.Fatalf("fault %s accepted", mode)
		}
		if v := snapshot(t, r); v.Status != "ACTIVE" || len(v.Entries) != 3 {
			t.Fatalf("fault %s destroyed state", mode)
		}
	}
	record("degraded-verification")
	s.configure("broadcast-A", "")
	r.stop(t)
	r = nil
	// Restart using the exact same database, then verify the authoritative UI snapshot.
	r = startRuntime(t, path, clock)
	waitConnected(t, s)
	if len(snapshot(t, r).Entries) != 3 {
		t.Fatal("restart lost entries")
	}
	record("restarted")
	s.command(t, "winner-retry", "Viewer1", "!rafadd", false)
	s.command(t, "failed-announcement-retry", "Viewer2", "!rafadd", false)
	barrier(t, s, "blocked-winners")
	if len(snapshot(t, r).Entries) != 3 {
		t.Fatal("winner reentered after restart")
	}
	record("winners-blocked")
	s.command(t, "clear", "owner", "!rafclear", false)
	barrier(t, s, "clear")
	record("cleared")
	s.command(t, "1", "Viewer1", "!rafadd", false)
	barrier(t, s, "old-command")
	if len(snapshot(t, r).Entries) != 0 {
		t.Fatal("dedup lost across clear/restart")
	}
	s.command(t, "new-entry", "Viewer1", "!rafadd", false)
	barrier(t, s, "re-entry")
	if v := snapshot(t, r); len(v.Entries) != 1 || v.Entries[0].Number != 1 {
		t.Fatal("numbering did not reset")
	}
	record("re-entered")
	s.configure("", "")
	event(t, s, r, "stream.offline", "offline-A")
	record("end-pending")
	clock.advance(59 * time.Second)
	event(t, s, r, "stream.offline", "offline-check-59")
	if snapshot(t, r).Status != "END_PENDING" {
		t.Fatal("early cleanup")
	}
	r.stop(t)
	r = nil
	r = startRuntime(t, path, clock)
	waitConnected(t, s)
	if len(snapshot(t, r).Entries) != 1 {
		t.Fatal("restart lost pending entrants")
	}
	clock.advance(time.Second)
	event(t, s, r, "stream.offline", "offline-check-60")
	record("closed")
	if v := snapshot(t, r); v.Status != "INACTIVE" || len(v.Entries) != 0 || v.Winner != nil {
		t.Fatal("cleanup failed")
	}
	s.configure("broadcast-B", "")
	event(t, s, r, "stream.online", "online-B")
	record("new-broadcast")
	s.command(t, "B-entry", "ViewerB", "!rafadd", false)
	barrier(t, s, "B-entry")
	event(t, s, r, "stream.offline", "late-offline-A")
	if v := snapshot(t, r); v.StreamID != "broadcast-B" || len(v.Entries) != 1 || v.Status != "ACTIVE" {
		t.Fatal("stale event erased B")
	}
	record("stale-event-ignored")
	if output := os.Getenv("LUPUSARIA_REHEARSAL_TRACE"); output != "" {
		data, err := json.MarshalIndent(trace, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRaffleProcessTermination(t *testing.T) {
	if path := os.Getenv("LUPUSARIA_REHEARSAL_CRASH_DB"); path != "" {
		s := newSimulator(t)
		s.configure("broadcast-A", "")
		r := startRuntime(t, path, &testClock{at: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)})
		waitConnected(t, s)
		s.command(t, "crash-entry", "CrashViewer", "!rafadd", false)
		s.command(t, "crash-pick", "owner", "!rafpick", false)
		barrier(t, s, "crash-committed")
		if len(snapshot(t, r).Entries) != 0 || snapshot(t, r).Winner == nil {
			t.Fatal("child did not commit")
		}
		// Deliberately bypass all deferred cleanup, including WebSocket and DB close.
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "crash.sqlite")
	child := exec.Command(os.Args[0], "-test.run=^TestRaffleProcessTermination$", "-test.v")
	child.Env = append(os.Environ(), "LUPUSARIA_REHEARSAL_CRASH_DB="+path)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("rehearsal child failed: %v %s", err, out)
	}
	s := newSimulator(t)
	s.configure("broadcast-A", "")
	r := startRuntime(t, path, &testClock{at: time.Date(2026, 11, 13, 12, 1, 0, 0, time.UTC)})
	defer r.stop(t)
	waitConnected(t, s)
	v := snapshot(t, r)
	if v.Status != "ACTIVE" || len(v.Entries) != 0 || v.Winner == nil || v.Winner.Name != "CrashViewer" {
		t.Fatalf("crash recovery lost committed state: %+v", v)
	}
	s.command(t, "crash-winner-retry", "CrashViewer", "!rafadd", false)
	barrier(t, s, "crash-winner-blocked")
	if len(snapshot(t, r).Entries) != 0 {
		t.Fatal("crashed winner reentered")
	}
}
