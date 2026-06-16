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
