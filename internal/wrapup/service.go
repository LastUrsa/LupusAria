package wrapup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Event struct {
	MessageID string         `json:"messageId"`
	Type      string         `json:"type"`
	UserID    string         `json:"userId,omitempty"`
	Name      string         `json:"name,omitempty"`
	At        time.Time      `json:"at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Session struct {
	ID            string     `json:"id"`
	StreamID      string     `json:"streamId,omitempty"`
	BroadcasterID string     `json:"broadcasterId"`
	StartedAt     time.Time  `json:"startedAt"`
	EndedAt       *time.Time `json:"endedAt,omitempty"`
	Status        string     `json:"status"`
	Events        []Event    `json:"events"`
}

type Report struct {
	SessionID           string `json:"sessionId"`
	StreamID            string `json:"streamId"`
	Status              string `json:"status"`
	StartedAt           string `json:"startedAt"`
	FollowerCount       int    `json:"followerCount"`
	SubscriptionCount   int    `json:"subscriptionCount"`
	GiftedSubCount      int    `json:"giftedSubCount"`
	Bits                int    `json:"bits"`
	RaidCount           int    `json:"raidCount"`
	Formatted           string `json:"formatted"`
	Stats               string `json:"stats"`
	CompletenessWarning string `json:"completenessWarning,omitempty"`
}

type state struct {
	Sessions []Session       `json:"sessions"`
	Seen     map[string]bool `json:"seen"`
}
type Service struct {
	mu      sync.Mutex
	path    string
	data    state
	changed func(Report)
}

const lateEventWindow = 5 * time.Minute

func New(path string, changed func(Report)) (*Service, error) {
	s := &Service{path: path, changed: changed, data: state{Seen: map[string]bool{}}}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(b) > 0 && json.Unmarshal(b, &s.data) != nil {
		return nil, fmt.Errorf("read wrap-up state: invalid JSON")
	}
	if s.data.Seen == nil {
		s.data.Seen = map[string]bool{}
	}
	return s, nil
}

func (s *Service) Record(e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.MessageID != "" && s.data.Seen[e.MessageID] {
		return nil
	}
	i := s.eventTargetLocked(e.At)
	if i < 0 {
		return nil
	} // Support received while Twitch says offline belongs to no stream.
	s.data.Sessions[i].Events = append(s.data.Sessions[i].Events, e)
	if e.MessageID != "" {
		s.data.Seen[e.MessageID] = true
	}
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.emitLocked(i)
	return nil
}

func (s *Service) Online(streamID, broadcasterID string, started time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Sessions {
		if s.data.Sessions[i].StreamID == streamID && streamID != "" {
			s.data.Sessions[i].Status = "live"
			s.data.Sessions[i].EndedAt = nil
			return s.saveLocked()
		}
	}
	if started.IsZero() {
		started = time.Now().UTC()
	}
	for i := range s.data.Sessions {
		if s.data.Sessions[i].Status == "live" {
			ended := started
			s.data.Sessions[i].Status = "complete"
			s.data.Sessions[i].EndedAt = &ended
		}
	}
	id := streamID
	if id == "" {
		id = fmt.Sprintf("stream-%d", started.Unix())
	}
	s.data.Sessions = append(s.data.Sessions, Session{ID: id, StreamID: streamID, BroadcasterID: broadcasterID, StartedAt: started, Status: "live", Events: []Event{}})
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.emitLocked(len(s.data.Sessions) - 1)
	return nil
}

func (s *Service) Offline(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	for i := len(s.data.Sessions) - 1; i >= 0; i-- {
		if s.data.Sessions[i].Status == "live" {
			s.data.Sessions[i].Status = "complete"
			s.data.Sessions[i].EndedAt = &at
			if err := s.saveLocked(); err != nil {
				return err
			}
			s.emitLocked(i)
			return nil
		}
	}
	return nil
}

func (s *Service) Current() (Report, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.targetLocked()
	if i < 0 {
		return Report{}, false
	}
	return build(s.data.Sessions[i]), true
}
func (s *Service) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.targetLocked()
	if i < 0 {
		return errors.New("no stream session exists")
	}
	for _, e := range s.data.Sessions[i].Events {
		delete(s.data.Seen, e.MessageID)
	}
	s.data.Sessions[i].Events = nil
	if err := s.saveLocked(); err != nil {
		return err
	}
	s.emitLocked(i)
	return nil
}
func (s *Service) targetLocked() int {
	for i := len(s.data.Sessions) - 1; i >= 0; i-- {
		if s.data.Sessions[i].Status == "live" {
			return i
		}
	}
	if len(s.data.Sessions) > 0 {
		return len(s.data.Sessions) - 1
	}
	return -1
}
func (s *Service) eventTargetLocked(at time.Time) int {
	for i := len(s.data.Sessions) - 1; i >= 0; i-- {
		if s.data.Sessions[i].Status == "live" {
			return i
		}
	}
	if at.IsZero() {
		return -1
	}
	for i := len(s.data.Sessions) - 1; i >= 0; i-- {
		session := s.data.Sessions[i]
		if session.EndedAt != nil && !at.Before(session.StartedAt) && !at.After(session.EndedAt.Add(lateEventWindow)) {
			return i
		}
	}
	return -1
}
func (s *Service) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
func (s *Service) emitLocked(i int) {
	if s.changed != nil {
		r := build(s.data.Sessions[i])
		go s.changed(r)
	}
}

func build(sess Session) Report {
	type item struct {
		id, name, desc string
		at             time.Time
		amount         int
	}
	var follows, subs, raids []item
	var warning string
	bits := map[string]*item{}
	gifts, totalBits, paidSubs := 0, 0, 0
	seenFollowers := map[string]bool{}
	for _, e := range sess.Events {
		m := e.Metadata
		switch e.Type {
		case "gap":
			warning = "Twitch disconnected during this stream; events during the interruption may be missing."
		case "follow":
			if !seenFollowers[e.UserID] {
				seenFollowers[e.UserID] = true
				follows = append(follows, item{e.UserID, e.Name, "", e.At, 0})
			}
		case "subscribe":
			if gift, _ := m["isGift"].(bool); !gift {
				paidSubs++
				subs = append(subs, item{e.UserID, e.Name, tierText(str(m["tier"])) + " Subscription", e.At, 1})
			}
		case "resub":
			paidSubs++
			months := integer(m["months"])
			desc := fmt.Sprintf("%d-Month Resubscription", months)
			if months <= 0 {
				desc = "Resubscription"
			}
			subs = append(subs, item{e.UserID, e.Name, desc, e.At, 1})
		case "gift":
			n := integer(m["total"])
			if n < 1 {
				n = 1
			}
			gifts += n
			name := e.Name
			if name == "" {
				name = "Anonymous"
			}
			subs = append(subs, item{e.UserID, name, fmt.Sprintf("Gifted %d Subs", n), e.At, n})
		case "cheer":
			n := integer(m["bits"])
			totalBits += n
			key := e.UserID
			if key == "" {
				key = "anonymous"
			}
			if bits[key] == nil {
				bits[key] = &item{e.UserID, e.Name, "", e.At, 0}
			}
			bits[key].amount += n
		case "raid":
			raids = append(raids, item{e.UserID, e.Name, "", e.At, integer(m["viewers"])})
		}
	}
	sort.SliceStable(follows, func(i, j int) bool { return follows[i].at.Before(follows[j].at) })
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].at.Before(subs[j].at) })
	sort.SliceStable(raids, func(i, j int) bool { return raids[i].at.Before(raids[j].at) })
	bitList := make([]item, 0, len(bits))
	for _, v := range bits {
		if v.name == "" {
			v.name = "Anonymous"
		}
		bitList = append(bitList, *v)
	}
	sort.SliceStable(bitList, func(i, j int) bool {
		if bitList[i].amount == bitList[j].amount {
			return bitList[i].name < bitList[j].name
		}
		return bitList[i].amount > bitList[j].amount
	})
	var b strings.Builder
	b.WriteString("# STREAM WRAP-UP\n")
	section := func(title string, xs []item, format func(item) string) {
		if len(xs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n**%s**\n", title)
		for _, x := range xs {
			fmt.Fprintf(&b, "- %s\n", format(x))
		}
	}
	section("New Followers", follows, func(x item) string { return x.name })
	section("Subscriptions", subs, func(x item) string { return x.name + " — " + x.desc })
	section("Bits", bitList, func(x item) string { return fmt.Sprintf("%s — %s Bits", x.name, comma(x.amount)) })
	section("Raids", raids, func(x item) string { return fmt.Sprintf("%s — %s Viewers", x.name, comma(x.amount)) })
	if len(follows)+len(subs)+len(bitList)+len(raids) == 0 {
		b.WriteString("\nNo follows, subscriptions, Bits, or incoming raids have been recorded for this stream.\n")
	} else {
		b.WriteString("\n**Thank you all for the incredible support!**\n")
	}
	stats := fmt.Sprintf("Stream Stats: %d New Followers | %d Subs | %d Gift Subs | %s Bits | %d Raids", len(follows), paidSubs, gifts, comma(totalBits), len(raids))
	return Report{SessionID: sess.ID, StreamID: sess.StreamID, Status: sess.Status, StartedAt: sess.StartedAt.Format(time.RFC3339), FollowerCount: len(follows), SubscriptionCount: paidSubs, GiftedSubCount: gifts, Bits: totalBits, RaidCount: len(raids), Formatted: strings.TrimSpace(b.String()), Stats: stats, CompletenessWarning: warning}
}
func str(v any) string { s, _ := v.(string); return s }
func integer(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
func tierText(t string) string {
	switch t {
	case "2000":
		return "Tier 2"
	case "3000":
		return "Tier 3"
	default:
		return "Tier 1"
	}
}
func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
