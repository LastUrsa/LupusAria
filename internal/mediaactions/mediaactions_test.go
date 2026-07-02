package mediaactions

import (
	"image"
	"image/color"
	"image/gif"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAllRejectsDuplicateRedeems(t *testing.T) {
	actions := []Action{
		{
			ID:       "one",
			Name:     "One",
			Enabled:  true,
			Trigger:  TriggerChannelPointRedeem,
			RewardID: "reward-1",
			Media:    []Asset{{ID: "media-1", Filename: "one.gif", Path: filepath.Join(t.TempDir(), "one.gif")}},
		},
		{
			ID:       "two",
			Name:     "Two",
			Enabled:  true,
			Trigger:  TriggerChannelPointRedeem,
			RewardID: "reward-1",
			Sounds:   []Asset{{ID: "sound-1", Filename: "two.mp3", Path: filepath.Join(t.TempDir(), "two.mp3")}},
		},
	}

	if err := ValidateAll(actions); err == nil {
		t.Fatal("ValidateAll succeeded for duplicate redeem assignment")
	}
}

func TestSelectPlaybackAllowsSoundOnlyAction(t *testing.T) {
	action := Action{
		ID:       "sound",
		Name:     "Sound",
		Enabled:  true,
		Trigger:  TriggerChannelPointRedeem,
		RewardID: "reward-1",
		Sounds: []Asset{
			{ID: "sound-1", Filename: "one.mp3", Path: "/tmp/one.mp3"},
			{ID: "sound-2", Filename: "two.mp3", Path: "/tmp/two.mp3"},
		},
	}

	playback, ok := SelectPlayback(action, rand.New(rand.NewSource(1)))
	if !ok {
		t.Fatal("SelectPlayback returned false")
	}
	if playback.Media != nil {
		t.Fatalf("media = %#v, want nil", playback.Media)
	}
	if playback.Sound == nil {
		t.Fatal("sound = nil, want selected sound")
	}
	if playback.Duration != 5 || playback.Position != "center" || playback.Scale != 100 {
		t.Fatalf("defaults were not normalized: %#v", playback)
	}
}

func TestSelectPlaybackUsesSelectedMediaPlaybackMode(t *testing.T) {
	action := Action{
		ID:                "media",
		Name:              "Media",
		Enabled:           true,
		Trigger:           TriggerChannelPointRedeem,
		RewardID:          "reward-1",
		MediaPlaybackMode: PlaybackLoop,
		Media: []Asset{
			{ID: "media-1", Filename: "one.gif", Path: "/tmp/one.gif", MediaPlaybackMode: PlaybackMatchAudio},
		},
	}

	playback, ok := SelectPlayback(action, rand.New(rand.NewSource(1)))
	if !ok {
		t.Fatal("SelectPlayback returned false")
	}
	if playback.MediaPlaybackMode != PlaybackMatchAudio {
		t.Fatalf("playback mode = %q, want %q", playback.MediaPlaybackMode, PlaybackMatchAudio)
	}
	if playback.Media == nil || playback.Media.MediaPlaybackMode != PlaybackMatchAudio {
		t.Fatalf("media = %#v", playback.Media)
	}
}

func TestSelectPlaybackLoopNextBuildsSequenceToCoverDuration(t *testing.T) {
	action := Action{
		ID:       "media",
		Name:     "Media",
		Enabled:  true,
		Trigger:  TriggerChannelPointRedeem,
		RewardID: "reward-1",
		Duration: 3,
		Media: []Asset{
			{ID: "media-1", Filename: "one.gif", Path: "/tmp/one.gif", DurationMS: 700, MediaPlaybackMode: PlaybackLoopNext},
		},
	}

	playback, ok := SelectPlayback(action, rand.New(rand.NewSource(1)))
	if !ok {
		t.Fatal("SelectPlayback returned false")
	}
	if playback.MediaPlaybackMode != PlaybackLoopNext {
		t.Fatalf("playback mode = %q, want %q", playback.MediaPlaybackMode, PlaybackLoopNext)
	}
	total := 0
	for _, asset := range playback.MediaSequence {
		total += asset.DurationMS
	}
	if total < 3000 {
		t.Fatalf("sequence duration = %d, want at least 3000; sequence = %#v", total, playback.MediaSequence)
	}
}

func TestSelectMediaSequenceExcludesDisabledRotationGIFs(t *testing.T) {
	media := []Asset{
		{ID: "media-1", Filename: "starter.gif", Path: "/tmp/starter.gif", DurationMS: 700, MediaPlaybackMode: PlaybackLoopNext, ExcludeFromGifRotation: true},
		{ID: "media-2", Filename: "short.gif", Path: "/tmp/short.gif", DurationMS: 700},
		{ID: "media-3", Filename: "long.gif", Path: "/tmp/long.gif", DurationMS: 700, ExcludeFromGifRotation: true},
	}

	sequence := selectMediaSequence(media, media[0], 5, rand.New(rand.NewSource(1)))
	for i, asset := range sequence {
		if i == 0 {
			continue
		}
		if asset.ID != "media-2" {
			t.Fatalf("sequence[%d] = %s, want only media-2 after first; sequence = %#v", i, asset.ID, sequence)
		}
	}
}

func TestSelectMediaSequenceUsesGIFsBeforeRepeating(t *testing.T) {
	media := []Asset{
		{ID: "media-1", Filename: "starter.gif", Path: "/tmp/starter.gif", DurationMS: 500, MediaPlaybackMode: PlaybackLoopNext},
		{ID: "media-2", Filename: "two.gif", Path: "/tmp/two.gif", DurationMS: 500},
		{ID: "media-3", Filename: "three.gif", Path: "/tmp/three.gif", DurationMS: 500},
		{ID: "media-4", Filename: "four.gif", Path: "/tmp/four.gif", DurationMS: 500},
	}

	sequence := selectMediaSequence(media, media[0], 2, rand.New(rand.NewSource(1)))
	seen := map[string]bool{}
	for i, asset := range sequence[:4] {
		if seen[asset.ID] {
			t.Fatalf("sequence repeated %s before using all GIFs: %#v", asset.ID, sequence)
		}
		seen[asset.ID] = true
		if i > 0 && asset.ID == sequence[i-1].ID {
			t.Fatalf("sequence repeated adjacent GIF %s: %#v", asset.ID, sequence)
		}
	}
}

func TestGIFDurationMS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alert.gif")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = gif.EncodeAll(file, &gif.GIF{
		Image: []*image.Paletted{
			image.NewPaletted(image.Rect(0, 0, 2, 2), []color.Color{color.Black, color.White}),
			image.NewPaletted(image.Rect(0, 0, 2, 2), []color.Color{color.Black, color.White}),
		},
		Delay: []int{25, 50},
	})
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	duration, err := GIFDurationMS(path)
	if err != nil {
		t.Fatal(err)
	}
	if duration != 750 {
		t.Fatalf("duration = %d, want 750", duration)
	}
}
