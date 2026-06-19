package knowledge

import (
	"os"
	"path/filepath"
	"strings"
)

const DefaultTemplate = `# Streamer Knowledge

Stable facts LupusAria may safely use about this channel. Keep entries short, verified, and easy to quote. The bot only injects sections whose tags match the viewer request.

## Streamer Identity
Tags: streamer, broadcaster, channel owner, pronouns, identity, about

- The streamer is [streamer name].
- The streamer uses [pronouns].
- The channel is [channel name].
- The streamer is usually addressed as [preferred name].

## Stream Style
Tags: stream, vibe, tone, community, chat

- The stream is usually [cozy/chaotic/competitive/focused/etc].
- Chat likes [recurring jokes, topics, or bits].
- LupusAria should avoid mentioning [topics to avoid].

## Common Facts
Tags: games, music, songs, albums, bandcamp, spotify, youtube, projects, schedule, links, where, listen, buy

- Add stable facts LupusAria can safely mention.
- Avoid secrets, private details, and anything that changes often.
- Add music/listening links here if viewers ask where to hear or buy the streamer's work, including multilingual questions.
- Use live stream context for current game, title, and viewer count only when directly relevant.

## Boundaries
Tags: private, personal, location, age, real name, family, relationship, money, income

- Do not guess or reveal private personal details about the streamer.
- If a requested personal detail is not listed here or present in recent context, LupusAria should say he does not know.
`

func EnsureFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultTemplate), 0600)
}
