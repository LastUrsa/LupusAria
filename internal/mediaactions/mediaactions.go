package mediaactions

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/gif"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	TriggerChannelPointRedeem = "channel_point_redeem"
	AssetKindMedia            = "media"
	AssetKindSound            = "sound"
	PlaybackNormal            = "normal"
	PlaybackMatchAudio        = "match_audio"
	PlaybackLoop              = "loop"
	PlaybackLoopNext          = "loop_next"
)

type Action struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Enabled           bool    `json:"enabled"`
	Trigger           string  `json:"trigger"`
	RewardID          string  `json:"rewardId"`
	RewardTitle       string  `json:"rewardTitle"`
	Media             []Asset `json:"media"`
	Sounds            []Asset `json:"sounds"`
	Duration          int     `json:"duration"`
	Position          string  `json:"position"`
	Scale             int     `json:"scale"`
	Animation         string  `json:"animation"`
	MediaPlaybackMode string  `json:"mediaPlaybackMode"`
}

type Asset struct {
	ID                     string `json:"id"`
	Filename               string `json:"filename"`
	Path                   string `json:"path"`
	DurationMS             int    `json:"durationMs"`
	MediaPlaybackMode      string `json:"mediaPlaybackMode"`
	ExcludeFromGifRotation bool   `json:"excludeFromGifRotation"`
}

type Playback struct {
	ActionID          string  `json:"actionId"`
	Name              string  `json:"name"`
	Media             *Asset  `json:"media,omitempty"`
	MediaSequence     []Asset `json:"mediaSequence,omitempty"`
	Sound             *Asset  `json:"sound,omitempty"`
	Duration          int     `json:"duration"`
	Position          string  `json:"position"`
	Scale             int     `json:"scale"`
	Animation         string  `json:"animation"`
	MediaPlaybackMode string  `json:"mediaPlaybackMode"`
}

var slugPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func Load(path string) ([]Action, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Action{}, nil
	}
	if err != nil {
		return nil, err
	}
	var actions []Action
	if err := json.Unmarshal(data, &actions); err != nil {
		return nil, err
	}
	for i := range actions {
		actions[i] = Normalize(actions[i])
	}
	return actions, nil
}

func Save(path string, actions []Action) error {
	if err := ValidateAll(actions); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	normalized := make([]Action, 0, len(actions))
	for _, action := range actions {
		normalized = append(normalized, Normalize(action))
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func ImportAssets(root string, action Action, kind string, paths []string) ([]Asset, error) {
	if kind != AssetKindMedia && kind != AssetKindSound {
		return nil, fmt.Errorf("unsupported asset kind %q", kind)
	}
	if len(paths) == 0 {
		return []Asset{}, nil
	}
	dir := filepath.Join(root, actionFolder(action), folderForKind(kind))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	imported := make([]Asset, 0, len(paths))
	for _, source := range paths {
		if !isSupported(kind, source) {
			return nil, fmt.Errorf("%s is not a supported %s file", filepath.Base(source), kind)
		}
		target := uniquePath(dir, filepath.Base(source))
		if err := copyFile(target, source); err != nil {
			return nil, err
		}
		imported = append(imported, Asset{
			ID:         newID(),
			Filename:   filepath.Base(target),
			Path:       target,
			DurationMS: mediaDurationMS(kind, target),
		})
	}
	return imported, nil
}

func SelectPlayback(action Action, rng *rand.Rand) (Playback, bool) {
	action = Normalize(action)
	if !action.Enabled || len(action.Media)+len(action.Sounds) == 0 {
		return Playback{}, false
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	playback := Playback{
		ActionID:  action.ID,
		Name:      action.Name,
		Duration:  action.Duration,
		Position:  action.Position,
		Scale:     action.Scale,
		Animation: action.Animation,
	}
	if len(action.Media) > 0 {
		asset := action.Media[rng.Intn(len(action.Media))]
		playback.Media = &asset
		playback.MediaPlaybackMode = asset.MediaPlaybackMode
		if asset.MediaPlaybackMode == PlaybackLoopNext {
			playback.MediaSequence = selectMediaSequence(action.Media, asset, action.Duration, rng)
		}
	}
	if len(action.Sounds) > 0 {
		asset := action.Sounds[rng.Intn(len(action.Sounds))]
		playback.Sound = &asset
	}
	return playback, true
}

func Normalize(action Action) Action {
	action.ID = strings.TrimSpace(action.ID)
	if action.ID == "" {
		action.ID = newID()
	}
	action.Name = strings.TrimSpace(action.Name)
	action.Trigger = strings.TrimSpace(action.Trigger)
	if action.Trigger == "" {
		action.Trigger = TriggerChannelPointRedeem
	}
	action.RewardID = strings.TrimSpace(action.RewardID)
	action.RewardTitle = strings.TrimSpace(action.RewardTitle)
	if action.Duration == 0 {
		action.Duration = 5
	}
	if action.Duration < 1 {
		action.Duration = 1
	}
	if action.Duration > 60 {
		action.Duration = 60
	}
	if action.Scale == 0 {
		action.Scale = 100
	}
	if action.Scale < 25 {
		action.Scale = 25
	}
	if action.Scale > 200 {
		action.Scale = 200
	}
	if !slices.Contains([]string{"center", "top-left", "top-right", "bottom-left", "bottom-right"}, action.Position) {
		action.Position = "center"
	}
	if !slices.Contains([]string{"none", "fade-in", "fade-out", "fade-in-out"}, action.Animation) {
		action.Animation = "fade-in-out"
	}
	actionMediaPlaybackMode := action.MediaPlaybackMode
	if !isPlaybackMode(actionMediaPlaybackMode) {
		actionMediaPlaybackMode = PlaybackNormal
	}
	action.MediaPlaybackMode = actionMediaPlaybackMode
	action.Media = normalizeAssets(action.Media, actionMediaPlaybackMode)
	action.Sounds = normalizeAssets(action.Sounds, PlaybackNormal)
	return action
}

func ValidateAll(actions []Action) error {
	assignedRewards := map[string]string{}
	for _, raw := range actions {
		action := Normalize(raw)
		if action.Name == "" {
			return errors.New("media action name is required")
		}
		if action.Trigger != TriggerChannelPointRedeem {
			return fmt.Errorf("%s uses an unsupported trigger", action.Name)
		}
		if action.RewardID == "" {
			return fmt.Errorf("%s needs a channel point redeem", action.Name)
		}
		if len(action.Media)+len(action.Sounds) == 0 {
			return fmt.Errorf("%s needs at least one media or sound asset", action.Name)
		}
		if existing := assignedRewards[action.RewardID]; existing != "" && existing != action.ID {
			return fmt.Errorf("channel point redeem is already assigned")
		}
		assignedRewards[action.RewardID] = action.ID
	}
	return nil
}

func IsAssetUnderRoot(root, assetPath string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	assetAbs, err := filepath.Abs(assetPath)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, assetAbs)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}

func SupportedExtensions(kind string) []string {
	if kind == AssetKindSound {
		return []string{".wav", ".mp3", ".ogg", ".flac"}
	}
	return []string{".gif", ".png", ".jpg", ".jpeg", ".webp"}
}

func normalizeAssets(assets []Asset, defaultPlaybackMode string) []Asset {
	out := make([]Asset, 0, len(assets))
	if !isPlaybackMode(defaultPlaybackMode) {
		defaultPlaybackMode = PlaybackNormal
	}
	for _, asset := range assets {
		asset.ID = strings.TrimSpace(asset.ID)
		if asset.ID == "" {
			asset.ID = newID()
		}
		asset.Filename = strings.TrimSpace(asset.Filename)
		asset.Path = strings.TrimSpace(asset.Path)
		if asset.Filename == "" && asset.Path != "" {
			asset.Filename = filepath.Base(asset.Path)
		}
		if asset.DurationMS < 0 {
			asset.DurationMS = 0
		}
		asset.MediaPlaybackMode = strings.TrimSpace(asset.MediaPlaybackMode)
		if !isPlaybackMode(asset.MediaPlaybackMode) {
			asset.MediaPlaybackMode = defaultPlaybackMode
		}
		if asset.Path != "" {
			out = append(out, asset)
		}
	}
	return out
}

func isPlaybackMode(mode string) bool {
	return slices.Contains([]string{PlaybackNormal, PlaybackMatchAudio, PlaybackLoop, PlaybackLoopNext}, mode)
}

func selectMediaSequence(media []Asset, first Asset, durationSeconds int, rng *rand.Rand) []Asset {
	gifs := make([]Asset, 0, len(media))
	for _, asset := range media {
		if !asset.ExcludeFromGifRotation && (strings.EqualFold(filepath.Ext(asset.Path), ".gif") || strings.EqualFold(filepath.Ext(asset.Filename), ".gif")) {
			gifs = append(gifs, asset)
		}
	}
	if len(gifs) == 0 {
		return []Asset{first}
	}
	targetMS := durationSeconds * 1000
	if targetMS <= 0 {
		targetMS = 5000
	}
	sequence := []Asset{first}
	total := assetDurationForSequence(first)
	pool := shuffledMediaPool(gifs, first, rng)
	for total < targetMS && len(sequence) < 120 {
		if len(pool) == 0 {
			pool = shuffledMediaPool(gifs, sequence[len(sequence)-1], rng)
		}
		next := pool[0]
		pool = pool[1:]
		sequence = append(sequence, next)
		total += assetDurationForSequence(next)
	}
	return sequence
}

func shuffledMediaPool(media []Asset, previous Asset, rng *rand.Rand) []Asset {
	pool := make([]Asset, 0, len(media))
	for _, asset := range media {
		if sameAsset(asset, previous) && len(media) > 1 {
			continue
		}
		pool = append(pool, asset)
	}
	if len(pool) == 0 && len(media) > 0 {
		pool = append(pool, media...)
	}
	rng.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})
	return pool
}

func sameAsset(a Asset, b Asset) bool {
	if a.ID != "" && b.ID != "" {
		return a.ID == b.ID
	}
	return a.Path != "" && a.Path == b.Path
}

func assetDurationForSequence(asset Asset) int {
	if asset.DurationMS > 0 {
		return asset.DurationMS
	}
	return 1000
}

func folderForKind(kind string) string {
	if kind == AssetKindSound {
		return "Sounds"
	}
	return "Media"
}

func actionFolder(action Action) string {
	base := strings.TrimSpace(action.Name)
	if base == "" {
		base = action.ID
	}
	base = strings.Trim(slugPattern.ReplaceAllString(base, "-"), "-._")
	if base == "" {
		base = "MediaAction"
	}
	return base
}

func isSupported(kind, path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(SupportedExtensions(kind), ext)
}

func mediaDurationMS(kind, path string) int {
	if kind != AssetKindMedia || strings.ToLower(filepath.Ext(path)) != ".gif" {
		return 0
	}
	duration, err := GIFDurationMS(path)
	if err != nil {
		return 0
	}
	return duration
}

func GIFDurationMS(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	decoded, err := gif.DecodeAll(file)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, delay := range decoded.Delay {
		if delay <= 0 {
			delay = 10
		}
		total += delay * 10
	}
	return total, nil
}

func uniquePath(dir, filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "asset"
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	candidate := filepath.Join(dir, name)
	for index := 2; ; index++ {
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, index, ext))
	}
}

func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
