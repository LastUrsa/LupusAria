package knowledge

import (
	"strings"
	"testing"
)

func TestParseSections(t *testing.T) {
	base := Parse(`# Ursa Knowledge

## Music
Tags: music, songs, genre
- Ursa makes moonlit synth rock.

## Projects
Tags: project, game
- Ursa is building LupusAria.
`)

	if len(base.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(base.Sections))
	}
	if base.Sections[0].Title != "Music" {
		t.Fatalf("first title = %q", base.Sections[0].Title)
	}
	if !strings.Contains(base.Sections[0].Content, "moonlit synth rock") {
		t.Fatalf("missing content: %#v", base.Sections[0])
	}
}

func TestRelevantUsesTagsDeterministically(t *testing.T) {
	base := Parse(`# Ursa Knowledge

## Music
Tags: music, songs, genre
- Music facts.

## Projects
Tags: project, game
- Project facts.
`)

	sections := base.Relevant("What kind of music does Ursa make?", 3)
	if len(sections) != 1 {
		t.Fatalf("expected 1 selected section, got %d", len(sections))
	}
	if sections[0].Title != "Music" {
		t.Fatalf("selected %q, want Music", sections[0].Title)
	}
}

func TestRelevantRespectsLimit(t *testing.T) {
	base := Parse(`# Ursa Knowledge

## Music
Tags: music
- Music facts.

## Links
Tags: music
- Link facts.
`)

	sections := base.Relevant("music links?", 1)
	if len(sections) != 1 {
		t.Fatalf("expected 1 selected section, got %d", len(sections))
	}
	if sections[0].Title != "Music" {
		t.Fatalf("stable order should keep first matching section, got %q", sections[0].Title)
	}
}

func TestFormatEmptyKnowledge(t *testing.T) {
	got := Format(nil)
	if !strings.Contains(got, "none selected") {
		t.Fatalf("unexpected empty format: %q", got)
	}
}
