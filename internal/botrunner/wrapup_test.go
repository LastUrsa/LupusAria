package botrunner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"lupusaria/internal/twitch"
	"lupusaria/internal/wrapup"
)

type wrapUpStreamProvider struct {
	info twitch.StreamInfo
	err  error
}

func (p *wrapUpStreamProvider) GetStreamInfo(context.Context, string) (twitch.StreamInfo, error) {
	return p.info, p.err
}

func TestReconcileWrapUpRecoversMissedLifecycleEvents(t *testing.T) {
	service, err := wrapup.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	provider := &wrapUpStreamProvider{}
	reconcile := func() { reconcileWrapUp(context.Background(), provider, service, "channel", "broadcaster", logger) }
	reconcile() // Bot started before stream.
	if _, ok := service.Current(); ok {
		t.Fatal("offline startup created a session")
	}
	provider.info = twitch.StreamInfo{Live: true, ID: "stream", StartedAt: time.Now().Add(-time.Hour)}
	reconcile() // Online event was missed.
	if err := service.Record(wrapup.Event{MessageID: "follow", Type: "follow", UserID: "viewer", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	reconcile()
	report, ok := service.Current()
	if !ok || report.Status != "live" || report.StreamID != "stream" || report.FollowerCount != 1 {
		t.Fatalf("report = %#v", report)
	}
	provider.err = errors.New("temporary lookup failure")
	reconcile()
	report, _ = service.Current()
	if report.Status != "live" {
		t.Fatal("lookup failure finalized session")
	}
	provider.err = nil
	provider.info = twitch.StreamInfo{}
	reconcile() // Offline event was missed.
	report, _ = service.Current()
	if report.Status != "complete" || report.FollowerCount != 1 {
		t.Fatalf("report = %#v", report)
	}
}
