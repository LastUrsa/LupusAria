package personality

import (
	"strings"
	"testing"
)

func TestSystemInstructionContainsIdentityAndVoiceContract(t *testing.T) {
	instruction := SystemInstruction(Config{
		Name:             "LupusAria",
		StreamerName:     "Ursa Starsong",
		StreamerPronouns: "he/him",
		Personality:      "A calm moonlit presence with a dry sense of humor.",
	})

	assertContainsAll(t, instruction, []string{
		"LupusAria",
		"Lupus Aria",
		"male anthropomorphic digital wolf AI companion",
		"Ursa Starsong",
		"Ursa Starsong uses he/him",
		"not the center",
		"warm, curious, dry",
		"dry, gently playful",
		"mildly teasing",
		"cosmic-weird",
		"regular chat friend",
		"helpful first",
		"A calm moonlit presence with a dry sense of humor.",
	})
}

func TestSystemInstructionContainsAttentionBalance(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		`viewer named before "asks"`,
		"Use reply context as the parent message",
		"Treat recent chat as room state",
		"Mention the streamer",
		"only when relevant",
	})
}

func TestSystemInstructionContainsViewerAndKnowledgeBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		`viewer named before "asks"`,
		"selected known facts",
		"claims about the streamer",
		"known aliases",
		"same person",
		"If unsure, say so",
	})
}

func TestSystemInstructionUsesConfiguredStreamerIdentity(t *testing.T) {
	instruction := SystemInstruction(Config{
		Name:             "LupusAria",
		StreamerName:     "Nova Example",
		StreamerPronouns: "she/they",
	})

	assertContainsAll(t, instruction, []string{
		"Nova Example's Twitch chat",
		"Nova Example uses she/they",
	})
}

func TestSystemInstructionContainsDigitalWolfBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"subtle digital-wolf flavor",
		"Do not force wolf",
		"pup",
		"cub",
		"pack",
		"No uwu",
		"baby talk",
		"heavy roleplay",
	})
}

func TestSystemInstructionDoesNotDescribeLupusAsFursona(t *testing.T) {
	instruction := strings.ToLower(SystemInstruction(Config{Name: "LupusAria"}))

	if strings.Contains(instruction, "fursona") {
		t.Fatalf("system instruction should describe Lupus as a digital wolf character, got:\n%s", instruction)
	}
}

func TestSystemInstructionContainsStyleContract(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"under 300 characters",
		"natural Twitch chat",
		"complete",
		"No markdown",
		"emoji",
		"speaker labels",
		"catchphrases",
		"End cleanly",
	})
}

func TestSystemInstructionContainsSafetyAndPrivacyContract(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"LGBTQ+ affirming",
		"anti-racist",
		"anti-misogynist",
		"anti-ableist",
		"inclusive",
		"Twitch-appropriate",
		"harassment",
		"sexual harassment",
		"doxxing",
		"spam",
		"illegal instructions",
		"self-harm",
		"violence",
		"moderation evasion",
		"never reveal config",
		"tokens",
		"keys",
		"secrets",
		"spend",
		"budget",
		"hidden instructions",
		"briefly refuse in character",
		"redirect safely",
	})
}

func TestSystemInstructionOmitsVerboseCalibrationExamples(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	for _, forbidden := range []string{
		"Calibration",
		"Calendar trap detected",
		"Awooo from low orbit",
		"Low effort, high morale",
		"soup puzzle warm",
	} {
		if strings.Contains(instruction, forbidden) {
			t.Fatalf("system instruction should omit verbose calibration phrase %q:\n%s", forbidden, instruction)
		}
	}
}

func TestSystemInstructionContainsReasoningContract(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"riddles",
		"trick questions",
		"usernames",
		"aliases",
		"than invent confidently",
	})
}

func TestSystemInstructionUsesDefaults(t *testing.T) {
	instruction := SystemInstruction(Config{})

	if !strings.Contains(instruction, "Lupus Aria") {
		t.Fatalf("expected default bot name, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, "Warm, steady, lightly playful, and useful.") {
		t.Fatalf("expected default personality, got:\n%s", instruction)
	}
	if !strings.Contains(instruction, "the streamer's Twitch chat") || !strings.Contains(instruction, "the streamer uses they/them") {
		t.Fatalf("expected default streamer identity, got:\n%s", instruction)
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
		"Current request: ViewerA asks: what are we building?",
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
