package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lupusaria/internal/ai"
	"lupusaria/internal/twitch"
)

const twitchEmoteCDNFormat = "https://static-cdn.jtvnw.net/emoticons/v2/%s/static/dark/3.0"

type emoteDescriptionCacheEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CachedAt    string `json:"cachedAt"`
}

type emoteDescriber struct {
	path     string
	analyzer ai.ImageAnalyzer
	logger   *slog.Logger
	client   *http.Client

	mu      sync.Mutex
	loaded  bool
	cache   map[string]emoteDescriptionCacheEntry
	catalog map[string]twitch.Emote
}

func newEmoteDescriber(path string, aiClient ai.Client, logger *slog.Logger, channelEmotes []twitch.Emote) *emoteDescriber {
	analyzer, _ := aiClient.(ai.ImageAnalyzer)
	d := &emoteDescriber{
		path:     strings.TrimSpace(path),
		analyzer: analyzer,
		logger:   logger,
		client:   &http.Client{Timeout: 10 * time.Second},
		cache:    map[string]emoteDescriptionCacheEntry{},
		catalog:  map[string]twitch.Emote{},
	}
	for _, emote := range channelEmotes {
		if emote.ID == "" || emote.Name == "" {
			continue
		}
		d.catalog[emote.Name] = twitch.Emote{ID: emote.ID, Name: emote.Name, Count: 1}
	}
	return d
}

func (d *emoteDescriber) Context(ctx context.Context, msg twitch.Message, describe func(context.Context, twitch.Emote, []byte, string) (string, bool)) string {
	if d == nil {
		return ""
	}
	emotes := d.MessageEmotes(msg)
	if len(emotes) == 0 {
		return ""
	}
	parts := make([]string, 0, len(emotes))
	for _, emote := range emotes {
		name := strings.TrimSpace(emote.Name)
		if name == "" {
			name = "emote " + emote.ID
		}
		description := d.cachedDescription(emote.ID)
		if description == "" && d.analyzer != nil && describe != nil {
			image, mimeType, err := d.fetchImage(ctx, emote.ID)
			if err != nil {
				if d.logger != nil {
					d.logger.Warn("failed to fetch twitch emote image", "emote_id", emote.ID, "emote_name", emote.Name, "error", err)
				}
			} else if text, ok := describe(ctx, emote, image, mimeType); ok {
				description = text
				d.store(emote, description)
			}
		}
		if description == "" {
			description = "custom Twitch emote; visual meaning unknown"
		}
		label := name
		if emote.Count > 1 {
			label = fmt.Sprintf("%s x%d", label, emote.Count)
		}
		parts = append(parts, fmt.Sprintf("%s = %s", label, description))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Emote context: " + strings.Join(parts, "; ")
}

func (d *emoteDescriber) MessageEmotes(msg twitch.Message) []twitch.Emote {
	if d == nil {
		return nil
	}
	byName := map[string]*twitch.Emote{}
	order := make([]string, 0, len(msg.Emotes))
	for _, emote := range msg.Emotes {
		if emote.Name == "" && emote.ID == "" {
			continue
		}
		name := emote.Name
		if name == "" {
			name = emote.ID
		}
		copy := emote
		if copy.Count <= 0 {
			copy.Count = 1
		}
		byName[name] = &copy
		order = append(order, name)
	}
	for _, token := range cleanedTextTokens(msg.Text) {
		emote, ok := d.catalog[token]
		if !ok {
			continue
		}
		if existing, exists := byName[token]; exists {
			if existing.Count <= 0 {
				existing.Count = 1
			}
			continue
		}
		copy := emote
		copy.Count = 1
		byName[token] = &copy
		order = append(order, token)
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]twitch.Emote, 0, len(order))
	seen := map[string]bool{}
	for _, name := range order {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, *byName[name])
	}
	return out
}

func (d *emoteDescriber) cachedDescription(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadLocked()
	return strings.TrimSpace(d.cache[id].Description)
}

func (d *emoteDescriber) store(emote twitch.Emote, description string) {
	description = cleanEmoteDescription(description)
	if emote.ID == "" || description == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadLocked()
	d.cache[emote.ID] = emoteDescriptionCacheEntry{
		ID:          emote.ID,
		Name:        emote.Name,
		Description: description,
		CachedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	d.saveLocked()
}

func (d *emoteDescriber) fetchImage(ctx context.Context, id string) ([]byte, string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, "", errors.New("empty emote id")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(twitchEmoteCDNFormat, id), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("twitch emote image request failed with status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 {
		return nil, "", errors.New("empty emote image")
	}
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mimeType == "" {
		mimeType = http.DetectContentType(body)
	}
	if index := strings.Index(mimeType, ";"); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = "image/png"
	}
	return body, mimeType, nil
}

func (d *emoteDescriber) loadLocked() {
	if d.loaded {
		return
	}
	d.loaded = true
	if d.path == "" {
		return
	}
	raw, err := os.ReadFile(d.path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		if d.logger != nil {
			d.logger.Warn("failed to read emote cache", "path", d.path, "error", err)
		}
		return
	}
	var entries []emoteDescriptionCacheEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		if d.logger != nil {
			d.logger.Warn("failed to parse emote cache", "path", d.path, "error", err)
		}
		return
	}
	for _, entry := range entries {
		if entry.ID != "" && strings.TrimSpace(entry.Description) != "" {
			d.cache[entry.ID] = entry
		}
	}
}

func (d *emoteDescriber) saveLocked() {
	if d.path == "" {
		return
	}
	entries := make([]emoteDescriptionCacheEntry, 0, len(d.cache))
	for _, entry := range d.cache {
		entries = append(entries, entry)
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(d.path), 0o700); err != nil {
		if d.logger != nil {
			d.logger.Warn("failed to create emote cache directory", "path", d.path, "error", err)
		}
		return
	}
	if err := os.WriteFile(d.path, raw, 0o600); err != nil {
		if d.logger != nil {
			d.logger.Warn("failed to write emote cache", "path", d.path, "error", err)
		}
	}
}

func cleanEmoteDescription(description string) string {
	description = strings.TrimSpace(description)
	description = strings.Trim(description, `"'`)
	description = strings.ReplaceAll(description, "\n", " ")
	description = strings.Join(strings.Fields(description), " ")
	description = strings.TrimRight(description, ".")
	if len(description) > 180 {
		description = smartTruncate(description, 180)
	}
	return strings.TrimSpace(description)
}

func formatPossibleEmoteContext(text string, known []twitch.Emote) string {
	tokens := possibleEmoteTokens(text, known)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		parts = append(parts, fmt.Sprintf("%s = possible custom/third-party emote or meme token; meaning unknown", token))
	}
	return "Possible emote tokens: " + strings.Join(parts, "; ")
}

func possibleEmoteTokens(text string, known []twitch.Emote) []string {
	knownNames := map[string]bool{}
	for _, emote := range known {
		if emote.Name != "" {
			knownNames[emote.Name] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, token := range cleanedTextTokens(text) {
		if token == "" || strings.HasPrefix(token, "@") || strings.HasPrefix(token, "!") || strings.Contains(token, "://") {
			continue
		}
		if knownNames[token] || seen[token] || !looksLikeCustomEmoteToken(token) {
			continue
		}
		seen[token] = true
		out = append(out, token)
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func cleanedTextTokens(text string) []string {
	fields := strings.Fields(text)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.Trim(field, " \t\r\n.,!?;:()[]{}<>\"'")
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func looksLikeCustomEmoteToken(token string) bool {
	runes := []rune(token)
	if len(runes) < 6 || len(runes) > 40 {
		return false
	}
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasLetter := false
	for _, r := range runes {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
			hasLetter = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	if !hasLetter {
		return false
	}
	return hasDigit && hasLower && hasUpper
}
