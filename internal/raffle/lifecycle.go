package raffle

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"lupusaria/internal/twitch"
)

type StreamProvider interface {
	GetStreamInfo(context.Context, string) (twitch.StreamInfo, error)
}
type Trigger = twitch.StreamSignal

// Timer permits controlled scheduling in lifecycle tests.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}
type systemTimer struct{ *time.Timer }

func (t systemTimer) C() <-chan time.Time { return t.Timer.C }

// Reconcile holds the same gate as commands, including the network request. This
// gives every observation and command a single order; old responses cannot win races.
func (s *Service) Reconcile(ctx context.Context, p StreamProvider, channel string, startup bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	committed := false
	defer func() {
		if !committed {
			s.ready = false
			s.operationalError = "Stream transition failed; commands suspended until verification"
			s.publishLocked()
		}
	}()
	nextReady := s.ready
	queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, lookupErr := p.GetStreamInfo(queryCtx, channel)
	if lookupErr == nil && info.Live && (info.ID == "" || !strings.EqualFold(info.Channel, channel)) {
		lookupErr = errors.New("invalid live stream identity")
	}
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	v, err := current(tx, s.broadcaster)
	if err != nil {
		return err
	}
	now := s.now().UTC().UnixNano()
	if lookupErr != nil {
		if startup {
			nextReady = false
			if v.ID != 0 && v.State != "END_PENDING" {
				if _, err = tx.Exec(`UPDATE sessions SET state='RECOVERING' WHERE id=?`, v.ID); err != nil {
					return err
				}
			}
		}
		s.verification = "Twitch verification unavailable; retrying"
		var httpErr *twitch.HelixError
		if errors.As(lookupErr, &httpErr) {
			s.retryAt = httpErr.RetryAt
		}
	} else {
		s.retryAt = time.Time{}
		s.verification = "Verified with Twitch"
		switch {
		case info.Live:
			var seenClosed int
			if err = tx.QueryRow(`SELECT count(*) FROM sessions WHERE broadcaster=? AND stream=? AND state='CLOSED'`, s.broadcaster, info.ID).Scan(&seenClosed); err != nil {
				return err
			}
			if seenClosed != 0 {
				nextReady = false
				s.verification = "Twitch reported a closed broadcast; awaiting reconciliation"
				break
			}
			if v.ID != 0 && v.Stream != info.ID {
				if err = closeSession(tx, v, now); err != nil {
					return err
				}
				v = session{}
			}
			if v.ID == 0 {
				var closed int
				if err = tx.QueryRow(`SELECT count(*) FROM sessions WHERE broadcaster=? AND stream=?`, s.broadcaster, info.ID).Scan(&closed); err != nil {
					return err
				}
				if closed != 0 {
					nextReady = false
					s.verification = "Twitch reported a closed broadcast; awaiting reconciliation"
					break
				}
				_, err = tx.Exec(`INSERT INTO sessions(broadcaster,stream,state,created,last_checked) VALUES(?,?,'ACTIVE',?,?)`, s.broadcaster, info.ID, now, now)
			} else {
				_, err = tx.Exec(`UPDATE sessions SET state='ACTIVE',first_offline=NULL,last_offline=NULL,last_checked=? WHERE id=?`, now, v.ID)
			}
			nextReady = true
		case v.ID != 0:
			nextReady = false
			if v.State == "END_PENDING" && v.First.Valid && now > v.First.Int64 && time.Duration(now-v.First.Int64) >= s.confirmation {
				// This request is a new successful observation, not an event or cached result.
				if _, err = tx.Exec(`UPDATE sessions SET last_offline=?,last_checked=? WHERE id=?`, now, now, v.ID); err != nil {
					return err
				}
				err = closeSession(tx, v, now)
			} else {
				_, err = tx.Exec(`UPDATE sessions SET state='END_PENDING',first_offline=COALESCE(first_offline,?),last_offline=?,last_checked=? WHERE id=?`, now, now, now, v.ID)
			}
		default:
			nextReady = false
		}
	}
	if err != nil {
		return err
	}
	if err = bump(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	s.ready = nextReady
	s.operationalError = ""
	s.publishLocked()
	return lookupErr
}
func closeSession(tx *sql.Tx, v session, now int64) error {
	if err := purge(tx, v.ID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE sessions SET state='CLOSED',closed=?,next_number=1 WHERE id=? AND state!='CLOSED'`, now, v.ID)
	return err
}
func (s *Service) acceptTrigger(t Trigger) (bool, error) {
	if t.Broadcaster != "" && t.Broadcaster != s.broadcaster {
		return false, nil
	}
	if t.ID == "" {
		return true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.repo.db.Exec(`INSERT OR IGNORE INTO notifications(id,received) VALUES(?,?)`, t.ID, s.now().UnixNano())
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}
func (s *Service) ReportError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operationalError = "Raffle operation failed; committed state preserved"
	s.repo.db.Exec(`UPDATE revision SET value=value+1`)
	s.publishLocked()
}

// Run is called after synchronous startup reconciliation and exits on bot shutdown.
func (s *Service) Run(ctx context.Context, p StreamProvider, channel string, triggers <-chan Trigger, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = time.Minute
	}
	delays := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}
	attempt := 0
	next := interval
	var retryAt time.Time
	s.mu.Lock()
	if strings.Contains(s.verification, "unavailable") {
		next = delays[0]
		attempt = 1
	}
	if wait := s.retryAt.Sub(s.now()); wait > next {
		next = wait
		retryAt = s.retryAt
	}
	s.mu.Unlock()
	timer := s.newTimer(next)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := s.Stop(); err != nil {
				report(err)
			}
			return
		case t := <-triggers:
			ok, err := s.acceptTrigger(t)
			if err != nil {
				report(err)
			}
			if !ok {
				continue
			}
		case <-timer.C():
		}
		if wait := retryAt.Sub(s.now()); wait > 0 {
			if !timer.Stop() {
				select {
				case <-timer.C():
				default:
				}
			}
			timer.Reset(wait)
			continue
		}
		err := s.Reconcile(ctx, p, channel, false)
		next = interval
		if err != nil {
			report(err)
			if attempt >= len(delays) {
				attempt = len(delays) - 1
			}
			next = delays[attempt]
			var httpErr *twitch.HelixError
			if errors.As(err, &httpErr) && httpErr.RetryAt.Sub(s.now()) > next {
				next = httpErr.RetryAt.Sub(s.now())
			}
			retryAt = s.now().Add(next)
			attempt++
		} else {
			attempt = 0
			retryAt = time.Time{}
		}
		if !timer.Stop() {
			select {
			case <-timer.C():
			default:
			}
		}
		timer.Reset(next)
	}
}
