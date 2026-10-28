package twitch

import (
	"context"
	"io"
	"lupusaria/internal/wrapup"
	"net/http"
	"strings"
	"testing"
)

func TestEventSubWrapUpNormalization(t *testing.T) {
	msg, err := parseEventSubMessage([]byte(`{"metadata":{"message_id":"delivery-1","message_type":"notification","subscription_type":"channel.raid"},"payload":{"event":{"from_broadcaster_user_id":"r1","from_broadcaster_user_name":"Raider","viewers":42}}}`))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := eventSubWrapUpEvent(msg.Metadata.SubscriptionType, msg.Metadata.MessageID, msg.Payload.Event)
	if !ok || e.MessageID != "delivery-1" || e.Type != "raid" || e.UserID != "r1" || e.Metadata["viewers"] != 42 {
		t.Fatalf("bad event: %#v", e)
	}
}

func TestOutgoingRaidIsNotNormalized(t *testing.T) {
	if _, ok := eventSubWrapUpEvent("channel.raid.outgoing", "id", eventSubChatEvent{}); ok {
		t.Fatal("outgoing raid accepted")
	}
}

func TestAllWrapUpEventTypesNormalize(t *testing.T) {
	tests := []struct {
		kind  string
		event eventSubChatEvent
		want  string
	}{
		{"stream.online", eventSubChatEvent{ID: "stream", BroadcasterUserID: "b", StartedAt: "2026-10-20T12:00:00Z"}, "online"},
		{"stream.offline", eventSubChatEvent{BroadcasterUserID: "b"}, "offline"},
		{"channel.follow", eventSubChatEvent{UserID: "u", UserName: "Follower", FollowedAt: "2026-10-20T12:01:00Z"}, "follow"},
		{"channel.subscribe", eventSubChatEvent{UserID: "u", UserName: "Subscriber", Tier: "1000"}, "subscribe"},
		{"channel.subscription.message", eventSubChatEvent{UserID: "u", UserName: "Resub", Tier: "2000", CumulativeMonths: 6}, "resub"},
		{"channel.subscription.gift", eventSubChatEvent{UserID: "u", UserName: "Gifter", Total: 5}, "gift"},
		{"channel.cheer", eventSubChatEvent{UserID: "u", UserName: "Cheerer", Bits: 100}, "cheer"},
		{"channel.raid", eventSubChatEvent{FromBroadcasterUserID: "u", FromBroadcasterUserName: "Raider", Viewers: 42}, "raid"},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			e, ok := eventSubWrapUpEvent(tc.kind, "delivery", tc.event)
			if !ok || e.Type != tc.want || e.MessageID != "delivery" {
				t.Fatalf("unexpected normalized event: %#v, %v", e, ok)
			}
		})
	}
}

func TestAnonymousWrapUpEventsDoNotInventIdentity(t *testing.T) {
	for _, kind := range []string{"channel.subscription.gift", "channel.cheer"} {
		e, ok := eventSubWrapUpEvent(kind, "id", eventSubChatEvent{UserID: "unexpected", UserName: "Unexpected", IsAnonymous: true, Total: 2, Bits: 100})
		if !ok || e.UserID != "" || e.Name != "Anonymous" {
			t.Fatalf("%s exposed anonymous identity: %#v", kind, e)
		}
	}
}

func TestWrapUpSubscriptionsUseMatchingBroadcasterCredentials(t *testing.T) {
	for _, separate := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared application", true: "separate applications"}[separate], func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			expectedID := "bot-app"
			cfg := EventSubConfig{ClientID: "bot-app", Token: "bot-token", BroadcasterToken: "broadcaster-token", BroadcasterID: "b", WrapUpEvents: make(chan wrapup.Event, 8)}
			if separate {
				cfg.BroadcasterClientID = "broadcaster-app"
				expectedID = "broadcaster-app"
			}
			calls := 0
			http.DefaultTransport = eventSubRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get("Client-ID") != expectedID || req.Header.Get("Authorization") != "Bearer broadcaster-token" {
					t.Errorf("incorrect credential pairing: client ID %q", req.Header.Get("Client-ID"))
				}
				return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Header: make(http.Header)}, nil
			})
			client := NewEventSubChatClient(cfg, nil)
			if err := client.subscribeToBroadcasterEvents(context.Background(), "session"); err != nil {
				t.Fatal(err)
			}
			if calls != 8 {
				t.Fatalf("subscriptions = %d, want 8", calls)
			}
		})
	}
}
