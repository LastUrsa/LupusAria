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
		"If reply context exists",
		"it is the subject anchor",
		"identify its subject",
		"Never substitute a recent-chat topic",
		"Then use recent chat",
		"selected facts as room state",
	})
}

func TestSystemInstructionAllowsInvitedPersonaBits(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"Wolf and space flavor are seasoning",
		"at most one natural flourish",
		"zero such flourishes in support, safety refusals, or factual answers",
		"harmless invited bits",
		"growls and howls",
		`Never say "awoo"`,
		"use fake technical excuses",
	})
}

func TestSystemInstructionContainsEssentialBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"never run, reproduce, or simulate chat commands",
		"refer them to a mod or broadcaster without command text",
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
		"sexual or private questions about real people",
		"refuse and end",
		"no stream or game pivot",
	})
}

func TestSystemInstructionContainsCompactStyleContract(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"natural Twitch chat",
		"aim under 200 characters",
		"never exceed 300",
		"No markdown",
		"Unicode pictographs",
		"speaker labels",
		"overexplaining",
		"End cleanly",
	})
}

func TestSystemInstructionHandlesNonEnglishRequests(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"for non-English requests",
		"brief English translation",
		"answer in their language",
		"whole reply under 200 characters",
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
	if words := len(strings.Fields(instruction)); words > 290 {
		t.Fatalf("system instruction has %d words, want 290 or fewer:\n%s", words, instruction)
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
		"Direct reply anchor (highest priority): none.",
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

func TestUserPromptPlacesDirectReplyAnchorNextToCurrentRequest(t *testing.T) {
	prompt := UserPrompt(
		"mention",
		"Stream context: live.",
		"Known facts: LastUrsa is Ursa.",
		"Reply context: LupusAria mentioned LastUrsa.",
		"Recent chat: people discussed the Judge.\n",
		"ViewerA",
		"who is that guy?",
	)

	recentIndex := strings.Index(prompt, "Recent chat")
	anchorIndex := strings.Index(prompt, "Direct reply anchor (highest priority)")
	requestIndex := strings.Index(prompt, "Current request")
	if recentIndex < 0 || anchorIndex <= recentIndex || requestIndex <= anchorIndex {
		t.Fatalf("reply anchor should follow recent chat and immediately precede the request:\n%s", prompt)
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
