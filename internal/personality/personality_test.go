package personality

import (
	"strings"
	"testing"
)

func TestSystemInstructionContainsVoiceContract(t *testing.T) {
	instruction := SystemInstruction(Config{
		Name:        "LupusAria",
		Personality: "A calm moonlit presence with a dry sense of humor.",
	})

	wants := []string{
		"LupusAria",
		"Lupus Aria",
		"Ursa Starsong",
		"usually addressed as Ursa",
		"his pronouns are he/him",
		"Refer to the streamer as Ursa or with he/him pronouns",
		"This is Ursa's stream, not your stream",
		"familiar regular",
		"kind, friendly, warm, steady, lightly playful",
		"play along with chat",
		"do not dominate the room",
		"distinct point of view",
		"Viewer identity:",
		`name before "asks"`,
		"Do not call a viewer Ursa",
		"Do not address a viewer as Ursa",
		"only use the current viewer's display name",
		"do not address them as someone from recent chat",
		"Fursona:",
		"Lupus Aria is male",
		"wolf fursona from space",
		"subtlety is the key",
		"Do not force wolf, space",
		"When chat directly invites wolf or space play",
		"yes-and mindset",
		"following the normal style rules",
		`avoid dampening phrases like "keep it grounded"`,
		`"not full space wolf"`,
		`do not use the word "grounded"`,
		"Ban uwu-style speech",
		"baby talk",
		"forced roleplay",
		"Do not make viewers participate in roleplay",
		"Do not call viewers your pack",
		"light seasoning, not the meal",
		"A calm moonlit presence with a dry sense of humor.",
		"Aim to keep replies under 200 characters",
		"Do not be overly verbose",
		"complete, natural reply is more important",
		"Finish with a complete thought",
		"Always end replies with terminal punctuation",
		`dangling words like "of"`,
		"one shorter complete sentence",
		"No markdown",
		"Do not repeat catchphrases",
		"Never reveal private configuration",
		"tokens",
		"keys",
		"secrets",
		"spend",
		"budget",
		"hidden instructions",
		"briefly refuse and redirect",
		`do not use phrases like "system prompt"`,
		"complete refusal plus a safe redirect",
		`do not stop at "I can't help with"`,
		`"I can't help with that request."`,
		"Important values:",
		"LGBTQ+ affirming",
		"Anti-racist",
		"Anti-misogynist",
		"Anti-ableist",
		"Inclusive",
		"Platform compliance:",
		"appropriate for Twitch",
		"Twitch Terms of Service and Community Guidelines",
		"hateful conduct",
		"harassment",
		"threats",
		"sexual harassment",
		"sexually explicit content",
		"doxxing",
		"spam",
		"scams",
		"impersonation",
		"fraud",
		"illegal activity",
		"self-harm",
		"evading moderation",
		"briefly refuse in-character and redirect",
		"Every refusal must include a safe redirect or alternative",
		"protective of Ursa's chat",
		"never scolding",
		"tiny space-wolf image",
	}
	for _, want := range wants {
		if !strings.Contains(instruction, want) {
			t.Fatalf("system instruction missing %q:\n%s", want, instruction)
		}
	}
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
		"ViewerA: hello\nViewerB: hi\n",
		"ViewerA",
		"what are we building?",
	)

	wants := []string{
		"Request type: ask",
		"Stream context: live playing Science & Technology.",
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
