package announcements

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/twitch"
)

const (
	KindCommand = "command"
	KindTimer   = "timer"
)

type Chat interface {
	Say(channel, text string) error
}

type StreamInfoProvider interface {
	GetStreamInfo(ctx context.Context, channel string) (twitch.StreamInfo, error)
}

type Announcement struct {
	ID            string `json:"id"`
	Enabled       bool   `json:"enabled"`
	Kind          string `json:"kind"`
	Command       string `json:"command,omitempty"`
	AfterMinutes  int    `json:"afterMinutes,omitempty"`
	RepeatMinutes int    `json:"repeatMinutes,omitempty"`
	Message       string `json:"message"`
}

type Config struct {
	Enabled      bool
	Channel      string
	PollInterval time.Duration
	Items        []Announcement
}

type Service struct {
	cfg      Config
	chat     Chat
	stream   StreamInfoProvider
	logger   *slog.Logger
	now      func() time.Time
	started  time.Time
	sentMu   sync.Mutex
	sentKeys map[string]bool
}

func Load(path string) ([]Announcement, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Announcement
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return Normalize(items), nil
}

func Save(path string, items []Announcement) error {
	items = Normalize(items)
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func Normalize(items []Announcement) []Announcement {
	normalized := make([]Announcement, 0, len(items))
	for i, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		item.Command = normalizeCommand(item.Command)
		item.Message = strings.TrimSpace(item.Message)
		if item.ID == "" {
			item.ID = fmt.Sprintf("announcement-%d", i+1)
		}
		if item.Kind == "" {
			item.Kind = KindCommand
		}
		if item.Kind == KindCommand && item.Command == "" {
			item.Command = "!" + item.ID
		}
		if item.AfterMinutes < 0 {
			item.AfterMinutes = 0
		}
		if item.RepeatMinutes < 0 {
			item.RepeatMinutes = 0
		}
		normalized = append(normalized, item)
	}
	return normalized
}

func New(cfg Config, chat Chat, stream StreamInfoProvider, logger *slog.Logger) *Service {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:      cfg,
		chat:     chat,
		stream:   stream,
		logger:   logger,
		now:      time.Now,
		started:  time.Now(),
		sentKeys: map[string]bool{},
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.cfg.Enabled || s.stream == nil || !s.hasTimerItems() {
		return
	}
	go s.run(ctx)
}

func (s *Service) HandleCommand(ctx context.Context, msg twitch.Message, broadcaster bool) bool {
	if s == nil || !s.cfg.Enabled {
		return false
	}
	text := normalizeCommand(msg.Text)
	for _, item := range s.cfg.Items {
		if !item.Enabled || item.Kind != KindCommand || item.Command == "" {
			continue
		}
		if text != item.Command {
			continue
		}
		if !broadcaster {
			_ = s.chat.Say(msg.Channel, "Only the broadcaster can use announcement commands.")
			return true
		}
		if item.Message != "" {
			if err := s.chat.Say(msg.Channel, item.Message); err != nil {
				s.logger.Warn("failed to send command announcement", "id", item.ID, "error", err)
			}
		}
		return true
	}
	return false
}

func (s *Service) run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	s.checkTimers(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkTimers(ctx)
		}
	}
}

func (s *Service) checkTimers(ctx context.Context) {
	info, err := s.stream.GetStreamInfo(ctx, s.cfg.Channel)
	if err != nil {
		s.logger.Warn("failed to fetch stream info for announcements", "error", err)
		return
	}
	if !info.Live || info.StartedAt.IsZero() {
		s.resetSent()
		return
	}

	elapsed := s.now().Sub(info.StartedAt)
	streamKey := info.StartedAt.UTC().Format(time.RFC3339)
	for _, item := range s.cfg.Items {
		if !item.Enabled || item.Kind != KindTimer || item.Message == "" {
			continue
		}
		dueAt, slot, ok := nextDueAt(info.StartedAt, elapsed, item)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s:%s:%d", streamKey, item.ID, slot)
		if s.sent(key) {
			continue
		}
		if dueAt.Before(s.started) {
			s.markSent(key)
			continue
		}
		if err := s.chat.Say(s.cfg.Channel, item.Message); err != nil {
			s.logger.Warn("failed to send timed announcement", "id", item.ID, "error", err)
			continue
		}
		s.markSent(key)
	}
}

func nextDueAt(streamStarted time.Time, elapsed time.Duration, item Announcement) (time.Time, int, bool) {
	firstDue := time.Duration(item.AfterMinutes) * time.Minute
	if elapsed < firstDue {
		return time.Time{}, 0, false
	}
	slot := 0
	dueOffset := firstDue
	if item.RepeatMinutes > 0 {
		repeat := time.Duration(item.RepeatMinutes) * time.Minute
		slot = int((elapsed - firstDue) / repeat)
		dueOffset = firstDue + time.Duration(slot)*repeat
	}
	return streamStarted.Add(dueOffset), slot, true
}

func (s *Service) hasTimerItems() bool {
	for _, item := range s.cfg.Items {
		if item.Enabled && item.Kind == KindTimer {
			return true
		}
	}
	return false
}

func (s *Service) sent(key string) bool {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	return s.sentKeys[key]
}

func (s *Service) markSent(key string) {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	s.sentKeys[key] = true
}

func (s *Service) resetSent() {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	clear(s.sentKeys)
}

func normalizeCommand(value string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
