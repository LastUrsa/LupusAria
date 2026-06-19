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

type fakeAdHelix struct {
	schedules []twitch.AdSchedule
	errs      []error
	tokens    []string
	calls     int
}

func (f *fakeAdHelix) GetAdSchedule(context.Context, string) (twitch.AdSchedule, error) {
	call := f.calls
	f.calls++
	if call < len(f.errs) && f.errs[call] != nil {
		return twitch.AdSchedule{}, f.errs[call]
	}
	if call < len(f.schedules) {
		return f.schedules[call], nil
	}
	return twitch.AdSchedule{}, nil
}

func (f *fakeAdHelix) SetAccessToken(accessToken string) {
	f.tokens = append(f.tokens, accessToken)
}

type fakeAdRefresher struct {
	tokens []twitch.TokenSet
	err    error
	calls  int
}

func (f *fakeAdRefresher) Refresh(context.Context) (twitch.TokenSet, error) {
	f.calls++
	if f.err != nil {
		return twitch.TokenSet{}, f.err
	}
	if len(f.tokens) == 0 {
		return twitch.TokenSet{}, nil
	}
	token := f.tokens[0]
	f.tokens = f.tokens[1:]
	return token, nil
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

func TestAdScheduleProviderRefreshesExpiringTokenBeforePolling(t *testing.T) {
	next := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	helix := &fakeAdHelix{schedules: []twitch.AdSchedule{{
		NextAdAt: next,
		Duration: 90 * time.Second,
	}}}
	refresher := &fakeAdRefresher{tokens: []twitch.TokenSet{{
		AccessToken:  "fresh-token",
		RefreshToken: "fresh-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}}}
	provider := &twitchAdScheduleProvider{
		helix:          helix,
		auth:           refresher,
		tokenExpiresAt: time.Now().Add(time.Minute),
	}

	schedule, err := provider.GetAdSchedule(context.Background(), "broadcaster")
	if err != nil {
		t.Fatal(err)
	}
	if schedule.NextAdAt != next || schedule.Duration != 90*time.Second {
		t.Fatalf("schedule = %#v", schedule)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresher.calls)
	}
	if len(helix.tokens) != 1 || helix.tokens[0] != "fresh-token" {
		t.Fatalf("tokens = %#v", helix.tokens)
	}
}

func TestAdScheduleProviderRefreshesAndRetriesUnauthorizedPoll(t *testing.T) {
	next := time.Date(2026, 6, 16, 12, 10, 0, 0, time.UTC)
	helix := &fakeAdHelix{
		errs: []error{errors.New("twitch helix returned 401 Unauthorized: bad token")},
		schedules: []twitch.AdSchedule{
			{},
			{NextAdAt: next, Duration: time.Minute},
		},
	}
	refresher := &fakeAdRefresher{tokens: []twitch.TokenSet{{
		AccessToken:  "fresh-token",
		RefreshToken: "fresh-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}}}
	provider := &twitchAdScheduleProvider{
		helix:          helix,
		auth:           refresher,
		tokenExpiresAt: time.Now().Add(time.Hour),
	}

	schedule, err := provider.GetAdSchedule(context.Background(), "broadcaster")
	if err != nil {
		t.Fatal(err)
	}
	if schedule.NextAdAt != next || schedule.Duration != time.Minute {
		t.Fatalf("schedule = %#v", schedule)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresher.calls)
	}
	if helix.calls != 2 {
		t.Fatalf("helix calls = %d, want 2", helix.calls)
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
