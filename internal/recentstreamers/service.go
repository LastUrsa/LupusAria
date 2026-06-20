package recentstreamers

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/twitch"
)

type Chat interface {
	Say(channel, text string) error
}

type Helix interface {
	GetUsersByLogin(ctx context.Context, logins []string) ([]twitch.UserInfo, error)
	GetRecentStream(ctx context.Context, userID string) (time.Time, bool, error)
	IsChannelFollower(ctx context.Context, broadcasterID, userID string) (bool, error)
	GetChatters(ctx context.Context, broadcasterID, moderatorID string) ([]twitch.Chatter, error)
	GetStreamInfo(ctx context.Context, channel string) (twitch.StreamInfo, error)
}

type Config struct {
	Channel              string
	Permission           string
	SORoulettePermission string
	RouletteStreamers    []string
	BroadcasterID        string
	ModeratorID          string
	MinWatch             time.Duration
	RecentWindow         time.Duration
	PageSize             int
	ShoutoutDelay        time.Duration
	CacheTTL             time.Duration
	ChatterPollInterval  time.Duration
}

const defaultShoutoutDelay = 5 * time.Second

type ViewerIdentity struct {
	Login       string
	DisplayName string
}

type Candidate struct {
	Login          string
	DisplayName    string
	Watch          time.Duration
	LastStreamedAt time.Time
}

type Service struct {
	cfg    Config
	chat   Chat
	helix  Helix
	logger *slog.Logger

	mu              sync.Mutex
	viewers         map[string]*viewerState
	queue           []Candidate
	nextIndex       int
	shoutedThisRun  map[string]bool
	userCache       map[string]cachedUser
	recentCache     map[string]cachedRecent
	followerCache   map[string]cachedFollower
	streamKey       string
	pollDisabled    bool
	pollWarned      bool
	dispatchRunning bool
	rng             *rand.Rand
}

type viewerState struct {
	Login        string
	DisplayName  string
	Watch        time.Duration
	Present      bool
	LastSnapshot time.Time
	LastSeen     time.Time
}

type cachedUser struct {
	user      twitch.UserInfo
	expiresAt time.Time
}

type cachedRecent struct {
	streamedAt time.Time
	ok         bool
	expiresAt  time.Time
}

type cachedFollower struct {
	follows   bool
	expiresAt time.Time
}

func New(cfg Config, chat Chat, helix Helix, logger *slog.Logger) *Service {
	if cfg.PageSize <= 0 {
		cfg.PageSize = 5
	}
	if cfg.MinWatch <= 0 {
		cfg.MinWatch = 15 * time.Minute
	}
	if cfg.RecentWindow <= 0 {
		cfg.RecentWindow = 14 * 24 * time.Hour
	}
	if cfg.ShoutoutDelay <= 0 {
		cfg.ShoutoutDelay = defaultShoutoutDelay
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 6 * time.Hour
	}
	if cfg.ChatterPollInterval <= 0 {
		cfg.ChatterPollInterval = time.Minute
	}
	cfg.Permission = normalizePermission(cfg.Permission, "mods")
	cfg.SORoulettePermission = normalizePermission(cfg.SORoulettePermission, cfg.Permission)
	cfg.RouletteStreamers = normalizeLoginList(cfg.RouletteStreamers)
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		cfg:            cfg,
		chat:           chat,
		helix:          helix,
		logger:         logger,
		viewers:        map[string]*viewerState{},
		shoutedThisRun: map[string]bool{},
		userCache:      map[string]cachedUser{},
		recentCache:    map[string]cachedRecent{},
		followerCache:  map[string]cachedFollower{},
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (s *Service) ObserveMessage(now time.Time, login, displayName string) {
	login = normalizeLogin(login)
	if login == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	viewer := s.ensureViewer(login, displayName)
	if !viewer.Present && !viewer.LastSeen.IsZero() && now.After(viewer.LastSeen) {
		gap := now.Sub(viewer.LastSeen)
		maxGap := s.cfg.ChatterPollInterval * 2
		if maxGap <= 0 {
			maxGap = 2 * time.Minute
		}
		if gap <= maxGap {
			viewer.Watch += gap
		}
	}
	viewer.LastSeen = now
}

func (s *Service) ApplySnapshot(now time.Time, viewers []ViewerIdentity) {
	present := map[string]ViewerIdentity{}
	for _, viewer := range viewers {
		login := normalizeLogin(viewer.Login)
		if login == "" {
			continue
		}
		viewer.Login = login
		present[login] = viewer
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for login, viewer := range s.viewers {
		if _, ok := present[login]; ok && viewer.Present && !viewer.LastSnapshot.IsZero() && now.After(viewer.LastSnapshot) {
			viewer.Watch += now.Sub(viewer.LastSnapshot)
		}
		viewer.Present = false
	}

	for login, identity := range present {
		viewer := s.ensureViewer(login, identity.DisplayName)
		viewer.Present = true
		viewer.LastSnapshot = now
		viewer.LastSeen = now
	}
}

func (s *Service) StartChatterPolling(ctx context.Context) {
	if s.helix == nil || s.cfg.BroadcasterID == "" || s.cfg.ModeratorID == "" {
		s.logger.Info("recent streamer chatter polling disabled; missing helix IDs")
		return
	}

	go func() {
		s.pollChatters(ctx)
		ticker := time.NewTicker(s.cfg.ChatterPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pollChatters(ctx)
			}
		}
	}()
}

func (s *Service) HandleCommand(ctx context.Context, msg twitch.Message) bool {
	text := strings.TrimSpace(msg.Text)
	lower := strings.ToLower(text)
	if lower != "!soroulette" && lower != "!autoso" && !strings.HasPrefix(lower, "!autoso ") {
		return false
	}

	if lower == "!soroulette" {
		if !permissionAllows(s.cfg.SORoulettePermission, msg) {
			_ = s.chat.Say(msg.Channel, permissionDeniedMessage(s.cfg.SORoulettePermission, "!soroulette"))
			return true
		}
		go s.sendRoulette(ctx, msg.Channel)
		return true
	}

	if !permissionAllows(s.cfg.Permission, msg) {
		_ = s.chat.Say(msg.Channel, permissionDeniedMessage(s.cfg.Permission, "!autoso"))
		return true
	}

	arg := strings.TrimSpace(text[len("!autoso"):])
	switch strings.ToLower(arg) {
	case "":
		go s.buildAndSend(ctx, msg.Channel, false)
	case "refresh":
		go s.buildAndSend(ctx, msg.Channel, true)
	case "next":
		go s.sendNext(ctx, msg.Channel)
	case "status":
		_ = s.chat.Say(msg.Channel, s.status())
	default:
		_ = s.chat.Say(msg.Channel, "Usage: !autoso, !autoso next, !autoso refresh, !autoso status, or !soroulette.")
	}
	return true
}

func (s *Service) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	watched := 0
	for _, viewer := range s.viewers {
		if viewer.Watch >= s.cfg.MinWatch && !s.isChannelOwner(viewer.Login) {
			watched++
		}
	}
	remaining := len(s.queue) - s.nextIndex
	if remaining < 0 {
		remaining = 0
	}
	return fmt.Sprintf("Streamer tracker: %d viewers over %s watch time, %d queued, %d already shouted this run.",
		watched, roundDuration(s.cfg.MinWatch), remaining, len(s.shoutedThisRun))
}

func (s *Service) buildAndSend(ctx context.Context, channel string, refresh bool) {
	if err := s.syncStreamRun(ctx); err != nil {
		s.logger.Warn("failed to sync stream shoutout state", "error", err)
		_ = s.chat.Say(channel, "I could not verify stream shoutout state right now.")
		return
	}
	candidates, err := s.buildQueue(ctx, time.Now())
	if err != nil {
		s.logger.Warn("failed to build recent streamer queue", "error", err)
		_ = s.chat.Say(channel, "I could not build the streamer list right now.")
		return
	}

	s.mu.Lock()
	s.queue = candidates
	s.nextIndex = 0
	s.mu.Unlock()

	if refresh {
		_ = s.chat.Say(channel, "Streamer list refreshed.")
	}
	s.sendNext(ctx, channel)
}

func (s *Service) buildQueue(ctx context.Context, now time.Time) ([]Candidate, error) {
	if s.helix == nil {
		return nil, fmt.Errorf("recent streamer lookups require Twitch Helix")
	}
	if s.cfg.BroadcasterID == "" {
		return nil, fmt.Errorf("recent streamer follower checks require broadcaster ID")
	}

	viewers := s.viewerCandidates()
	if len(viewers) == 0 {
		return nil, nil
	}

	logins := make([]string, 0, len(viewers))
	watchByLogin := map[string]time.Duration{}
	for _, viewer := range viewers {
		login := normalizeLogin(viewer.Login)
		if s.alreadyShouted(login) {
			continue
		}
		logins = append(logins, login)
		watchByLogin[login] = viewer.Watch
	}

	users, err := s.getUsers(ctx, now, logins)
	if err != nil {
		return nil, err
	}

	candidates := make([]Candidate, 0, len(users))
	for _, user := range users {
		follows, err := s.isFollower(ctx, now, user.ID)
		if err != nil {
			return nil, err
		}
		if !follows {
			continue
		}
		streamedAt, ok, err := s.getRecentStream(ctx, now, user.ID)
		if err != nil {
			return nil, err
		}
		if !ok || now.Sub(streamedAt) > s.cfg.RecentWindow {
			continue
		}
		display := user.DisplayName
		if display == "" {
			display = user.Login
		}
		candidates = append(candidates, Candidate{
			Login:          normalizeLogin(user.Login),
			DisplayName:    display,
			Watch:          watchByLogin[normalizeLogin(user.Login)],
			LastStreamedAt: streamedAt,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].LastStreamedAt.After(candidates[j].LastStreamedAt)
	})
	return candidates, nil
}

func (s *Service) sendNext(ctx context.Context, channel string) {
	if err := s.syncStreamRun(ctx); err != nil {
		s.logger.Warn("failed to sync stream shoutout state", "error", err)
		_ = s.chat.Say(channel, "I could not verify stream shoutout state right now.")
		return
	}
	s.mu.Lock()
	if s.dispatchRunning {
		s.mu.Unlock()
		_ = s.chat.Say(channel, "Streamer shoutouts are already running.")
		return
	}

	page := make([]Candidate, 0, s.cfg.PageSize)
	for s.nextIndex < len(s.queue) && len(page) < s.cfg.PageSize {
		candidate := s.queue[s.nextIndex]
		s.nextIndex++
		login := normalizeLogin(candidate.Login)
		if login == "" || s.shoutedThisRun[login] {
			continue
		}
		page = append(page, candidate)
	}
	remaining := 0
	for _, candidate := range s.queue[s.nextIndex:] {
		login := normalizeLogin(candidate.Login)
		if login != "" && !s.shoutedThisRun[login] {
			remaining++
		}
	}
	s.mu.Unlock()

	if len(page) == 0 {
		_ = s.chat.Say(channel, "No unshouted streamers are queued right now.")
		return
	}
	s.dispatchShoutouts(ctx, channel, page, fmt.Sprintf("Shouting out %%d streamer(s). %d left in queue.", remaining))
}

func (s *Service) sendRoulette(ctx context.Context, channel string) {
	if err := s.syncStreamRun(ctx); err != nil {
		s.logger.Warn("failed to sync stream shoutout state", "error", err)
		_ = s.chat.Say(channel, "I could not verify stream shoutout state right now.")
		return
	}
	page := s.roulettePage(5)
	if len(page) == 0 {
		_ = s.chat.Say(channel, "No roulette shoutouts are available right now.")
		return
	}
	s.dispatchShoutouts(ctx, channel, page, "Roulette picked %d streamer(s).")
}

func (s *Service) dispatchShoutouts(ctx context.Context, channel string, page []Candidate, summaryFormat string) {
	s.mu.Lock()
	if s.dispatchRunning {
		s.mu.Unlock()
		_ = s.chat.Say(channel, "Streamer shoutouts are already running.")
		return
	}
	var filtered []Candidate
	for _, candidate := range page {
		if candidate.Login == "" || s.shoutedThisRun[normalizeLogin(candidate.Login)] {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		s.mu.Unlock()
		_ = s.chat.Say(channel, "No unshouted streamers are queued right now.")
		return
	}
	s.dispatchRunning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.dispatchRunning = false
		s.mu.Unlock()
	}()

	_ = s.chat.Say(channel, fmt.Sprintf(summaryFormat, len(filtered)))
	for i, candidate := range filtered {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if i > 0 && s.cfg.ShoutoutDelay > 0 {
			timer := time.NewTimer(s.cfg.ShoutoutDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		login := normalizeLogin(candidate.Login)
		if err := s.chat.Say(channel, "!so @"+login); err != nil {
			s.logger.Warn("failed to send streamer shoutout", "login", login, "error", err)
			continue
		}
		s.markShouted(login)
	}
}

func (s *Service) roulettePage(limit int) []Candidate {
	if limit <= 0 {
		limit = 5
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pool := make([]string, 0, len(s.cfg.RouletteStreamers))
	for _, login := range s.cfg.RouletteStreamers {
		login = normalizeLogin(login)
		if login == "" || s.isChannelOwner(login) || s.shoutedThisRun[login] {
			continue
		}
		pool = append(pool, login)
	}
	if len(pool) == 0 {
		return nil
	}
	s.rng.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})
	if len(pool) > limit {
		pool = pool[:limit]
	}
	out := make([]Candidate, 0, len(pool))
	for _, login := range pool {
		out = append(out, Candidate{Login: login, DisplayName: login})
	}
	return out
}

func (s *Service) syncStreamRun(ctx context.Context) error {
	if s.helix == nil || s.cfg.Channel == "" {
		return nil
	}
	info, err := s.helix.GetStreamInfo(ctx, s.cfg.Channel)
	if err != nil {
		return err
	}
	key := "offline"
	if info.Live && !info.StartedAt.IsZero() {
		key = info.StartedAt.UTC().Format(time.RFC3339)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamKey == "" {
		s.streamKey = key
		return nil
	}
	if s.streamKey != key {
		s.streamKey = key
		s.shoutedThisRun = map[string]bool{}
		s.queue = nil
		s.nextIndex = 0
	}
	return nil
}

func (s *Service) pollChatters(ctx context.Context) {
	s.mu.Lock()
	disabled := s.pollDisabled
	s.mu.Unlock()
	if disabled {
		return
	}

	chatters, err := s.helix.GetChatters(ctx, s.cfg.BroadcasterID, s.cfg.ModeratorID)
	if err != nil {
		s.mu.Lock()
		if !s.pollWarned {
			s.logger.Warn("recent streamer chatter polling failed; falling back to message-only watch tracking", "error", err)
			s.pollWarned = true
		}
		s.pollDisabled = true
		s.mu.Unlock()
		return
	}

	viewers := make([]ViewerIdentity, 0, len(chatters))
	for _, chatter := range chatters {
		display := chatter.UserName
		if display == "" {
			display = chatter.UserLogin
		}
		viewers = append(viewers, ViewerIdentity{Login: chatter.UserLogin, DisplayName: display})
	}
	s.ApplySnapshot(time.Now(), viewers)
	s.logger.Info("recent streamer chatter snapshot applied", "chatters", len(viewers))
}

func (s *Service) viewerCandidates() []viewerState {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := make([]viewerState, 0, len(s.viewers))
	for _, viewer := range s.viewers {
		if s.isChannelOwner(viewer.Login) {
			continue
		}
		if viewer.Watch >= s.cfg.MinWatch {
			candidates = append(candidates, *viewer)
		}
	}
	return candidates
}

func (s *Service) getUsers(ctx context.Context, now time.Time, logins []string) ([]twitch.UserInfo, error) {
	seen := map[string]bool{}
	var users []twitch.UserInfo
	var missing []string

	s.mu.Lock()
	for _, login := range logins {
		login = normalizeLogin(login)
		if login == "" || seen[login] {
			continue
		}
		seen[login] = true
		if item, ok := s.userCache[login]; ok && now.Before(item.expiresAt) {
			users = append(users, item.user)
			continue
		}
		missing = append(missing, login)
	}
	s.mu.Unlock()

	for start := 0; start < len(missing); start += 100 {
		end := start + 100
		if end > len(missing) {
			end = len(missing)
		}
		fetched, err := s.helix.GetUsersByLogin(ctx, missing[start:end])
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		for _, user := range fetched {
			login := normalizeLogin(user.Login)
			s.userCache[login] = cachedUser{user: user, expiresAt: now.Add(s.cfg.CacheTTL)}
			users = append(users, user)
		}
		s.mu.Unlock()
	}

	return users, nil
}

func (s *Service) getRecentStream(ctx context.Context, now time.Time, userID string) (time.Time, bool, error) {
	s.mu.Lock()
	if item, ok := s.recentCache[userID]; ok && now.Before(item.expiresAt) {
		s.mu.Unlock()
		return item.streamedAt, item.ok, nil
	}
	s.mu.Unlock()

	streamedAt, ok, err := s.helix.GetRecentStream(ctx, userID)
	if err != nil {
		return time.Time{}, false, err
	}

	s.mu.Lock()
	s.recentCache[userID] = cachedRecent{streamedAt: streamedAt, ok: ok, expiresAt: now.Add(s.cfg.CacheTTL)}
	s.mu.Unlock()
	return streamedAt, ok, nil
}

func (s *Service) isFollower(ctx context.Context, now time.Time, userID string) (bool, error) {
	s.mu.Lock()
	if item, ok := s.followerCache[userID]; ok && now.Before(item.expiresAt) {
		s.mu.Unlock()
		return item.follows, nil
	}
	s.mu.Unlock()

	follows, err := s.helix.IsChannelFollower(ctx, s.cfg.BroadcasterID, userID)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	s.followerCache[userID] = cachedFollower{follows: follows, expiresAt: now.Add(s.cfg.CacheTTL)}
	s.mu.Unlock()
	return follows, nil
}

func (s *Service) ensureViewer(login, displayName string) *viewerState {
	login = normalizeLogin(login)
	viewer, ok := s.viewers[login]
	if !ok {
		viewer = &viewerState{Login: login, DisplayName: displayName}
		s.viewers[login] = viewer
	}
	if displayName != "" {
		viewer.DisplayName = displayName
	}
	return viewer
}

func (s *Service) alreadyShouted(login string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shoutedThisRun[normalizeLogin(login)]
}

func (s *Service) markShouted(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shoutedThisRun[normalizeLogin(login)] = true
}

func (s *Service) isChannelOwner(login string) bool {
	return normalizeLogin(login) == normalizeLogin(s.cfg.Channel)
}

func normalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(login, "@")))
}

func normalizeLoginList(logins []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, login := range logins {
		login = normalizeLogin(login)
		if login == "" || seen[login] {
			continue
		}
		seen[login] = true
		out = append(out, login)
	}
	return out
}

func permissionAllows(permission string, msg twitch.Message) bool {
	switch normalizePermission(permission, "everyone") {
	case "everyone":
		return true
	case "mods":
		return msg.IsBroadcaster || msg.IsMod || strings.EqualFold(msg.Username, msg.Channel)
	case "broadcaster":
		return msg.IsBroadcaster || strings.EqualFold(msg.Username, msg.Channel)
	default:
		return true
	}
}

func normalizePermission(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "everyone":
		return "everyone"
	case "mods", "mod":
		return "mods"
	case "broadcaster", "streamer", "owner":
		return "broadcaster"
	default:
		return fallback
	}
}

func permissionDeniedMessage(permission, subject string) string {
	switch normalizePermission(permission, "everyone") {
	case "mods":
		return fmt.Sprintf("Only mods or the broadcaster can run %s.", subject)
	case "broadcaster":
		return fmt.Sprintf("Only the broadcaster can run %s.", subject)
	default:
		return ""
	}
}

func roundDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}
