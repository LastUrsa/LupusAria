package wrapup

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectionAggregationPersistenceAndDeduplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrapup.json")
	s, err := New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if err = s.Online("stream-1", "broadcaster", start); err != nil {
		t.Fatal(err)
	}
	events := []Event{
		{MessageID: "f1", Type: "follow", UserID: "u1", Name: "Alpha", At: start.Add(time.Minute)},
		{MessageID: "f2", Type: "follow", UserID: "u1", Name: "AlphaRenamed", At: start.Add(2 * time.Minute)},
		{MessageID: "s1", Type: "subscribe", UserID: "u2", Name: "Moon", At: start.Add(3 * time.Minute), Metadata: map[string]any{"tier": "1000"}},
		{MessageID: "r1", Type: "resub", UserID: "u3", Name: "Silver", At: start.Add(4 * time.Minute), Metadata: map[string]any{"months": 6}},
		{MessageID: "g1", Type: "gift", UserID: "g1", Name: "Wolf", At: start.Add(5 * time.Minute), Metadata: map[string]any{"total": 5}},
		// Recipient notifications overlap the community gift and are intentionally ignored.
		{MessageID: "recipient", Type: "subscribe", UserID: "recipient", Name: "Recipient", At: start.Add(6 * time.Minute), Metadata: map[string]any{"isGift": true}},
		{MessageID: "b1", Type: "cheer", UserID: "u4", Name: "Crimson", At: start.Add(7 * time.Minute), Metadata: map[string]any{"bits": 500}},
		{MessageID: "b2", Type: "cheer", UserID: "u4", Name: "Crimson", At: start.Add(8 * time.Minute), Metadata: map[string]any{"bits": 1000}},
		{MessageID: "ba", Type: "cheer", Name: "Anonymous", At: start.Add(9 * time.Minute), Metadata: map[string]any{"bits": 100}},
		{MessageID: "raid", Type: "raid", UserID: "raider", Name: "Cosmic", At: start.Add(10 * time.Minute), Metadata: map[string]any{"viewers": 42}},
	}
	for _, e := range events {
		if err = s.Record(e); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Record(events[0]); err != nil {
		t.Fatal(err)
	}
	r, ok := s.Current()
	if !ok {
		t.Fatal("missing report")
	}
	if r.FollowerCount != 1 || r.SubscriptionCount != 2 || r.GiftedSubCount != 5 || r.Bits != 1600 || r.RaidCount != 1 {
		t.Fatalf("wrong totals: %#v", r)
	}
	for _, want := range []string{"**New Followers**", "**Subscriptions**", "**Bits**", "**Raids**", "Crimson — 1,500 Bits", "Silver — 6-Month Resubscription"} {
		if !strings.Contains(r.Formatted, want) {
			t.Errorf("missing %q in %s", want, r.Formatted)
		}
	}
	reloaded, err := New(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	rr, _ := reloaded.Current()
	if rr.Stats != r.Stats {
		t.Fatalf("reload changed report: %q != %q", rr.Stats, r.Stats)
	}
}

func TestSessionLifecycleResumeLateEventAndResetIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrapup.json")
	s, _ := New(path, nil)
	now := time.Now().UTC()
	if err := s.Online("one", "b", now); err != nil {
		t.Fatal(err)
	}
	_ = s.Record(Event{MessageID: "one-follow", Type: "follow", UserID: "u1", Name: "One", At: now})
	if err := s.Online("one", "b", now); err != nil {
		t.Fatal(err)
	} // reconnect, not a new session
	if err := s.Offline(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = s.Record(Event{MessageID: "late", Type: "cheer", UserID: "u", Name: "Late", At: now.Add(time.Hour), Metadata: map[string]any{"bits": 10}})
	completed, _ := s.Current()
	if completed.FollowerCount != 1 || completed.Bits != 10 || completed.Status != "complete" {
		t.Fatalf("late attribution failed: %#v", completed)
	}
	if err := s.Online("two", "b", now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, _ := s.Current()
	if second.FollowerCount != 0 || second.Bits != 0 {
		t.Fatalf("new stream inherited events: %#v", second)
	}
	_ = s.Record(Event{MessageID: "two-raid", Type: "raid", Name: "Two", At: now, Metadata: map[string]any{"viewers": 2}})
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	second, _ = s.Current()
	if second.RaidCount != 0 {
		t.Fatal("reset did not clear active session")
	}
	if len(s.data.Sessions) != 2 || len(s.data.Sessions[0].Events) != 2 {
		t.Fatal("reset altered completed session")
	}
}

func TestEmptyReport(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "w.json"), nil)
	_ = s.Online("s", "b", time.Now())
	r, _ := s.Current()
	if !strings.Contains(r.Formatted, "No follows, subscriptions, Bits, or incoming raids") {
		t.Fatal(r.Formatted)
	}
}

func TestEventsOutsideLateDeliveryWindowAreNotAttributed(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "w.json"), nil)
	start := time.Now().UTC().Add(-time.Hour)
	_ = s.Online("s", "b", start)
	end := start.Add(30 * time.Minute)
	_ = s.Offline(end)
	_ = s.Record(Event{MessageID: "too-late", Type: "cheer", UserID: "u", Name: "Late", At: end.Add(lateEventWindow + time.Second), Metadata: map[string]any{"bits": 100}})
	r, _ := s.Current()
	if r.Bits != 0 {
		t.Fatalf("offline event was attributed after late window: %#v", r)
	}
}

func TestDistinctOnlineEventFinalizesStaleLiveSession(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "w.json"), nil)
	start := time.Now().UTC()
	_ = s.Online("one", "b", start)
	_ = s.Online("two", "b", start.Add(time.Hour))
	if len(s.data.Sessions) != 2 || s.data.Sessions[0].Status != "complete" || s.data.Sessions[1].Status != "live" {
		t.Fatalf("unexpected rollover: %#v", s.data.Sessions)
	}
}

func TestReportOrderingAndDisconnectWarning(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "w.json"), nil)
	start := time.Now().UTC()
	_ = s.Online("s", "b", start)
	for _, event := range []Event{
		{MessageID: "f2", Type: "follow", UserID: "2", Name: "Second", At: start.Add(2 * time.Minute)},
		{MessageID: "f1", Type: "follow", UserID: "1", Name: "First", At: start.Add(time.Minute)},
		{MessageID: "b1", Type: "cheer", UserID: "1", Name: "Small", At: start, Metadata: map[string]any{"bits": 10}},
		{MessageID: "b2", Type: "cheer", UserID: "2", Name: "Large", At: start, Metadata: map[string]any{"bits": 20}},
		{MessageID: "gap", Type: "gap", At: start},
	} {
		_ = s.Record(event)
	}
	r, _ := s.Current()
	if strings.Index(r.Formatted, "First") > strings.Index(r.Formatted, "Second") || strings.Index(r.Formatted, "Large") > strings.Index(r.Formatted, "Small") {
		t.Fatalf("report ordering is wrong: %s", r.Formatted)
	}
	if r.CompletenessWarning == "" {
		t.Fatal("disconnect warning missing")
	}
}
