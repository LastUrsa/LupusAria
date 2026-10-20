package twitch

import (
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
