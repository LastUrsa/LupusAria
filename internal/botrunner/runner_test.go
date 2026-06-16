package botrunner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"lupusaria/internal/twitch"
)

type fakeUserResolver struct {
	users []twitch.UserInfo
	err   error
}

func (f fakeUserResolver) GetUsersByLogin(context.Context, []string) ([]twitch.UserInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.users, nil
}

func TestResolveRecentStreamerIDsFindsBroadcasterAndBot(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver := fakeUserResolver{users: []twitch.UserInfo{
		{ID: "broadcaster-id", Login: "LastUrsa"},
		{ID: "bot-id", Login: "LupusAria"},
	}}

	broadcasterID, moderatorID := resolveRecentStreamerIDs(context.Background(), resolver, "lastursa", "lupusaria", logger)

	if broadcasterID != "broadcaster-id" {
		t.Fatalf("broadcasterID = %q", broadcasterID)
	}
	if moderatorID != "bot-id" {
		t.Fatalf("moderatorID = %q", moderatorID)
	}
}

func TestResolveRecentStreamerIDsReturnsEmptyOnLookupError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	resolver := fakeUserResolver{err: errors.New("helix down")}

	broadcasterID, moderatorID := resolveRecentStreamerIDs(context.Background(), resolver, "lastursa", "lupusaria", logger)

	if broadcasterID != "" || moderatorID != "" {
		t.Fatalf("ids = %q/%q, want empty", broadcasterID, moderatorID)
	}
}

func TestConvertAdScheduleMapsFields(t *testing.T) {
	next := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	last := time.Date(2026, 6, 16, 11, 40, 0, 0, time.UTC)
	snooze := time.Date(2026, 6, 16, 12, 20, 0, 0, time.UTC)

	got := convertAdSchedule(twitch.AdSchedule{
		NextAdAt:        next,
		LastAdAt:        last,
		Duration:        90 * time.Second,
		PrerollFreeTime: 20 * time.Minute,
		SnoozeCount:     2,
		SnoozeRefreshAt: snooze,
	})

	if !got.NextAdAt.Equal(next) || !got.LastAdAt.Equal(last) || !got.SnoozeRefreshAt.Equal(snooze) {
		t.Fatalf("times = %#v", got)
	}
	if got.Duration != 90*time.Second || got.PrerollFreeTime != 20*time.Minute || got.SnoozeCount != 2 {
		t.Fatalf("schedule = %#v", got)
	}
}
