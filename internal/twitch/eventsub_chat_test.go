package twitch

import "testing"

func TestNewEventSubChatClientUsesSendTokenForChatMessages(t *testing.T) {
	client := NewEventSubChatClient(EventSubConfig{
		ClientID:      "client-id",
		Token:         "user-token",
		SendToken:     "app-token",
		BroadcasterID: "broadcaster",
		UserID:        "bot",
	}, nil)

	if client.helix.accessToken != "user-token" {
		t.Fatalf("receive token = %q", client.helix.accessToken)
	}
	if client.sender.accessToken != "app-token" {
		t.Fatalf("send token = %q", client.sender.accessToken)
	}
}

func TestEventSubChatMessageToMessageMapsIdentityBadgesReplyAndEmotes(t *testing.T) {
	raw := `{
		"metadata": {
			"message_type": "notification",
			"subscription_type": "channel.chat.message",
			"subscription_version": "1"
		},
		"payload": {
			"event": {
				"broadcaster_user_login": "lastursa",
				"chatter_user_id": "user-123",
				"chatter_user_login": "viewer",
				"chatter_user_name": "Viewer",
				"message_id": "message-123",
				"message": {
					"text": "@LupusAria Kappa Kappa",
					"fragments": [
						{"type": "text", "text": "@LupusAria ", "emote": null},
						{"type": "emote", "text": "Kappa", "emote": {"id": "25"}},
						{"type": "text", "text": " ", "emote": null},
						{"type": "emote", "text": "Kappa", "emote": {"id": "25"}}
					]
				},
				"badges": [{"set_id": "moderator", "id": "1", "info": ""}],
				"reply": {
					"parent_message_id": "parent-123",
					"parent_message_body": "@Viewer hello there",
					"parent_user_id": "bot-123",
					"parent_user_login": "lupusaria",
					"parent_user_name": "LupusAria"
				}
			}
		}
	}`
	envelope, err := parseEventSubMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}

	msg, ok := eventSubChatMessageToMessage(envelope.Payload.Event, raw)
	if !ok {
		t.Fatal("expected chat message to map")
	}
	if msg.ID != "message-123" || msg.UserID != "user-123" || msg.Channel != "lastursa" || msg.Username != "viewer" || msg.DisplayName != "Viewer" {
		t.Fatalf("identity = %#v", msg)
	}
	if !msg.IsMod || msg.IsBroadcaster {
		t.Fatalf("badges = %#v", msg)
	}
	if len(msg.Emotes) != 1 || msg.Emotes[0].ID != "25" || msg.Emotes[0].Name != "Kappa" || msg.Emotes[0].Count != 2 {
		t.Fatalf("emotes = %#v", msg.Emotes)
	}
	if msg.ReplyParentDisplayName != "LupusAria" || msg.ReplyParentUserLogin != "lupusaria" || msg.ReplyParentText != "@Viewer hello there" {
		t.Fatalf("reply = %#v", msg)
	}
	if msg.Raw == "" {
		t.Fatal("expected raw payload")
	}
}
