package rehearsal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lupusaria/internal/raffle"
)

// Only the installer smoke script opts into this fixture. It uses no Twitch
// credentials; the seed stage uses the same loopback simulator as the rehearsal.
func TestInstallerPersistenceFixture(t *testing.T) {
	dir := os.Getenv("LUPUSARIA_SMOKE_STATE_DIR")
	if dir == "" {
		t.Skip("installer fixture not requested")
	}
	stage := os.Getenv("LUPUSARIA_SMOKE_STAGE")
	path := filepath.Join(dir, ".lupusaria-raffle.sqlite")
	switch stage {
	case "seed":
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("seed requires a fresh isolated database")
		}
		s := newSimulator(t)
		s.configure("installer-broadcast", "")
		r := startRuntime(t, path, &testClock{at: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)})
		defer r.stop(t)
		waitConnected(t, s)
		s.command(t, "installer-entry", "InstallerViewer", "!rafadd", false)
		s.command(t, "installer-draw", "owner", "!rafpick", false)
		barrier(t, s, "installer-seed")
		v := snapshot(t, r)
		if len(v.Entries) != 1 || v.Winner == nil {
			t.Fatal("fixture not committed")
		}
	case "verify":
		repo, err := raffle.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer repo.Close()
		service, err := raffle.New(repo, "owner", raffle.Options{})
		if err != nil {
			t.Fatal(err)
		}
		v, err := service.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if v.StreamID != "installer-broadcast" || v.Generation != 1 || len(v.Entries) != 1 || v.Entries[0].Number != 1 || v.Entries[0].Name != "InstallerViewer" || v.Winner == nil || v.Winner.Name != "InstallerViewer" {
			t.Fatalf("installer changed committed state: %+v", v)
		}
	default:
		t.Fatal("fixture stage must be seed or verify")
	}
}
