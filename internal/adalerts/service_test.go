package adalerts

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

type fakeChat struct {
	sent []string
}

func (f *fakeChat) Say(_ string, text string) error {
	f.sent = append(f.sent, text)
	return nil
}

type fakeComposer struct {
	events []Event
	text   string
	err    error
}

func (f *fakeComposer) ComposeAdAlert(_ context.Context, event Event) (string, error) {
	f.events = append(f.events, event)
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

func TestHandleScheduleWarnsStartsAndEndsOnce(t *testing.T) {
	chat := &fakeChat{}
	service := New(Config{
		Channel:      "lastursa",
		Enabled:      true,
		WarningLead:  5 * time.Minute,
		PollInterval: time.Minute,
	}, chat, nil, nil)

	start := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return start.Add(-4 * time.Minute) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})

	service.now = func() time.Time { return start.Add(10 * time.Second) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})

	service.now = func() time.Time { return start.Add(91 * time.Second) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})

	want := []string{
		"Heads up: ads are scheduled in about 4 minutes.",
		"Ad break starting now. Good moment to stretch, hydrate, and rest your eyes.",
		"Welcome back. Ads should be done now.",
	}
	if !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func TestHandleScheduleUsesComposerWhenAvailable(t *testing.T) {
	chat := &fakeChat{}
	composer := &fakeComposer{text: "Composed in character."}
	service := New(Config{
		Channel:      "lastursa",
		Enabled:      true,
		WarningLead:  5 * time.Minute,
		PollInterval: time.Minute,
		Composer:     composer,
	}, chat, nil, nil)

	start := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return start.Add(-4 * time.Minute) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})

	if want := []string{"Composed in character."}; !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
	if len(composer.events) != 1 {
		t.Fatalf("composer events = %#v", composer.events)
	}
	if composer.events[0].Kind != EventWarning {
		t.Fatalf("event kind = %q, want %q", composer.events[0].Kind, EventWarning)
	}
	if composer.events[0].Lead != 4*time.Minute {
		t.Fatalf("lead = %s, want 4m", composer.events[0].Lead)
	}
}

func TestHandleScheduleFallsBackWhenComposerFails(t *testing.T) {
	chat := &fakeChat{}
	composer := &fakeComposer{err: errors.New("ai down")}
	service := New(Config{
		Channel:        "lastursa",
		Enabled:        true,
		WarningLead:    5 * time.Minute,
		PollInterval:   time.Minute,
		WarningMessage: "Fallback in %s.",
		Composer:       composer,
	}, chat, nil, nil)

	start := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return start.Add(-4 * time.Minute) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})

	want := []string{"Fallback in 4 minutes."}
	if !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func TestHandleScheduleFallsBackWhenComposerReturnsEmpty(t *testing.T) {
	chat := &fakeChat{}
	composer := &fakeComposer{text: ""}
	service := New(Config{
		Channel:        "lastursa",
		Enabled:        true,
		WarningLead:    5 * time.Minute,
		PollInterval:   time.Minute,
		WarningMessage: "Fallback in %s.",
		Composer:       composer,
	}, chat, nil, nil)

	start := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return start.Add(-4 * time.Minute) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: 90 * time.Second})

	want := []string{"Fallback in 4 minutes."}
	if !slices.Equal(chat.sent, want) {
		t.Fatalf("sent = %#v, want %#v", chat.sent, want)
	}
}

func TestHandleScheduleDoesNotWarnOutsideLeadWindow(t *testing.T) {
	chat := &fakeChat{}
	service := New(Config{
		Channel:     "lastursa",
		Enabled:     true,
		WarningLead: 5 * time.Minute,
	}, chat, nil, nil)

	start := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return start.Add(-6 * time.Minute) }
	service.HandleSchedule(Schedule{NextAdAt: start, Duration: time.Minute})

	if len(chat.sent) != 0 {
		t.Fatalf("expected no messages, got %#v", chat.sent)
	}
}

func TestFormatLeadUsesMinutesOnly(t *testing.T) {
	tests := map[time.Duration]string{
		10 * time.Second:               "1 minute",
		90 * time.Second:               "2 minutes",
		4*time.Minute + 10*time.Second: "4 minutes",
	}
	for duration, want := range tests {
		if got := formatLead(duration); got != want {
			t.Fatalf("formatLead(%s) = %q, want %q", duration, got, want)
		}
	}
}
