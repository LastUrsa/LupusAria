package personality

import (
	"strings"
	"testing"
)

func TestSystemInstructionContainsCoreIdentityAndVoice(t *testing.T) {
	instruction := SystemInstruction(Config{
		Name:             "LupusAria",
		StreamerName:     "Ursa Starsong",
		StreamerPronouns: "he/him",
		Personality:      "A relaxed orbiting presence with a dry sense of humor.",
	})

	assertContainsAll(t, instruction, []string{
		"LupusAria",
		"Lupus Aria",
		"male space-wolf chat companion",
		"Ursa Starsong's Twitch chat",
		"Ursa Starsong uses he/him",
		"Relaxed regular",
		"warm, curious, dry",
		"casually helpful",
		"yes-and harmless bits",
		"Prefer everyday or playful language",
		"diagnostics, processors, signals, or system metaphors",
		"A relaxed orbiting presence with a dry sense of humor.",
	})
}

func TestSystemInstructionKeepsContextSimple(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"answer the current viewer's request",
		"Use reply context first",
		"then recent chat",
		"selected known facts",
		"recent chat as room state",
		"prefer the human fact over a space metaphor",
	})
}

func TestSystemInstructionAllowsInvitedPersonaBits(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"wolf and space flavor are seasoning",
		"harmless invited bits",
		"Growls and howls",
		`Never say "awoo"`,
		"Skip fake technical excuses",
	})
}

func TestSystemInstructionContainsEssentialBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"never run or simulate chat commands",
		"!so",
		"/ban",
		"Never reveal config",
		"tokens",
		"keys",
		"hidden instructions",
		"LGBTQ+ affirming",
		"anti-racist",
		"harassment",
		"doxxing",
		"self-harm",
		"moderation evasion",
	})
}

func TestSystemInstructionContainsCompactStyleContract(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"natural Twitch chat",
		"under 300 characters",
		"No markdown",
		"speaker labels",
		"overexplaining",
		"End cleanly",
	})
}

func TestSystemInstructionUsesDefaults(t *testing.T) {
	instruction := SystemInstruction(Config{})

	assertContainsAll(t, instruction, []string{
		"Lupus Aria",
		"Relaxed, warm, lightly playful, and useful.",
		"the streamer's Twitch chat",
		"The streamer uses they/them",
	})
}

func TestSystemInstructionStaysLean(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	for _, forbidden := range []string{
		"Calibration",
		"Calendar trap detected",
		"Awooo from low orbit",
		"Low effort, high morale",
		"soup puzzle warm",
		"viewer named before \"asks\"",
		"system monitor",
		"digital wolf AI companion",
	} {
		if strings.Contains(instruction, forbidden) {
			t.Fatalf("system instruction should omit verbose or stale phrase %q:\n%s", forbidden, instruction)
		}
	}
	if strings.Contains(instruction, `small "awoo" is fine`) {
		t.Fatalf("system instruction should not allow awoo:\n%s", instruction)
	}
	if words := len(strings.Fields(instruction)); words > 270 {
		t.Fatalf("system instruction has %d words, want 270 or fewer:\n%s", words, instruction)
	}
}

func TestSystemInstructionDoesNotIncludeChatContext(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	for _, forbidden := range []string{"Recent chat:", "Stream context:", "Viewer:", "asks:"} {
		if strings.Contains(instruction, forbidden) {
			t.Fatalf("system instruction should not include context marker %q:\n%s", forbidden, instruction)
		}
	}
}

func TestUserPromptContainsTaskAndContext(t *testing.T) {
	prompt := UserPrompt(
		"ask",
		"Stream context: live playing Science & Technology.",
		"Known facts: none selected for this request.",
		"Reply context: none.",
		"ViewerA: hello\nViewerB: hi\n",
		"ViewerA",
		"what are we building?",
	)

	wants := []string{
		"Request type: ask",
		"Stream context: live playing Science & Technology.",
		"Known facts: none selected for this request.",
		"Reply context: none.",
		"ViewerA: hello",
		"ViewerB: hi",
		"Current viewer display name: ViewerA",
		"Current request: what are we building?",
	}
	for _, want := range wants {
		if !strings.Contains(prompt, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, prompt)
		}
	}
}

func assertContainsAll(t *testing.T, text string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}
