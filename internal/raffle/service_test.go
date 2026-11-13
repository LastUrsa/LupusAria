package raffle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"lupusaria/internal/twitch"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

type provider struct {
	info twitch.StreamInfo
	err  error
}

func (p provider) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return p.info, p.err
}
func live(id string) provider {
	return provider{info: twitch.StreamInfo{Live: true, ID: id, Channel: "channel"}}
}

type fixture struct {
	s    *Service
	r    *Repository
	now  time.Time
	path string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC), path: filepath.Join(t.TempDir(), "raffle.sqlite")}
	var err error
	f.r, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.r.Close() })
	f.s, err = New(f.r, "b", Options{Now: func() time.Time { return f.now }, Random: func(n int) (int, error) { return n, nil }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) reconcile(t *testing.T, p provider, start bool) {
	t.Helper()
	if err := f.s.Reconcile(context.Background(), p, "channel", start); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) snapshot(t *testing.T) Snapshot {
	t.Helper()
	v, err := f.s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *fixture) request(command, id, user string) Request {
	v, _ := f.s.Capture()
	return Request{Command: command, MessageID: id, UserID: user, Login: user, Name: user, Fence: v}
}
func (f *fixture) execute(t *testing.T, r Request) []string {
	t.Helper()
	var out []string
	err := f.s.Execute(context.Background(), r, func(v string) error { out = append(out, v); return nil })
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func (f *fixture) add(t *testing.T, id string) { f.execute(t, f.request("!rafadd", "add-"+id, id)) }
func count(t *testing.T, r *Repository, table string) int {
	t.Helper()
	var n int
	if err := r.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func assertEntries(t *testing.T, f *fixture, n int, status string) {
	t.Helper()
	v := f.snapshot(t)
	if len(v.Entries) != n || v.Status != status {
		t.Fatalf("entries=%d status=%s, want %d %s", len(v.Entries), v.Status, n, status)
	}
	for i, e := range v.Entries {
		if e.Number != i+1 {
			t.Fatal("noncontinuous numbering")
		}
	}
}

func TestCommandsNumberingDrawsClearAndDedup(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	r := f.request("!rafadd", "first", "ViewerA")
	out := f.execute(t, r)
	if len(out) != 1 || out[0] != "ViewerA added to raffle" {
		t.Fatal(out)
	}
	r.MessageID = "duplicate-user"
	r.Name = "Renamed"
	if out = f.execute(t, r); len(out) != 0 {
		t.Fatal(out)
	}
	for i := 2; i <= 5; i++ {
		f.add(t, fmt.Sprint(i))
	}
	assertEntries(t, f, 5, "ACTIVE")
	for _, user := range []string{"viewer", "moderator"} {
		for _, cmd := range []string{"!rafpick", "!rafclear"} {
			out = f.execute(t, f.request(cmd, cmd+user, user))
			if len(out) != 1 || out[0] != "Only the broadcaster can use "+cmd+"." {
				t.Fatal(out)
			}
		}
	}
	pick := f.request("!rafpick", "pick-1", "b")
	out = f.execute(t, pick)
	if len(out) != 1 || out[0] != "Congratulations 5 you are the winner!" {
		t.Fatal(out)
	}
	if out = f.execute(t, pick); len(out) != 0 {
		t.Fatal(out)
	}
	f.execute(t, f.request("!rafpick", "pick-2", "b"))
	if count(t, f.r, "draws") != 2 {
		t.Fatal("independent draws missing")
	}
	stale := f.request("!rafadd", "delayed", "late")
	stalePick := f.request("!rafpick", "delayed-pick", "b")
	out = f.execute(t, f.request("!rafclear", "clear", "b"))
	if len(out) != 1 || out[0] != "Raffle cleared." {
		t.Fatal(out)
	}
	if count(t, f.r, "entries") != 0 || count(t, f.r, "draws") != 0 || f.snapshot(t).Winner != nil {
		t.Fatal("clear retained identity")
	}
	for _, r = range []Request{stale, stalePick} {
		if err := f.s.Execute(context.Background(), r, func(string) error { return nil }); !errors.Is(err, ErrStale) {
			t.Fatalf("stale operation: %v", err)
		}
	}
	replay := f.request("!rafadd", "first", "ViewerA")
	if out = f.execute(t, replay); len(out) != 0 {
		t.Fatal("duplicate survived clear")
	}
	f.add(t, "ViewerA")
	assertEntries(t, f, 1, "ACTIVE")
	f.execute(t, f.request("!rafclear", "clear-2", "b"))
	out = f.execute(t, f.request("!rafpick", "empty", "b"))
	if len(out) != 1 || out[0] != "Raffle is empty. No winner can be selected." {
		t.Fatal(out)
	}
}
func TestEnrollmentAndDrawFailuresPreserveCommittedState(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	sendErr := errors.New("chat unavailable")
	r := f.request("!rafadd", "a", "ViewerA")
	if err := f.s.Execute(context.Background(), r, func(string) error { return sendErr }); err != sendErr {
		t.Fatal(err)
	}
	assertEntries(t, f, 1, "ACTIVE")
	if out := f.execute(t, r); len(out) != 0 {
		t.Fatal(out)
	}
	pick := f.request("!rafpick", "p", "b")
	if err := f.s.Execute(context.Background(), pick, func(string) error { return sendErr }); err != sendErr {
		t.Fatal(err)
	}
	if f.snapshot(t).Winner.Announcement != "failed" || count(t, f.r, "draws") != 1 {
		t.Fatal("failed draw lost")
	}
	f.execute(t, pick)
	f.s.random = func(int) (int, error) { return 0, errors.New("rng failed") }
	if err := f.s.Execute(context.Background(), f.request("!rafpick", "rng", "b"), func(string) error { t.Fatal("announced failed RNG"); return nil }); err == nil {
		t.Fatal("expected failure")
	}
	if count(t, f.r, "draws") != 1 {
		t.Fatal("draw persisted despite RNG error")
	}
	_, err := f.r.db.Exec(`CREATE TRIGGER fail_entry BEFORE INSERT ON entries BEGIN SELECT RAISE(ABORT,'injected'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.Execute(context.Background(), f.request("!rafadd", "db-fail", "x"), func(string) error { t.Fatal("ack before commit"); return nil }); err == nil {
		t.Fatal("expected failure")
	}
	f.r.db.Exec(`DROP TRIGGER fail_entry`)
	f.add(t, "x")
	assertEntries(t, f, 2, "ACTIVE")
}
func TestLifecycleGraceRecoveryReplacementAndTerminalSessions(t *testing.T) {
	f := setup(t)
	f.reconcile(t, provider{}, true)
	assertEntries(t, f, 0, "INACTIVE")
	f.execute(t, f.request("!rafadd", "offline", "v"))
	if count(t, f.r, "sessions") != 0 {
		t.Fatal("offline created session")
	}
	f.reconcile(t, live("A"), false)
	f.add(t, "one")
	f.reconcile(t, live("A"), false)
	assertEntries(t, f, 1, "ACTIVE")
	f.reconcile(t, provider{}, false)
	assertEntries(t, f, 1, "END_PENDING")
	f.execute(t, f.request("!rafadd", "suspended", "two"))
	f.execute(t, f.request("!rafpick", "suspended-pick", "b"))
	assertEntries(t, f, 1, "END_PENDING")
	if count(t, f.r, "draws") != 0 {
		t.Fatal("pending draw accepted")
	}
	f.now = f.now.Add(59 * time.Second)
	f.reconcile(t, provider{}, false)
	assertEntries(t, f, 1, "END_PENDING")
	// Reload with a new service as on application restart; timestamps survive.
	var err error
	f.s, err = New(f.r, "b", Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	f.reconcile(t, provider{}, true)
	assertEntries(t, f, 0, "INACTIVE")
	if count(t, f.r, "commands") != 0 || count(t, f.r, "draws") != 0 {
		t.Fatal("cleanup retained round state")
	}
	f.reconcile(t, provider{}, false)
	f.reconcile(t, live("A"), false)
	assertEntries(t, f, 0, "INACTIVE")
	f.reconcile(t, live("B"), false)
	f.add(t, "new")
	f.reconcile(t, live("C"), false)
	assertEntries(t, f, 0, "ACTIVE")
	if f.snapshot(t).StreamID != "C" {
		t.Fatal("wrong stream")
	}
	f.add(t, "C-user")
	f.reconcile(t, live("A"), false)
	assertEntries(t, f, 1, "ACTIVE") // Closed A must not erase C.
}
func TestGraceCancellationFailuresAndManualClearWhileRecovering(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	f.add(t, "v")
	err := f.s.Reconcile(context.Background(), provider{err: errors.New("timeout")}, "channel", false)
	if err == nil {
		t.Fatal("expected timeout")
	}
	assertEntries(t, f, 1, "ACTIVE")
	f.add(t, "still-active")
	f.reconcile(t, provider{}, false)
	f.now = f.now.Add(2 * time.Hour)
	f.s.Reconcile(context.Background(), provider{err: errors.New("timeout")}, "channel", false)
	assertEntries(t, f, 2, "END_PENDING")
	f.reconcile(t, live("A"), false)
	assertEntries(t, f, 2, "ACTIVE")
	f.reconcile(t, provider{}, false)
	f.now = f.now.Add(59 * time.Second)
	f.reconcile(t, provider{}, false)
	assertEntries(t, f, 2, "END_PENDING")
	f.execute(t, f.request("!rafclear", "clear-pending", "b"))
	assertEntries(t, f, 0, "END_PENDING")
	f.reconcile(t, live("A"), false)
	f.add(t, "again")
	f.s, _ = New(f.r, "b", Options{Now: func() time.Time { return f.now }})
	f.s.Reconcile(context.Background(), provider{err: errors.New("auth failed")}, "channel", true)
	assertEntries(t, f, 1, "RECOVERING")
	f.execute(t, f.request("!rafadd", "no-entry", "blocked"))
	assertEntries(t, f, 1, "RECOVERING")
	f.execute(t, f.request("!rafclear", "clear-recovering", "b"))
	assertEntries(t, f, 0, "RECOVERING")
}
func TestRestartAfterMissedStreamEndOrNewStream(t *testing.T) {
	for _, next := range []string{"offline", "B", "A"} {
		t.Run(next, func(t *testing.T) {
			f := setup(t)
			f.reconcile(t, live("A"), true)
			f.add(t, "v")
			f.r.Close()
			var err error
			f.r, err = Open(f.path)
			if err != nil {
				t.Fatal(err)
			}
			f.s, err = New(f.r, "b", Options{Now: func() time.Time { return f.now }})
			if err != nil {
				t.Fatal(err)
			}
			if next == "offline" {
				f.reconcile(t, provider{}, true)
				assertEntries(t, f, 1, "END_PENDING")
				f.now = f.now.Add(time.Minute)
				f.reconcile(t, provider{}, false)
				assertEntries(t, f, 0, "INACTIVE")
			} else {
				f.reconcile(t, live(next), true)
				expected := 0
				if next == "A" {
					expected = 1
				}
				assertEntries(t, f, expected, "ACTIVE")
			}
		})
	}
}
func TestCleanupAndReplacementRollback(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			f := setup(t)
			f.reconcile(t, live("A"), true)
			f.add(t, "v")
			f.execute(t, f.request("!rafpick", "p", "b"))
			f.reconcile(t, provider{}, false)
			f.now = f.now.Add(time.Minute)
			trigger := `CREATE TRIGGER fail_cleanup BEFORE DELETE ON entries BEGIN SELECT RAISE(ABORT,'injected'); END;`
			if replacement {
				trigger = `CREATE TRIGGER fail_creation BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT,'injected'); END;`
			}
			if _, err := f.r.db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			next := provider{}
			if replacement {
				next = live("B")
			}
			if err := f.s.Reconcile(context.Background(), next, "channel", false); err == nil {
				t.Fatal("expected rollback")
			}
			assertEntries(t, f, 1, "END_PENDING")
			if count(t, f.r, "draws") != 1 {
				t.Fatal("partial cleanup")
			}
			f.r.db.Exec(`DROP TRIGGER IF EXISTS fail_cleanup; DROP TRIGGER IF EXISTS fail_creation`)
			f.reconcile(t, next, false)
			expected := "INACTIVE"
			if replacement {
				expected = "ACTIVE"
			}
			assertEntries(t, f, 0, expected)
		})
	}
}
func TestConcurrentRegistrationsDrawsAndConstraints(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for i := 0; i < 100; i++ {
		for duplicate := 0; duplicate < 2; duplicate++ {
			wg.Add(1)
			go func(i, d int) {
				defer wg.Done()
				r := f.request("!rafadd", fmt.Sprintf("%d-%d", i, d), fmt.Sprint(i))
				errs <- f.s.Execute(context.Background(), r, func(string) error { return nil })
			}(i, duplicate)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertEntries(t, f, 100, "ACTIVE")
	for i := 100; i < 1000; i++ {
		f.add(t, fmt.Sprint(i))
	}
	assertEntries(t, f, 1000, "ACTIVE")
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := f.s.Execute(context.Background(), f.request("!rafpick", fmt.Sprint(i), "b"), func(string) error { return nil })
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if count(t, f.r, "draws") != 10 {
		t.Fatal("draw race")
	}
	for _, q := range []string{
		`INSERT INTO entries VALUES(1,1,'dup','x','x',1,0)`,
		`INSERT INTO entries VALUES(1,1,'negative','x','x',0,0)`,
		`INSERT INTO entries VALUES(999,1,'orphan','x','x',1,0)`,
		`INSERT INTO sessions(broadcaster,stream,state,created) VALUES('b','other','ACTIVE',0)`,
		`INSERT INTO sessions(broadcaster,stream,state,created) VALUES('b','A','CLOSED',0)`,
	} {
		if _, err := f.r.db.Exec(q); err == nil {
			t.Fatalf("constraint accepted %s", q)
		}
	}
	var check string
	if err := f.r.db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("integrity %s %v", check, err)
	}
}
func TestDrawBoundariesAndIdentityMapping(t *testing.T) {
	for _, n := range []int{1, 2, 100} {
		for _, boundary := range []string{"low", "high"} {
			t.Run(fmt.Sprintf("%d-%s", n, boundary), func(t *testing.T) {
				f := setup(t)
				f.reconcile(t, live("A"), true)
				for i := 1; i <= n; i++ {
					f.add(t, fmt.Sprint(i))
				}
				want := 1
				if boundary == "high" {
					want = n
				}
				f.s.random = func(int) (int, error) { return want, nil }
				f.execute(t, f.request("!rafpick", "pick", "b"))
				w := f.snapshot(t).Winner
				if w == nil || w.Number != want || w.Name != fmt.Sprint(want) {
					t.Fatalf("wrong mapping: %+v", w)
				}
			})
		}
	}
}
func TestSecureRandomDiagnostic(t *testing.T) {
	if _, err := SecureNumber(0); err == nil {
		t.Fatal("empty RNG range")
	}
	var frequencies [10]int
	for i := 0; i < 100000; i++ {
		v, err := SecureNumber(10)
		if err != nil {
			t.Fatal(err)
		}
		if v < 1 || v > 10 {
			t.Fatal(v)
		}
		frequencies[v-1]++
	}
	chi := 0.0
	for _, n := range frequencies {
		d := float64(n) - 10000
		chi += d * d / 10000
	}
	// df=9, 0.001 upper-tail threshold=27.877. Diagnostic only: a random
	// sample can exceed any fixed threshold; cryptographic API and mapping are tested separately.
	t.Logf("100000 crypto/rand.Int draws: frequencies=%v chi-square=%.4f df=9 threshold(0.001)=27.877 exceeds=%v", frequencies, chi, chi > 27.877)
}
func TestEventDedupAndBroadcasterScope(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		v    Trigger
		want bool
	}{{Trigger{ID: "1", Broadcaster: "other"}, false}, {Trigger{ID: "1", Broadcaster: "b"}, true}, {Trigger{ID: "1", Broadcaster: "b"}, false}, {Trigger{}, true}} {
		ok, err := f.s.acceptTrigger(tc.v)
		if err != nil || ok != tc.want {
			t.Fatalf("%+v: %v %v", tc, ok, err)
		}
	}
	f.s, _ = New(f.r, "b", Options{})
	if ok, _ := f.s.acceptTrigger(Trigger{ID: "1", Broadcaster: "b"}); ok {
		t.Fatal("dedup lost on restart")
	}
}
func TestCrashRecovery(t *testing.T) {
	if path := os.Getenv("RAFFLE_CRASH_TEST_PATH"); path != "" {
		r, err := Open(path)
		if err != nil {
			os.Exit(2)
		}
		s, err := New(r, "b", Options{})
		if err != nil {
			os.Exit(3)
		}
		if err = s.Reconcile(context.Background(), live("A"), "channel", true); err != nil {
			os.Exit(4)
		}
		fence, _ := s.Capture()
		if err = s.Execute(context.Background(), Request{Command: "!rafadd", MessageID: "committed", UserID: "u", Name: "u", Fence: fence}, func(string) error { return nil }); err != nil {
			os.Exit(5)
		}
		tx, err := r.db.Begin()
		if err != nil {
			os.Exit(6)
		}
		if _, err = tx.Exec(`DELETE FROM entries`); err != nil {
			os.Exit(7)
		}
		// Exit with an uncommitted destructive transaction and without closing the DB.
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "crash.sqlite")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecovery$")
	cmd.Env = append(os.Environ(), "RAFFLE_CRASH_TEST_PATH="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, out)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if count(t, r, "entries") != 1 {
		t.Fatal("committed entry lost or uncommitted cleanup applied")
	}
}

type controlledTimer struct {
	ticks  chan time.Time
	resets chan time.Duration
}

func (t *controlledTimer) C() <-chan time.Time        { return t.ticks }
func (t *controlledTimer) Stop() bool                 { return true }
func (t *controlledTimer) Reset(d time.Duration) bool { t.resets <- d; return true }
func nextDuration(t *testing.T, timer *controlledTimer) time.Duration {
	t.Helper()
	select {
	case d := <-timer.resets:
		return d
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler failed to advance")
		return 0
	}
}
func TestControlledRetryScheduleAndShutdown(t *testing.T) {
	f := setup(t)
	timer := &controlledTimer{ticks: make(chan time.Time), resets: make(chan time.Duration, 10)}
	f.s.newTimer = func(d time.Duration) Timer { timer.resets <- d; return timer }
	failed := provider{err: errors.New("network unavailable")}
	_ = f.s.Reconcile(context.Background(), failed, "channel", true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.s.Run(ctx, failed, "channel", nil, time.Minute, func(error) {}) }()
	for _, want := range []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, time.Minute} {
		if got := nextDuration(t, timer); got != want {
			t.Fatalf("delay=%v want %v", got, want)
		}
		f.now = f.now.Add(want)
		timer.ticks <- f.now
	}
	_ = nextDuration(t, timer)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown stuck")
	}
}
func TestControlledSchedulerHonorsRateLimitBeforeEventTriggers(t *testing.T) {
	f := setup(t)
	timer := &controlledTimer{ticks: make(chan time.Time), resets: make(chan time.Duration, 10)}
	f.s.newTimer = func(d time.Duration) Timer { timer.resets <- d; return timer }
	failed := provider{err: &twitch.HelixError{Message: "429", RetryAt: f.now.Add(2 * time.Minute)}}
	_ = f.s.Reconcile(context.Background(), failed, "channel", true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals := make(chan Trigger)
	done := make(chan struct{})
	go func() { defer close(done); f.s.Run(ctx, failed, "channel", signals, time.Minute, func(error) {}) }()
	if got := nextDuration(t, timer); got != 2*time.Minute {
		t.Fatal(got)
	}
	signals <- Trigger{}
	if got := nextDuration(t, timer); got != 2*time.Minute {
		t.Fatal("event bypassed server retry guidance", got)
	}
	cancel()
	<-done
}
func TestClearAndCloseFencePendingRegistrationBatches(t *testing.T) {
	for _, closeIt := range []bool{false, true} {
		t.Run(fmt.Sprint(closeIt), func(t *testing.T) {
			f := setup(t)
			f.reconcile(t, live("A"), true)
			f.add(t, "initial")
			requests := make([]Request, 100)
			for i := range requests {
				requests[i] = f.request("!rafadd", fmt.Sprint(i), fmt.Sprint(i))
			}
			if closeIt {
				f.reconcile(t, live("B"), false)
			} else {
				f.execute(t, f.request("!rafclear", "clear", "b"))
			}
			var wg sync.WaitGroup
			for _, r := range requests {
				wg.Add(1)
				go func(r Request) {
					defer wg.Done()
					if err := f.s.Execute(context.Background(), r, func(string) error { t.Error("stale registration acknowledged"); return nil }); !errors.Is(err, ErrStale) {
						t.Errorf("stale registration: %v", err)
					}
				}(r)
			}
			wg.Wait()
			assertEntries(t, f, 0, "ACTIVE")
		})
	}
}
func TestDatabaseContentionPreservesNumbering(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	other, err := Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = f.r.db.Exec(`PRAGMA busy_timeout=1`); err != nil {
		t.Fatal(err)
	}
	tx, err := other.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE revision SET value=value+1`); err != nil {
		t.Fatal(err)
	}
	err = f.s.Execute(context.Background(), f.request("!rafadd", "blocked-write", "v"), func(string) error { t.Fatal("uncommitted acknowledgment"); return nil })
	if err == nil {
		t.Fatal("contention should fail safely")
	}
	tx.Rollback()
	f.add(t, "v")
	assertEntries(t, f, 1, "ACTIVE")
}
func TestAnnouncementDispatchAndClearHaveConsistentOrder(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	f.add(t, "v")
	sending := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	pick := f.request("!rafpick", "pick", "b")
	go func() {
		done <- f.s.Execute(context.Background(), pick, func(string) error { close(sending); <-release; return nil })
	}()
	<-sending
	// Capture is lock-free even while a send is in progress, retaining the old round.
	stale := f.request("!rafadd", "queued", "late")
	clear := f.request("!rafclear", "clear", "b")
	cleared := make(chan error, 1)
	go func() { cleared <- f.s.Execute(context.Background(), clear, func(string) error { return nil }) }()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-cleared; err != nil {
		t.Fatal(err)
	}
	if err := f.s.Execute(context.Background(), stale, func(string) error { t.Fatal("stale ack"); return nil }); !errors.Is(err, ErrStale) {
		t.Fatal(err)
	}
	assertEntries(t, f, 0, "ACTIVE")
	if f.snapshot(t).Winner != nil {
		t.Fatal("stale winner")
	}
}

type orderedProvider struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (p *orderedProvider) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	p.calls++
	if p.calls == 1 {
		close(p.started)
		<-p.release
		return live("A").info, nil
	}
	return live("B").info, nil
}
func TestVerificationRequestsAreSerialized(t *testing.T) {
	f := setup(t)
	p := &orderedProvider{started: make(chan struct{}), release: make(chan struct{})}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- f.s.Reconcile(context.Background(), p, "channel", true) }()
	<-p.started
	go func() { second <- f.s.Reconcile(context.Background(), p, "channel", false) }()
	close(p.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if f.snapshot(t).StreamID != "B" || p.calls != 2 {
		t.Fatal("old response replaced newer stream")
	}
}
func TestDrawingDuringRegistrationUsesCommittedSnapshot(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	f.add(t, "initial")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := f.request("!rafadd", fmt.Sprint(i), fmt.Sprint(i))
			if err := f.s.Execute(context.Background(), r, func(string) error { return nil }); err != nil {
				t.Error(err)
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := f.s.Execute(context.Background(), f.request("!rafpick", fmt.Sprintf("pick-%d", i), "b"), func(string) error { return nil }); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	assertEntries(t, f, 101, "ACTIVE")
	rows, err := f.r.db.Query(`SELECT number,count FROM draws`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var number, n int
		if err = rows.Scan(&number, &n); err != nil {
			t.Fatal(err)
		}
		if number != n || n < 1 || n > 101 {
			t.Fatalf("inconsistent snapshot: number=%d count=%d", number, n)
		}
	}
}
func TestSecureDeletionRemovesParticipantPayloadsFromLiveDatabase(t *testing.T) {
	for _, closeIt := range []bool{false, true} {
		t.Run(fmt.Sprint(closeIt), func(t *testing.T) {
			f := setup(t)
			f.reconcile(t, live("A"), true)
			marker := "RafflePrivateIdentity_82691_DeletionCheck"
			r := f.request("!rafadd", "opaque-twitch-message-id", "opaque-user-id")
			r.Name = marker
			r.Login = marker
			f.execute(t, r)
			f.execute(t, f.request("!rafpick", "opaque-pick-id", "b"))
			if closeIt {
				f.reconcile(t, live("B"), false)
			} else {
				f.execute(t, f.request("!rafclear", "opaque-clear-id", "b"))
			}
			data, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte(marker)) {
				t.Fatal("deleted identity remains in live SQLite pages")
			}
			if _, err = os.Stat(f.path + "-journal"); !os.IsNotExist(err) {
				t.Fatalf("persistent journal: %v", err)
			}
		})
	}
}

func TestReplacementConnectionsRetainDatabaseSafetySettings(t *testing.T) {
	name := "raffle # unicode ✨.sqlite"
	if runtime.GOOS != "windows" {
		name = "raffle # ? unicode ✨.sqlite"
	}
	path := filepath.Join(t.TempDir(), name)
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.db.SetMaxIdleConns(0)
	for pragma, want := range map[string]int{"foreign_keys": 1, "secure_delete": 1, "synchronous": 2, "busy_timeout": 5000} {
		var got int
		if err = r.db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d want %d: %v", pragma, got, want, err)
		}
	}
}

func TestFutureSchemaIsRejectedWithoutOverwritingParticipants(t *testing.T) {
	f := setup(t)
	f.reconcile(t, live("A"), true)
	f.add(t, "saved")
	if _, err := f.r.db.Exec(`UPDATE schema_version SET version=2`); err != nil {
		t.Fatal(err)
	}
	if r, err := Open(f.path); err == nil {
		r.Close()
		t.Fatal("unsupported schema accepted")
	}
	if count(t, f.r, "entries") != 1 {
		t.Fatal("unsupported schema destroyed saved data")
	}
}
