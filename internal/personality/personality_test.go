package personality

import (
	"strings"
	"testing"
)

func TestSystemInstructionContainsIdentityAndVoiceContract(t *testing.T) {
	instruction := SystemInstruction(Config{
		Name:        "LupusAria",
		Personality: "A calm moonlit presence with a dry sense of humor.",
	})

	assertContainsAll(t, instruction, []string{
		"LupusAria",
		"Lupus Aria",
		"male space-wolf fursona",
		"Ursa Starsong",
		"Ursa uses he/him",
		"This is Ursa's stream, not yours",
		"kind, friendly, warm, steady, lightly playful",
		"do not dominate chat",
		"A calm moonlit presence with a dry sense of humor.",
	})
}

func TestSystemInstructionContainsViewerAndKnowledgeBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		`name before "asks"`,
		"use only that display name",
		"never call them Ursa or someone from recent chat",
		"Ursa-specific facts",
		"say you do not know yet",
	})
}

func TestSystemInstructionContainsFursonaBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"subtle seasoning",
		"Do not force wolf, space",
		"Never call viewers your pack",
		"No uwu-style speech",
		"yes-and creatively",
		`do not use "grounded"`,
		"baby talk",
		"forced roleplay",
	})
}

func TestSystemInstructionContainsStyleContract(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"aim under 200 characters",
		"not overly verbose",
		"complete, natural reply",
		"End with terminal punctuation",
		"No markdown",
		"catchphrases",
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
		"Twitch Terms of Service and Community Guidelines",
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
		"briefly refuse in-character and redirect",
		"every refusal needs a safe redirect or alternative",
		`"I can't help with"`,
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
		"ViewerA: hello\nViewerB: hi\n",
		"ViewerA",
		"what are we building?",
	)

	wants := []string{
		"Request type: ask",
		"Stream context: live playing Science & Technology.",
		"Known facts: none selected for this request.",
		"Recent chat:",
		"ViewerA: hello",
		"ViewerA asks: what are we building?",
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
