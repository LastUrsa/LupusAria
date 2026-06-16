package twitch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGetStreamInfoParsesLiveAndOfflineResponses(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		if req.URL.Path != "/helix/streams" {
			t.Fatalf("path = %q, want /helix/streams", req.URL.Path)
		}
		if got := req.URL.Query().Get("user_login"); got != "lastursa" {
			t.Fatalf("user_login = %q", got)
		}
		return `{"data":[{"user_login":"lastursa","title":"Testing","game_name":"Music","viewer_count":7,"started_at":"2026-06-16T12:00:00Z"}]}`
	})

	info, err := client.GetStreamInfo(context.Background(), "lastursa")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Live || info.Title != "Testing" || info.GameName != "Music" || info.ViewerCount != 7 {
		t.Fatalf("stream info = %#v", info)
	}

	client = newTestHelixClient(t, func(*http.Request) string {
		return `{"data":[]}`
	})
	info, err = client.GetStreamInfo(context.Background(), "lastursa")
	if err != nil {
		t.Fatal(err)
	}
	if info.Live {
		t.Fatalf("offline stream should not be live: %#v", info)
	}
}

func TestGetUsersByLoginNormalizesLogins(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		values := req.URL.Query()["login"]
		if len(values) != 2 || values[0] != "alice" || values[1] != "bob" {
			t.Fatalf("login query = %#v", values)
		}
		return `{"data":[{"id":"1","login":"alice","display_name":"Alice","broadcaster_type":"affiliate"}]}`
	})

	users, err := client.GetUsersByLogin(context.Background(), []string{" Alice ", "@Bob", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %#v", users)
	}
	if users[0].ID != "1" || users[0].DisplayName != "Alice" || users[0].BroadcasterType != "affiliate" {
		t.Fatalf("user = %#v", users[0])
	}
}

func TestGetRecentStreamParsesLatestArchive(t *testing.T) {
	want := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	client := newTestHelixClient(t, func(req *http.Request) string {
		query := req.URL.Query()
		if query.Get("user_id") != "123" || query.Get("type") != "archive" || query.Get("first") != "1" {
			t.Fatalf("query = %s", query.Encode())
		}
		return `{"data":[{"created_at":"2026-06-16T12:00:00Z"}]}`
	})

	got, ok, err := client.GetRecentStream(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !got.Equal(want) {
		t.Fatalf("created_at = %s, ok = %v", got, ok)
	}
}

func TestGetChattersHandlesPagination(t *testing.T) {
	calls := 0
	client := newTestHelixClient(t, func(req *http.Request) string {
		calls++
		query := req.URL.Query()
		if query.Get("broadcaster_id") != "broadcaster" || query.Get("moderator_id") != "moderator" || query.Get("first") != "1000" {
			t.Fatalf("query = %s", query.Encode())
		}
		if calls == 1 {
			if query.Get("after") != "" {
				t.Fatalf("first call after = %q", query.Get("after"))
			}
			return `{"data":[{"user_id":"1","user_login":"alice","user_name":"Alice"}],"pagination":{"cursor":"next"}}`
		}
		if query.Get("after") != "next" {
			t.Fatalf("second call after = %q", query.Get("after"))
		}
		return `{"data":[{"user_id":"2","user_login":"bob","user_name":"Bob"}],"pagination":{}}`
	})

	chatters, err := client.GetChatters(context.Background(), "broadcaster", "moderator")
	if err != nil {
		t.Fatal(err)
	}
	if len(chatters) != 2 || chatters[0].UserLogin != "alice" || chatters[1].UserLogin != "bob" {
		t.Fatalf("chatters = %#v", chatters)
	}
}

func TestGetAdScheduleParsesFlexibleFields(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		if req.URL.Query().Get("broadcaster_id") != "broadcaster" {
			t.Fatalf("query = %s", req.URL.RawQuery)
		}
		return `{"data":[{
			"next_ad_at":"2026-06-16T12:10:00Z",
			"last_ad_at":"2026-06-16T11:40:00Z",
			"duration":"90",
			"preroll_free_time":120,
			"snooze_count":"2",
			"snooze_refresh_at":"2026-06-16T12:20:00Z"
		}]}`
	})

	schedule, err := client.GetAdSchedule(context.Background(), "broadcaster")
	if err != nil {
		t.Fatal(err)
	}
	if schedule.Duration != 90*time.Second || schedule.PrerollFreeTime != 120*time.Second || schedule.SnoozeCount != 2 {
		t.Fatalf("schedule = %#v", schedule)
	}
	if schedule.NextAdAt.IsZero() || schedule.LastAdAt.IsZero() || schedule.SnoozeRefreshAt.IsZero() {
		t.Fatalf("expected parsed times: %#v", schedule)
	}
}

func TestGetJSONReturnsHTTPStatusError(t *testing.T) {
	client := newTestHelixClientWithStatus(t, http.StatusUnauthorized, func(*http.Request) string {
		return `{"message":"bad token"}`
	})

	var target any
	err := client.getJSON(context.Background(), "https://api.twitch.tv/helix/users", &target)
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestTrimOAuthPrefix(t *testing.T) {
	if got := trimOAuthPrefix("oauth:abc"); got != "abc" {
		t.Fatalf("trimOAuthPrefix = %q", got)
	}
	if got := trimOAuthPrefix("abc"); got != "abc" {
		t.Fatalf("trimOAuthPrefix without prefix = %q", got)
	}
}

func newTestHelixClient(t *testing.T, bodyFor func(*http.Request) string) *HelixClient {
	t.Helper()
	return newTestHelixClientWithStatus(t, http.StatusOK, bodyFor)
}

func newTestHelixClientWithStatus(t *testing.T, status int, bodyFor func(*http.Request) string) *HelixClient {
	t.Helper()
	client := NewHelixClient("client-id", "oauth:access-token")
	client.httpClient = &http.Client{Transport: helixRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		assertHelixRequest(t, req)
		return &http.Response{
			StatusCode: status,
			Status:     statusCodeAndText(status),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(bodyFor(req))),
		}, nil
	})}
	return client
}

func statusCodeAndText(status int) string {
	return strconv.Itoa(status) + " " + http.StatusText(status)
}

func assertHelixRequest(t *testing.T, req *http.Request) {
	t.Helper()
	if req.URL.Scheme != "https" || req.URL.Host != "api.twitch.tv" {
		t.Fatalf("url = %s", req.URL.String())
	}
	if _, err := url.Parse(req.URL.String()); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Client-Id"); got != "client-id" {
		t.Fatalf("client id = %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
		t.Fatalf("authorization = %q", got)
	}
}

type helixRoundTripFunc func(*http.Request) (*http.Response, error)

func (f helixRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
