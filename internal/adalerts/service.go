package adalerts

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Chat interface {
	Say(channel, text string) error
}

type Composer interface {
	ComposeAdAlert(ctx context.Context, event Event) (string, error)
}

type ScheduleProvider interface {
	GetAdSchedule(ctx context.Context, broadcasterID string) (Schedule, error)
}

type Config struct {
	Channel        string
	BroadcasterID  string
	Enabled        bool
	WarningLead    time.Duration
	PollInterval   time.Duration
	StartMessage   string
	EndMessage     string
	WarningMessage string
	Composer       Composer
}

type Event struct {
	Kind     string
	Lead     time.Duration
	Duration time.Duration
}

type AdBreakBegin struct {
	StartedAt time.Time
	Duration  time.Duration
	Automatic bool
}

const (
	EventWarning = "warning"
	EventStart   = "start"
	EventEnd     = "end"
)

type Schedule struct {
	NextAdAt         time.Time
	LastAdAt         time.Time
	Duration         time.Duration
	PrerollFreeTime  time.Duration
	SnoozeCount      int
	SnoozeRefreshAt  time.Time
	RawDuration      string
	RawPrerollFree   string
	RawSnoozeCount   string
	RawSnoozeRefresh string
	RawNextAdAt      string
	RawLastAdAt      string
}

type Service struct {
	cfg    Config
	chat   Chat
	helix  ScheduleProvider
	ctx    context.Context
	logger *slog.Logger
	now    func() time.Time

	mu           sync.Mutex
	warnedAdKey  string
	warnedStart  time.Time
	warnedEnd    time.Time
	warnedDur    time.Duration
	startedAdKey string
	endedAdKey   string
	activeAdKey  string
	activeEndAt  time.Time
	activeDur    time.Duration
}

func New(cfg Config, chat Chat, helix ScheduleProvider, logger *slog.Logger) *Service {
	if cfg.WarningLead <= 0 {
		cfg.WarningLead = 5 * time.Minute
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.WarningMessage == "" {
		cfg.WarningMessage = "Heads up: ads are scheduled in about %s."
	}
	if cfg.StartMessage == "" {
		cfg.StartMessage = "Ad break starting now. Good moment to stretch, hydrate, and rest your eyes."
	}
	if cfg.EndMessage == "" {
		cfg.EndMessage = "Welcome back. Ads should be done now."
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:    cfg,
		chat:   chat,
		helix:  helix,
		ctx:    context.Background(),
		logger: logger,
		now:    time.Now,
	}
}

func (s *Service) Start(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	if s.helix == nil || s.chat == nil || s.cfg.BroadcasterID == "" {
		s.logger.Info("ad alerts disabled; missing helix client, chat, or broadcaster ID")
		return
	}

	go func() {
		s.ctx = ctx
		s.logger.Info("ad alerts started", "broadcaster_id", s.cfg.BroadcasterID, "warning_lead", s.cfg.WarningLead, "poll_interval", s.cfg.PollInterval)
		s.poll(ctx)
		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.poll(ctx)
			}
		}
	}()
}

func (s *Service) poll(ctx context.Context) {
	schedule, err := s.helix.GetAdSchedule(ctx, s.cfg.BroadcasterID)
	if err != nil {
		s.logger.Warn("ad alert polling failed; will retry", "error", err)
		return
	}
	s.logger.Info("ad alert schedule polled",
		"next_ad_at", formatLogTime(schedule.NextAdAt),
		"last_ad_at", formatLogTime(schedule.LastAdAt),
		"duration", schedule.Duration,
		"preroll_free_time", schedule.PrerollFreeTime,
		"snooze_count", schedule.SnoozeCount,
	)
	s.HandleSchedule(schedule)
}

func (s *Service) HandleSchedule(schedule Schedule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleSchedule(s.ctx, schedule)
}

func (s *Service) HandleAdBreakBegin(ctx context.Context, event AdBreakBegin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if event.Duration <= 0 {
		s.logger.Info("ad break begin event ignored; missing duration")
		return
	}
	startedAt := event.StartedAt
	if startedAt.IsZero() {
		startedAt = s.now()
	}
	key := startedAt.UTC().Format(time.RFC3339)
	now := s.now()
	if s.activeAdKey != "" && now.Before(s.activeEndAt) {
		s.logger.Info("ad break begin event ignored; ad alert already active", "started_at", formatLogTime(startedAt), "duration", event.Duration)
		return
	}
	if s.startedAdKey == key {
		return
	}
	s.say(ctx, Event{Kind: EventStart, Duration: event.Duration}, s.cfg.StartMessage)
	s.startedAdKey = key
	s.activeAdKey = key
	s.activeEndAt = startedAt.Add(event.Duration)
	s.activeDur = event.Duration
	s.logger.Info("ad alert started from EventSub", "started_at", formatLogTime(startedAt), "duration", event.Duration, "automatic", event.Automatic)
}

func (s *Service) handleSchedule(ctx context.Context, schedule Schedule) {
	now := s.now()
	if !s.activeEndAt.IsZero() && !now.Before(s.activeEndAt) {
		duration := schedule.Duration
		if s.activeDur > 0 {
			duration = s.activeDur
		}
		s.sendEnd(ctx, duration)
	}
	s.synthesizeWarnedAd(ctx, now)
	if s.activeAdKey != "" && now.Before(s.activeEndAt) {
		s.logger.Info("ad alert schedule ignored; ad alert already active", "active_end_at", formatLogTime(s.activeEndAt))
		return
	}
	if schedule.NextAdAt.IsZero() || schedule.Duration <= 0 {
		s.logger.Info("ad alert schedule has no upcoming ad", "next_ad_at", formatLogTime(schedule.NextAdAt), "duration", schedule.Duration)
		return
	}

	key := schedule.NextAdAt.UTC().Format(time.RFC3339)
	startAt := schedule.NextAdAt
	endAt := startAt.Add(schedule.Duration)
	if s.activeAdKey != "" && now.Before(s.activeEndAt) {
		s.logger.Info("ad alert schedule ignored; ad alert already active", "active_end_at", formatLogTime(s.activeEndAt))
		return
	}
	if now.Before(startAt) {
		if s.warnedAdKey != key && !now.Before(startAt.Add(-s.cfg.WarningLead)) {
			lead := startAt.Sub(now).Round(time.Second)
			if lead < 0 {
				lead = 0
			}
			s.say(ctx, Event{Kind: EventWarning, Lead: lead, Duration: schedule.Duration}, fmt.Sprintf(s.cfg.WarningMessage, formatLead(lead)))
			s.warnedAdKey = key
			s.warnedStart = startAt
			s.warnedEnd = endAt
			s.warnedDur = schedule.Duration
		} else {
			s.logger.Info("ad alert waiting for warning window",
				"starts_at", formatLogTime(startAt),
				"warning_opens_at", formatLogTime(startAt.Add(-s.cfg.WarningLead)),
				"warning_lead", s.cfg.WarningLead,
			)
		}
		return
	}
	if now.Before(endAt) && s.startedAdKey != key {
		s.say(ctx, Event{Kind: EventStart, Duration: schedule.Duration}, s.cfg.StartMessage)
		s.startedAdKey = key
		s.activeAdKey = key
		s.activeEndAt = endAt
		s.activeDur = schedule.Duration
		return
	}
	if !now.Before(endAt) && s.startedAdKey == key && s.endedAdKey != key {
		s.activeAdKey = key
		s.activeEndAt = endAt
		s.activeDur = schedule.Duration
		s.sendEnd(ctx, schedule.Duration)
	}
}

func (s *Service) synthesizeWarnedAd(ctx context.Context, now time.Time) {
	if s.warnedAdKey == "" || s.warnedStart.IsZero() || s.warnedEnd.IsZero() {
		return
	}
	if now.Before(s.warnedStart) {
		return
	}
	if s.startedAdKey != s.warnedAdKey && now.Before(s.warnedEnd) {
		s.say(ctx, Event{Kind: EventStart, Duration: s.warnedDur}, s.cfg.StartMessage)
		s.startedAdKey = s.warnedAdKey
		s.activeAdKey = s.warnedAdKey
		s.activeEndAt = s.warnedEnd
		s.activeDur = s.warnedDur
		s.logger.Info("ad alert start synthesized from warned schedule", "started_at", formatLogTime(s.warnedStart), "duration", s.warnedDur)
		return
	}
	if !now.Before(s.warnedEnd) && s.startedAdKey == s.warnedAdKey && s.endedAdKey != s.warnedAdKey {
		s.activeAdKey = s.warnedAdKey
		s.activeEndAt = s.warnedEnd
		s.activeDur = s.warnedDur
		s.sendEnd(ctx, s.warnedDur)
	}
}

func formatLogTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *Service) sendEnd(ctx context.Context, duration time.Duration) {
	if s.activeAdKey == "" || s.endedAdKey == s.activeAdKey {
		return
	}
	s.say(ctx, Event{Kind: EventEnd, Duration: duration}, s.cfg.EndMessage)
	s.endedAdKey = s.activeAdKey
	s.activeAdKey = ""
	s.activeEndAt = time.Time{}
	s.activeDur = 0
}

func (s *Service) say(ctx context.Context, event Event, fallback string) {
	text := fallback
	if s.cfg.Composer != nil {
		composed, err := s.cfg.Composer.ComposeAdAlert(ctx, event)
		if err != nil {
			s.logger.Warn("failed to compose ad alert message; using configured fallback", "event", event.Kind, "error", err)
		} else if composed != "" {
			text = composed
		}
	}
	if err := s.chat.Say(s.cfg.Channel, text); err != nil {
		s.logger.Warn("failed to send ad alert", "event", event.Kind, "error", err)
		return
	}
	s.logger.Info("ad alert sent", "event", event.Kind, "channel", s.cfg.Channel)
}

func formatLead(d time.Duration) string {
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	if minutes == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", minutes)
}
