// Package raffle owns durable raffle state; frontend state is never authoritative.
package raffle

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Repository struct{ db *sql.DB }

func Open(path string) (*Repository, error) {
	if path == "" {
		return nil, errors.New("raffle database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	options := url.Values{"_pragma": []string{"foreign_keys(ON)", "busy_timeout(5000)", "synchronous(FULL)", "secure_delete(ON)"}}
	dsn := url.URL{Scheme: "file", Path: uriPath, RawQuery: options.Encode()}
	// Connection-local safety settings must also apply if database/sql replaces
	// a connection after cancellation; initializing just the first one is insufficient.
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Repository, error) { db.Close(); return nil, e }
	// DELETE journaling avoids persistent WAL copies of participant data.
	if _, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA secure_delete=ON;`); err != nil {
		return fail(err)
	}
	var exists int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='schema_version'`).Scan(&exists); err != nil {
		return fail(err)
	}
	if exists != 0 {
		var v int
		if err = db.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
			return fail(err)
		}
		if v != 1 {
			return fail(fmt.Errorf("unsupported raffle schema version %d", v))
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return fail(err)
	}
	if _, err = tx.Exec(schema); err != nil {
		tx.Rollback()
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return &Repository{db: db}, nil
}
func (r *Repository) Close() error { return r.db.Close() }

type session struct {
	ID                         int64
	Broadcaster, Stream, State string
	Generation, Next           int
	First, Last, Checked       sql.NullInt64
}

func current(tx *sql.Tx, broadcaster string) (session, error) {
	var s session
	err := tx.QueryRow(`SELECT id,broadcaster,stream,state,generation,next_number,first_offline,last_offline,last_checked FROM sessions WHERE broadcaster=? AND state!='CLOSED'`, broadcaster).Scan(&s.ID, &s.Broadcaster, &s.Stream, &s.State, &s.Generation, &s.Next, &s.First, &s.Last, &s.Checked)
	if errors.Is(err, sql.ErrNoRows) {
		return session{}, nil
	}
	return s, err
}
func purge(tx *sql.Tx, id int64) error {
	for _, table := range []string{"entries", "draws", "commands"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE session=?", id); err != nil {
			return err
		}
	}
	return nil
}

// SavedBroadcaster allows the desktop to display recoverable state before the bot starts.
func (r *Repository) SavedBroadcaster() (string, error) {
	var id string
	err := r.db.QueryRow(`SELECT broadcaster FROM sessions WHERE state!='CLOSED' LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
