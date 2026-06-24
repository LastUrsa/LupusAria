package twitch

import (
	"context"
	"encoding/json"
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

func TestIsChannelFollowerChecksSpecificUser(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		query := req.URL.Query()
		if req.URL.Path != "/helix/channels/followers" {
			t.Fatalf("path = %q, want /helix/channels/followers", req.URL.Path)
		}
		if query.Get("broadcaster_id") != "broadcaster" || query.Get("user_id") != "viewer" {
			t.Fatalf("query = %s", query.Encode())
		}
		return `{"data":[{"user_id":"viewer","user_login":"viewer","user_name":"Viewer"}],"pagination":{},"total":1}`
	})

	follows, err := client.IsChannelFollower(context.Background(), "broadcaster", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if !follows {
		t.Fatal("expected user to follow channel")
	}
}

func TestIsChannelFollowerReturnsFalseForEmptyResult(t *testing.T) {
	client := newTestHelixClient(t, func(*http.Request) string {
		return `{"data":[],"pagination":{},"total":0}`
	})

	follows, err := client.IsChannelFollower(context.Background(), "broadcaster", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if follows {
		t.Fatal("expected empty follower result to be false")
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

func TestGetChannelEmotesParsesNames(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		if req.URL.Path != "/helix/chat/emotes" {
			t.Fatalf("path = %q, want /helix/chat/emotes", req.URL.Path)
		}
		if got := req.URL.Query().Get("broadcaster_id"); got != "broadcaster" {
			t.Fatalf("broadcaster_id = %q", got)
		}
		return `{"data":[{"id":"111","name":"lasturPride"},{"id":"222","name":"lupuseNod"},{"id":"","name":"ignored"}]}`
	})

	emotes, err := client.GetChannelEmotes(context.Background(), "broadcaster")
	if err != nil {
		t.Fatal(err)
	}
	if len(emotes) != 2 || emotes[0].ID != "111" || emotes[0].Name != "lasturPride" || emotes[1].Name != "lupuseNod" {
		t.Fatalf("emotes = %#v", emotes)
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

func TestGetAdScheduleParsesUnixTimestampFields(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		if req.URL.Query().Get("broadcaster_id") != "broadcaster" {
			t.Fatalf("query = %s", req.URL.RawQuery)
		}
		return `{"data":[{
			"next_ad_at":1782262592,
			"last_ad_at":1782258992,
			"duration":180,
			"preroll_free_time":3531,
			"snooze_count":3,
			"snooze_refresh_at":0
		}]}`
	})

	schedule, err := client.GetAdSchedule(context.Background(), "broadcaster")
	if err != nil {
		t.Fatal(err)
	}
	if !schedule.NextAdAt.Equal(time.Unix(1782262592, 0).UTC()) {
		t.Fatalf("next ad = %s", schedule.NextAdAt)
	}
	if !schedule.LastAdAt.Equal(time.Unix(1782258992, 0).UTC()) {
		t.Fatalf("last ad = %s", schedule.LastAdAt)
	}
	if !schedule.SnoozeRefreshAt.IsZero() {
		t.Fatalf("snooze refresh = %s, want zero", schedule.SnoozeRefreshAt)
	}
}

func TestCreateEventSubWebSocketSubscriptionPostsSessionTransport(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/helix/eventsub/subscriptions" {
			t.Fatalf("path = %q, want /helix/eventsub/subscriptions", req.URL.Path)
		}
		var body struct {
			Type      string            `json:"type"`
			Version   string            `json:"version"`
			Condition map[string]string `json:"condition"`
			Transport struct {
				Method    string `json:"method"`
				SessionID string `json:"session_id"`
			} `json:"transport"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Type != "channel.chat.message" || body.Version != "1" {
			t.Fatalf("subscription = %#v", body)
		}
		if body.Condition["broadcaster_user_id"] != "broadcaster" || body.Condition["user_id"] != "bot" {
			t.Fatalf("condition = %#v", body.Condition)
		}
		if body.Transport.Method != "websocket" || body.Transport.SessionID != "session-123" {
			t.Fatalf("transport = %#v", body.Transport)
		}
		return `{"data":[]}`
	})

	err := client.CreateEventSubWebSocketSubscription(context.Background(), "channel.chat.message", "1", map[string]string{
		"broadcaster_user_id": "broadcaster",
		"user_id":             "bot",
	}, "session-123")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendChatMessagePostsMessageAndReturnsID(t *testing.T) {
	client := newTestHelixClient(t, func(req *http.Request) string {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.Path != "/helix/chat/messages" {
			t.Fatalf("path = %q, want /helix/chat/messages", req.URL.Path)
		}
		var body struct {
			BroadcasterID        string `json:"broadcaster_id"`
			SenderID             string `json:"sender_id"`
			Message              string `json:"message"`
			ReplyParentMessageID string `json:"reply_parent_message_id"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.BroadcasterID != "broadcaster" || body.SenderID != "bot" || body.Message != "Hello chat" || body.ReplyParentMessageID != "parent" {
			t.Fatalf("body = %#v", body)
		}
		return `{"data":[{"message_id":"message-123","is_sent":true,"drop_reason":null}]}`
	})

	result, err := client.SendChatMessage(context.Background(), "broadcaster", "bot", "Hello\nchat", "parent")
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageID != "message-123" || !result.IsSent {
		t.Fatalf("result = %#v", result)
	}
}

func TestSendChatMessageReturnsDropReason(t *testing.T) {
	client := newTestHelixClient(t, func(*http.Request) string {
		return `{"data":[{"message_id":"","is_sent":false,"drop_reason":{"code":"automod_held","message":"held by automod"}}]}`
	})

	result, err := client.SendChatMessage(context.Background(), "broadcaster", "bot", "Hello chat", "")
	if err == nil || !strings.Contains(err.Error(), "held by automod") {
		t.Fatalf("err = %v", err)
	}
	if result.DropCode != "automod_held" {
		t.Fatalf("result = %#v", result)
	}
}

func TestGetJSONReturnsHTTPStatusError(t *testing.T) {
	client := newTestHelixClientWithStatus(t, http.StatusUnauthorized, func(*http.Request) string {
		return `{"message":"bad token"}`
	})

	var target any
	err := client.getJSON(context.Background(), "https://api.twitch.tv/helix/users", &target)
	if err == nil || !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("err = %v, want 401 with response body", err)
	}
}

func TestPostJSONReturnsHTTPStatusErrorBody(t *testing.T) {
	client := newTestHelixClientWithStatus(t, http.StatusForbidden, func(*http.Request) string {
		return `{"message":"missing required scope channel:read:ads"}`
	})

	var target any
	err := client.postJSON(context.Background(), "https://api.twitch.tv/helix/eventsub/subscriptions", map[string]string{"type": "test"}, &target)
	if err == nil || !strings.Contains(err.Error(), "403 Forbidden") || !strings.Contains(err.Error(), "channel:read:ads") {
		t.Fatalf("err = %v, want 403 with response body", err)
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
