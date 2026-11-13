package raffle

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrStale = errors.New("raffle session or round changed")

type Entry struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}
type Winner struct {
	Number       int    `json:"number"`
	Name         string `json:"name"`
	DrawnAt      string `json:"drawnAt"`
	Announcement string `json:"announcement"`
}
type Snapshot struct {
	Revision     int64   `json:"revision"`
	SessionID    int64   `json:"sessionId"`
	StreamID     string  `json:"streamId"`
	Generation   int     `json:"generation"`
	Status       string  `json:"status"`
	Verification string  `json:"verification"`
	Entries      []Entry `json:"entries"`
	Winner       *Winner `json:"winner"`
	Error        string  `json:"error"`
}
type Fence struct {
	SessionID  int64
	Generation int
}
type Request struct {
	Command, MessageID, UserID, Login, Name string
	Fence                                   Fence
}
type Options struct {
	NewTimer     func(time.Duration) Timer
	Now          func() time.Time
	Random       func(int) (int, error)
	Changed      func(Snapshot)
	Confirmation time.Duration
}
type Service struct {
	newTimer                       func(time.Duration) Timer
	retryAt                        time.Time
	unavailableAt                  time.Time
	fence                          atomic.Value
	mu                             sync.Mutex
	repo                           *Repository
	broadcaster                    string
	now                            func() time.Time
	random                         func(int) (int, error)
	changed                        func(Snapshot)
	confirmation                   time.Duration
	ready                          bool
	verification, operationalError string
}

func SecureNumber(n int) (int, error) {
	if n <= 0 {
		return 0, errors.New("empty random range")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()) + 1, nil
}
func New(repo *Repository, broadcaster string, o Options) (*Service, error) {
	if broadcaster == "" {
		return nil, errors.New("missing broadcaster identity")
	}
	if o.NewTimer == nil {
		o.NewTimer = func(d time.Duration) Timer { return systemTimer{time.NewTimer(d)} }
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Random == nil {
		o.Random = SecureNumber
	}
	if o.Confirmation <= 0 {
		o.Confirmation = time.Minute
	}
	s := &Service{newTimer: o.NewTimer, repo: repo, broadcaster: broadcaster, now: o.Now, random: o.Random, changed: o.Changed, confirmation: o.Confirmation, verification: "Awaiting stream verification"}
	tx, err := repo.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE sessions SET state='RECOVERING' WHERE broadcaster=? AND state='ACTIVE'`, broadcaster); err != nil {
		return nil, err
	}
	// An interrupted network send has an unknown outcome. Never automatically replay it.
	if _, err = tx.Exec(`UPDATE draws SET announcement='failed' WHERE announcement='pending' AND session IN (SELECT id FROM sessions WHERE broadcaster=?)`, broadcaster); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`UPDATE revision SET value=value+1`); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.refreshFenceLocked()
	return s, nil
}
func (s *Service) Capture() (Fence, error) {
	v := s.fence.Load()
	if v == nil {
		return Fence{}, nil
	}
	return v.(Fence), nil
}
func (s *Service) refreshFenceLocked() {
	tx, err := s.repo.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	v, err := current(tx, s.broadcaster)
	if err == nil {
		s.fence.Store(Fence{v.ID, v.Generation})
	}
}
func (s *Service) Snapshot() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}
func (s *Service) snapshotLocked() (Snapshot, error) {
	out := Snapshot{Status: "INACTIVE", Entries: []Entry{}, Verification: s.verification, Error: s.operationalError}
	tx, err := s.repo.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow(`SELECT value FROM revision`).Scan(&out.Revision); err != nil {
		return out, err
	}
	v, err := current(tx, s.broadcaster)
	if err != nil {
		return out, err
	}
	if v.ID == 0 {
		return out, nil
	}
	out.SessionID = v.ID
	out.StreamID = v.Stream
	out.Generation = v.Generation
	out.Status = v.State
	rows, err := tx.Query(`SELECT number,name FROM entries WHERE session=? AND generation=? ORDER BY number`, v.ID, v.Generation)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.Number, &e.Name); err != nil {
			rows.Close()
			return out, err
		}
		out.Entries = append(out.Entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	var w Winner
	var at int64
	err = tx.QueryRow(`SELECT number,name,drawn,announcement FROM draws WHERE session=? AND generation=? ORDER BY id DESC LIMIT 1`, v.ID, v.Generation).Scan(&w.Number, &w.Name, &at, &w.Announcement)
	if err == nil {
		w.DrawnAt = time.Unix(0, at).UTC().Format(time.RFC3339)
		out.Winner = &w
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	return out, nil
}

// Callbacks receive complete snapshots after commit and must not call back into Service.
func (s *Service) publishLocked() {
	s.refreshFenceLocked()
	if s.changed != nil {
		if v, err := s.snapshotLocked(); err == nil {
			s.changed(v)
		}
	}
}
func bump(tx *sql.Tx) error { _, err := tx.Exec(`UPDATE revision SET value=value+1`); return err }

// Execute serializes send dispatch with clear/close, so invalidated payloads cannot
// begin sending after a destructive operation. A send already accepted by Twitch is final.
func (s *Service) Execute(ctx context.Context, r Request, send func(string) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Command != "!rafadd" && r.Command != "!rafpick" && r.Command != "!rafclear" {
		return errors.New("unknown raffle command")
	}
	if r.UserID == "" || r.MessageID == "" {
		return errors.New("raffle requires authenticated user and message IDs")
	}
	if r.Command != "!rafadd" && r.UserID != s.broadcaster {
		return send("Only the broadcaster can use " + r.Command + ".")
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
	if v.ID != r.Fence.SessionID || v.Generation != r.Fence.Generation {
		return ErrStale
	}
	if v.ID == 0 || (r.Command != "!rafclear" && (!s.ready || v.State != "ACTIVE")) {
		tx.Rollback()
		if !s.unavailableAt.IsZero() && s.now().Sub(s.unavailableAt) < 10*time.Second {
			return nil
		}
		s.unavailableAt = s.now()
		return send("Raffle is available when the stream is live and verified.")
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO commands(session,message_id,kind,processed) VALUES(?,?,?,?)`, v.ID, r.MessageID, r.Command, s.now().UnixNano())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	reply := ""
	var drawID int64
	switch r.Command {
	case "!rafadd":
		if strings.TrimSpace(r.Name) == "" {
			r.Name = r.Login
		}
		result, err = tx.Exec(`INSERT OR IGNORE INTO entries(session,generation,user_id,login,name,number,entered) VALUES(?,?,?,?,?,?,?)`, v.ID, v.Generation, r.UserID, r.Login, r.Name, v.Next, s.now().UnixNano())
		if err != nil {
			return err
		}
		n, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			if _, err = tx.Exec(`UPDATE sessions SET next_number=next_number+1 WHERE id=?`, v.ID); err != nil {
				return err
			}
			reply = r.Name + " added to raffle"
		}
	case "!rafclear":
		for _, table := range []string{"entries", "draws"} {
			if _, err = tx.Exec("DELETE FROM "+table+" WHERE session=?", v.ID); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(`UPDATE sessions SET generation=generation+1,next_number=1 WHERE id=?`, v.ID); err != nil {
			return err
		}
		reply = "Raffle cleared."
	case "!rafpick":
		var count int
		if err = tx.QueryRow(`SELECT count(*) FROM entries WHERE session=? AND generation=?`, v.ID, v.Generation).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			reply = "Raffle is empty. No winner can be selected."
			break
		}
		number, e := s.random(count)
		if e != nil {
			return e
		}
		if number < 1 || number > count {
			return errors.New("random result outside valid range")
		}
		var user, name string
		if err = tx.QueryRow(`SELECT user_id,name FROM entries WHERE session=? AND generation=? AND number=?`, v.ID, v.Generation, number).Scan(&user, &name); err != nil {
			return err
		}
		result, err = tx.Exec(`INSERT INTO draws(session,generation,message_id,number,user_id,name,count,drawn,announcement) VALUES(?,?,?,?,?,?,?,?,'pending')`, v.ID, v.Generation, r.MessageID, number, user, name, count, s.now().UnixNano())
		if err != nil {
			return err
		}
		drawID, err = result.LastInsertId()
		if err != nil {
			return err
		}
		reply = fmt.Sprintf("Congratulations %s you are the winner!", name)
	}
	if err = bump(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.operationalError = ""
	s.publishLocked()
	if reply == "" {
		return nil
	}
	err = send(reply)
	if drawID != 0 {
		status := "sent"
		if err != nil {
			status = "failed"
		}
		updateErr := s.setAnnouncement(drawID, status)
		if updateErr != nil {
			s.operationalError = "Could not persist announcement status"
			s.publishLocked()
			return updateErr
		}
		s.publishLocked()
	}
	return err
}
func (s *Service) setAnnouncement(id int64, status string) error {
	tx, err := s.repo.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE draws SET announcement=? WHERE id=?`, status, id); err != nil {
		return err
	}
	if err = bump(tx); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	s.verification = "Bot stopped; saved state awaiting verification"
	tx, err := s.repo.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE sessions SET state='RECOVERING' WHERE broadcaster=? AND state='ACTIVE'`, s.broadcaster); err != nil {
		return err
	}
	if err = bump(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.publishLocked()
	return nil
}
