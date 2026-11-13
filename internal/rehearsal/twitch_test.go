// The simulator exists only in test binaries. Production endpoint configuration,
// credential checks and identity verification cannot be switched into mock mode.
package rehearsal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type peer struct {
	mu    sync.Mutex
	conn  *websocket.Conn
	id    string
	types map[string]bool
}

func (p *peer) send(v any) error { p.mu.Lock(); defer p.mu.Unlock(); return p.conn.WriteJSON(v) }

type twitchSimulator struct {
	mu                           sync.Mutex
	server                       *httptest.Server
	peers                        map[string]*peer
	serial, subscriptions, polls int
	stream, mode                 string
	failedSend                   bool
	sent, attempted              []string
}

func newSimulator(t *testing.T) *twitchSimulator {
	t.Helper()
	s := &twitchSimulator{peers: map[string]*peer{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	transport := &http.Transport{Proxy: nil}
	oldHTTP, oldWS := http.DefaultTransport, websocket.DefaultDialer
	base, _ := url.Parse(s.server.URL)
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		// No request can fall through to a real Twitch endpoint or other network host.
		if r.URL.Host != "api.twitch.tv" {
			return nil, fmt.Errorf("rehearsal blocked outbound host %s", r.URL.Host)
		}
		s.mu.Lock()
		mode := s.mode
		s.mu.Unlock()
		if r.URL.Path == "/helix/streams" && mode == "timeout" {
			return nil, context.DeadlineExceeded
		}
		next := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = base.Scheme
		u.Host = base.Host
		next.URL = &u
		next.Host = base.Host
		return transport.RoundTrip(next)
	})
	dialer := *oldWS
	dialer.Proxy = nil
	dialer.NetDialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "eventsub.wss.twitch.tv:443" {
			return nil, errors.New("rehearsal blocked websocket destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, s.server.Listener.Addr().String())
	}
	websocket.DefaultDialer = &dialer
	t.Cleanup(func() {
		s.mu.Lock()
		for _, p := range s.peers {
			p.conn.Close()
		}
		s.mu.Unlock()
		s.server.Close()
		transport.CloseIdleConnections()
		http.DefaultTransport = oldHTTP
		websocket.DefaultDialer = oldWS
	})
	return s
}
func (s *twitchSimulator) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ws" {
		s.websocket(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer rehearsal-token" || r.Header.Get("Client-Id") != "rehearsal-client" {
		http.Error(w, "invalid rehearsal credentials", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/helix/streams":
		s.mu.Lock()
		s.polls++
		stream, mode := s.stream, s.mode
		s.mu.Unlock()
		switch mode {
		case "malformed":
			fmt.Fprint(w, `{"unexpected":true}`)
			return
		case "http-error":
			http.Error(w, "unavailable", 503)
			return
		case "rate-limit":
			w.Header().Set("Retry-After", "120")
			http.Error(w, "rate limited", 429)
			return
		}
		if stream == "" {
			fmt.Fprint(w, `{"data":[]}`)
		} else {
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": stream, "user_login": "rehearsal", "started_at": "2026-11-13T12:00:00Z"}}})
		}
	case "/helix/eventsub/subscriptions":
		var payload struct {
			Type      string            `json:"type"`
			Condition map[string]string `json:"condition"`
			Transport struct {
				SessionID string `json:"session_id"`
			} `json:"transport"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Condition["broadcaster_user_id"] != "owner" {
			http.Error(w, "invalid subscription", 400)
			return
		}
		s.mu.Lock()
		p := s.peers[payload.Transport.SessionID]
		if p != nil {
			p.types[payload.Type] = true
			s.subscriptions++
		}
		s.mu.Unlock()
		if p == nil {
			http.Error(w, "unknown session", 400)
			return
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"data":[]}`)
	case "/helix/chat/messages":
		var payload struct {
			Broadcaster string `json:"broadcaster_id"`
			Sender      string `json:"sender_id"`
			Message     string `json:"message"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Broadcaster != "owner" || payload.Sender != "bot" {
			http.Error(w, "invalid chat identity", 400)
			return
		}
		s.mu.Lock()
		s.attempted = append(s.attempted, payload.Message)
		fail := s.failedSend
		s.failedSend = false
		if !fail {
			s.sent = append(s.sent, payload.Message)
		}
		s.mu.Unlock()
		if fail {
			http.Error(w, "send unavailable", 503)
			return
		}
		fmt.Fprint(w, `{"data":[{"message_id":"sent","is_sent":true}]}`)
	default:
		http.Error(w, "unexpected mock API route", 404)
	}
}
func (s *twitchSimulator) websocket(w http.ResponseWriter, r *http.Request) {
	conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.serial++
	id := fmt.Sprintf("session-%d", s.serial)
	types := map[string]bool{}
	if previous := r.URL.Query().Get("migrate"); previous != "" {
		if old := s.peers[previous]; old != nil {
			for kind := range old.types {
				types[kind] = true
			}
			id = previous
		}
	}
	p := &peer{conn: conn, id: id, types: types}
	s.peers[id] = p
	s.mu.Unlock()
	defer func() {
		conn.Close()
		s.mu.Lock()
		if s.peers[id] == p {
			delete(s.peers, id)
		}
		s.mu.Unlock()
	}()
	if p.send(map[string]any{"metadata": map[string]any{"message_type": "session_welcome"}, "payload": map[string]any{"session": map[string]any{"id": id, "keepalive_timeout_seconds": 60}}}) != nil {
		return
	}
	for {
		if _, _, err = conn.ReadMessage(); err != nil {
			return
		}
	}
}
func (s *twitchSimulator) configure(stream, mode string) {
	s.mu.Lock()
	s.stream = stream
	s.mode = mode
	s.mu.Unlock()
}
func (s *twitchSimulator) notify(t *testing.T, kind, id string, event map[string]any) {
	t.Helper()
	s.mu.Lock()
	var targets []*peer
	for _, p := range s.peers {
		if p.types[kind] {
			targets = append(targets, p)
		}
	}
	s.mu.Unlock()
	if len(targets) != 1 {
		t.Fatalf("%s has %d subscriptions", kind, len(targets))
	}
	err := targets[0].send(map[string]any{"metadata": map[string]any{"message_type": "notification", "message_id": id, "subscription_type": kind}, "payload": map[string]any{"subscription": map[string]any{"type": kind}, "event": event}})
	if err != nil {
		t.Fatal(err)
	}
}
func (s *twitchSimulator) command(t *testing.T, id, user, text string, mod bool) {
	event := map[string]any{"broadcaster_user_id": "owner", "broadcaster_user_login": "rehearsal", "chatter_user_id": user, "chatter_user_login": strings.ToLower(user), "chatter_user_name": user, "message_id": id, "message": map[string]any{"text": text}}
	if mod {
		event["badges"] = []any{map[string]any{"set_id": "moderator", "id": "1"}}
	}
	s.notify(t, "channel.chat.message", id, event)
}
func (s *twitchSimulator) outputs() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...), append([]string(nil), s.attempted...)
}
func (s *twitchSimulator) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subscriptions, s.polls
}
func (s *twitchSimulator) reconnect(t *testing.T, migrate bool) {
	t.Helper()
	s.mu.Lock()
	var p *peer
	for _, candidate := range s.peers {
		if candidate.types["channel.chat.message"] {
			p = candidate
			break
		}
	}
	s.mu.Unlock()
	if p == nil {
		t.Fatal("no chat session")
	}
	if !migrate {
		p.conn.Close()
		return
	}
	if err := p.send(map[string]any{"metadata": map[string]any{"message_type": "session_reconnect"}, "payload": map[string]any{"session": map[string]any{"reconnect_url": "wss://eventsub.wss.twitch.tv/ws?migrate=" + p.id}}}); err != nil {
		t.Fatal(err)
	}
}
