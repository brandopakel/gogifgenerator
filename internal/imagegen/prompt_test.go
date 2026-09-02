package imagegen

import (
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/intent"
)

func TestCinematicPromptPreservesIdeaAndAddsConcreteDirection(t *testing.T) {
	prompt := CinematicPrompt(Request{Prompt: "a hero swinging through the city", Width: 720, Height: 480})
	for _, expected := range []string{
		"hero swinging through the city", "landscape", "720:480", "one continuous scene", "no letterbox bars",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("CinematicPrompt() does not contain %q: %s", expected, prompt)
		}
	}
}

func TestBackroomsConceptIsLiteralAndExcludesTheWrongExterior(t *testing.T) {
	prompt := CinematicPrompt(Request{Prompt: "backrooms", Width: 480, Height: 480, Brief: intent.Interpret("backrooms")})
	for _, expected := range []string{"yellow wallpaper", "fluorescent", "entirely indoors", "no city", "no castle"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("CinematicPrompt() does not contain %q: %s", expected, prompt)
		}
	}
	if !ShouldUpsamplePrompt("backrooms") {
		t.Fatal("ShouldUpsamplePrompt() did not identify a terse concept")
	}
}

func TestCinematicPromptUsesInterpretedSceneAndDirection(t *testing.T) {
	request := Request{
		Prompt: "please make me a gif of a spooky anime skeleton dancing in a graveyard",
		Width:  512, Height: 512,
		Brief: intent.Interpret("please make me a gif of a spooky anime skeleton dancing in a graveyard"),
	}
	prompt := CinematicPrompt(request)
	if !strings.Contains(prompt, "skeleton dancing in a graveyard") {
		t.Fatalf("CinematicPrompt() lost the interpreted scene: %s", prompt)
	}
	if strings.Contains(prompt, "please make me a gif") {
		t.Fatalf("CinematicPrompt() kept request filler: %s", prompt)
	}
	if !strings.Contains(prompt, "Art direction:") || !strings.Contains(prompt, "anime key art") {
		t.Fatalf("CinematicPrompt() dropped the interpreted style: %s", prompt)
	}
	if !strings.Contains(prompt, "Avoid:") {
		t.Fatalf("CinematicPrompt() dropped the negative direction: %s", prompt)
	}
}

func TestCompactDiffusionPromptAvoidsMultiPanelLanguage(t *testing.T) {
	prompt := CompactDiffusionPrompt(Request{Prompt: "a hero swinging through the city", Width: 512, Height: 512})
	for _, expected := range []string{"a hero swinging through the city", "single square cinematic scene", "high detail"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("CompactDiffusionPrompt() does not contain %q: %s", expected, prompt)
		}
	}
	for _, unwanted := range []string{"GIF", "frame", "storyboard", "panel"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("CompactDiffusionPrompt() contains %q: %s", unwanted, prompt)
		}
	}
}

func TestCompactDiffusionPromptStaysShortWithABrief(t *testing.T) {
	idea := "a calm watercolor lighthouse standing on a rocky shore at dawn"
	prompt := CompactDiffusionPrompt(Request{Prompt: idea, Width: 512, Height: 512, Brief: intent.Interpret(idea)})
	if len(prompt) > 400 {
		t.Fatalf("CompactDiffusionPrompt() is %d characters: %s", len(prompt), prompt)
	}
	if !strings.Contains(prompt, "painterly brushwork") {
		t.Fatalf("CompactDiffusionPrompt() dropped the interpreted style: %s", prompt)
	}
}

func TestNegativePromptPrefersTheInterpretation(t *testing.T) {
	brief := intent.Interpret("a photorealistic wolf howling")
	if got := NegativePrompt(Request{Brief: brief}, "fallback"); got == "fallback" {
		t.Fatal("NegativePrompt() ignored the interpreted negative")
	}
	if got := NegativePrompt(Request{}, "fallback"); got != "fallback" {
		t.Fatalf("NegativePrompt() = %q", got)
	}
}
