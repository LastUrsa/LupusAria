package twitch

import "testing"

func TestSanitizeIRCLineStripsLineBreaks(t *testing.T) {
	got := sanitizeIRCLine("PRIVMSG #lastursa :hello\r\nPASS oauth:stolen\nJOIN #other")
	want := "PRIVMSG #lastursa :hello PASS oauth:stolen JOIN #other"
	if got != want {
		t.Fatalf("sanitizeIRCLine = %q, want %q", got, want)
	}
}

func TestParseMessageExtractsIdentityAndBadges(t *testing.T) {
	raw := "@badges=broadcaster/1;color=;display-name=Ursa;mod=0 :lastursa!lastursa@lastursa.tmi.twitch.tv PRIVMSG #lastursa :hello chat"
	msg, ok := parseMessage(raw)
	if !ok {
		t.Fatal("expected message to parse")
	}
	if msg.Channel != "lastursa" || msg.Username != "lastursa" || msg.DisplayName != "Ursa" {
		t.Fatalf("message identity = %#v", msg)
	}
	if !msg.IsBroadcaster || !msg.IsMod {
		t.Fatalf("badges not parsed: %#v", msg)
	}
	if msg.Text != "hello chat" {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestParseMessageExtractsReplyContext(t *testing.T) {
	raw := `@badges=;display-name=ragenowich;reply-parent-display-name=LupusAria;reply-parent-user-login=lupusaria;reply-parent-msg-body=@ragenowich\sAlways\shappy\sto\shighlight\sLastUrsa :ragenowich!ragenowich@ragenowich.tmi.twitch.tv PRIVMSG #lastursa :who's that?`
	msg, ok := parseMessage(raw)
	if !ok {
		t.Fatal("expected message to parse")
	}
	if msg.ReplyParentDisplayName != "LupusAria" {
		t.Fatalf("reply parent display name = %q", msg.ReplyParentDisplayName)
	}
	if msg.ReplyParentUserLogin != "lupusaria" {
		t.Fatalf("reply parent login = %q", msg.ReplyParentUserLogin)
	}
	if msg.ReplyParentText != "@ragenowich Always happy to highlight LastUrsa" {
		t.Fatalf("reply parent text = %q", msg.ReplyParentText)
	}
}

func TestParseMessageExtractsEmotes(t *testing.T) {
	raw := `@badges=;display-name=Foxhound8492nd;emotes=emote-123:11-26/emote-456:28-32,34-38 :foxhound8492nd!foxhound8492nd@foxhound8492nd.tmi.twitch.tv PRIVMSG #lastursa :@LupusAria foxhou33Renegade Kappa Kappa`
	msg, ok := parseMessage(raw)
	if !ok {
		t.Fatal("expected message to parse")
	}
	if len(msg.Emotes) != 2 {
		t.Fatalf("emotes = %#v, want two", msg.Emotes)
	}
	if msg.Emotes[0].ID != "emote-123" || msg.Emotes[0].Name != "foxhou33Renegade" || msg.Emotes[0].Count != 1 {
		t.Fatalf("first emote = %#v", msg.Emotes[0])
	}
	if msg.Emotes[1].ID != "emote-456" || msg.Emotes[1].Name != "Kappa" || msg.Emotes[1].Count != 2 {
		t.Fatalf("second emote = %#v", msg.Emotes[1])
	}
}
