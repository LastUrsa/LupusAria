package bot

import (
	"context"
	"fmt"
	"sync"
	"time"

	"lupusaria/internal/twitch"
)

type StreamInfoProvider interface {
	GetStreamInfo(ctx context.Context, channel string) (twitch.StreamInfo, error)
}

type cachedStreamContext struct {
	provider StreamInfoProvider
	ttl      time.Duration

	mu        sync.Mutex
	lastFetch time.Time
	info      twitch.StreamInfo
	hasInfo   bool
}

func newCachedStreamContext(provider StreamInfoProvider, ttl time.Duration) *cachedStreamContext {
	return &cachedStreamContext{provider: provider, ttl: ttl}
}

func (c *cachedStreamContext) Get(ctx context.Context, channel string) (twitch.StreamInfo, bool, error) {
	if c == nil || c.provider == nil {
		return twitch.StreamInfo{}, false, nil
	}

	c.mu.Lock()
	if c.hasInfo && time.Since(c.lastFetch) < c.ttl {
		info := c.info
		c.mu.Unlock()
		return info, true, nil
	}
	c.mu.Unlock()

	info, err := c.provider.GetStreamInfo(ctx, channel)
	if err != nil {
		return twitch.StreamInfo{}, false, err
	}

	c.mu.Lock()
	c.info = info
	c.hasInfo = true
	c.lastFetch = time.Now()
	c.mu.Unlock()

	return info, true, nil
}

func formatStreamContext(info twitch.StreamInfo, ok bool) string {
	if !ok {
		return "Stream context: unavailable."
	}
	if !info.Live {
		return "Stream context: channel appears offline."
	}
	started := "unknown"
	if !info.StartedAt.IsZero() {
		started = info.StartedAt.Format(time.RFC3339)
	}
	return fmt.Sprintf("Stream context: live. Category: %s. Title: %s. Viewers: %d. Started at: %s.",
		valueOrUnknown(info.GameName),
		valueOrUnknown(info.Title),
		info.ViewerCount,
		started,
	)
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
