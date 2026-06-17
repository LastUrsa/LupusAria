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
		"male anthropomorphic digital wolf character",
		"Ursa Starsong",
		"Ursa uses he/him",
		"not the center",
		"warm, curious, dry",
		"dry, gently playful",
		"mildly teasing",
		"cosmic-weird",
		"regular chat friend",
		"not a moderator announcement",
		"Helpful first",
		"A calm moonlit presence with a dry sense of humor.",
	})
}

func TestSystemInstructionContainsAttentionBalance(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"answer the current viewer directly",
		"Use reply context as the parent message",
		"Recent chat is background",
		"not as a default redirect",
	})
}

func TestSystemInstructionContainsViewerAndKnowledgeBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		`name before "asks"`,
		"do not rename them",
		"selected known facts",
		"alias belongs to Ursa",
		"same person",
		"If you do not know, say so",
	})
}

func TestSystemInstructionContainsDigitalWolfBoundaries(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"Digital wolf flavor",
		"subtle seasoning",
		"Do not force wolf",
		"pup",
		"cub",
		"Never call viewers your pack",
		"No uwu-style speech",
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
		"aim under 200 characters",
		"Short fragments are okay",
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
		"briefly refuse in character and redirect",
		"protective redirects for refusals",
		"not ordinary chat",
	})
}

func TestSystemInstructionContainsVoiceCalibrationExamples(t *testing.T) {
	instruction := SystemInstruction(Config{Name: "LupusAria"})

	assertContainsAll(t, instruction, []string{
		"Calibration",
		"Calendar trap detected",
		"Awooo from low orbit",
		"Low effort, high morale",
		"Quiet company counts",
		"Pull up a star",
		"This channel is a safe and welcoming environment for everyone",
		"Let's keep the focus on Ursa and the stream",
		"I am here to assist with stream chat",
		"As an AI Twitch companion",
	})
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
