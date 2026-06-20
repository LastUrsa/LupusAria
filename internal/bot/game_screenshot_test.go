package bot

import (
	"bytes"
	"context"
	"image"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lupusaria/internal/twitch"
)

func TestGameScreenshotFixtureFeedsCroppedVisualContextToGameAnswer(t *testing.T) {
	imageBytes, err := os.ReadFile(filepath.Join("testdata", "slay_spire_ironclad_card_pick.png"))
	if err != nil {
		t.Fatalf("read screenshot fixture: %v", err)
	}
	original, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		t.Fatalf("decode screenshot fixture: %v", err)
	}

	cfg := testConfig()
	cfg.Channel = "lastursa"
	cfg.GlobalCooldown = 0
	cfg.UserCooldown = 0
	cfg.SnapshotCrop = SnapshotCrop{Enabled: true, X: 0.255, Y: 0.085, Width: 0.73, Height: 0.73}

	gameAI := &fakeGameAI{
		imageText:  "Visible Slay the Spire card reward screen with three choices and the Ironclad UI visible.",
		searchText: "Use the visible reward screen and damage goal to choose the best offensive option.",
	}
	chat := &fakeChat{}
	b := New(cfg, chat, gameAI, fakeStreamProvider{info: twitch.StreamInfo{
		Channel:     "lastursa",
		Title:       "Slaaaaaay The Spire 2 w/@the_polar_pop",
		GameName:    "Slay the Spire 2",
		ViewerCount: 16,
		Live:        true,
	}}, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b.thumb = &fakeThumbnailFetcher{image: imageBytes, mime: "image/png"}

	b.handleGameCommand(context.Background(), twitch.Message{
		Channel:     "lastursa",
		Username:    "lastursa",
		DisplayName: "LastUrsa",
	}, "which card should I pick if I want to maximize damage as Ironclad")

	if len(gameAI.imageBytes) != 1 {
		t.Fatalf("image analysis calls = %d, want 1", len(gameAI.imageBytes))
	}
	cropped, _, err := image.Decode(bytes.NewReader(gameAI.imageBytes[0]))
	if err != nil {
		t.Fatalf("decode image sent to AI: %v", err)
	}
	if cropped.Bounds().Dx() >= original.Bounds().Dx() || cropped.Bounds().Dy() >= original.Bounds().Dy() {
		t.Fatalf("snapshot was not cropped before analysis: original=%v analyzed=%v", original.Bounds(), cropped.Bounds())
	}
	if gameAI.imageMIMEs[0] != "image/jpeg" {
		t.Fatalf("image analysis MIME = %q, want image/jpeg", gameAI.imageMIMEs[0])
	}
	if len(gameAI.searchPrompts) != 1 {
		t.Fatalf("search prompts = %#v, want one prompt", gameAI.searchPrompts)
	}
	for _, want := range []string{
		`Current Twitch category/title context: "Slaaaaaay The Spire 2 w/@the_polar_pop"`,
		`Stream snapshot: "Visible Slay the Spire card reward screen`,
		`which card should I pick if I want to maximize damage as Ironclad`,
		`Prioritize the visible snapshot`,
	} {
		if !strings.Contains(gameAI.searchPrompts[0], want) {
			t.Fatalf("search prompt missing %q:\n%s", want, gameAI.searchPrompts[0])
		}
	}
	if len(chat.sent) != 1 || !strings.Contains(chat.sent[0], "visible reward screen") {
		t.Fatalf("sent = %#v", chat.sent)
	}
}
