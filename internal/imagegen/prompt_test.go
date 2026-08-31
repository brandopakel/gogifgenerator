package imagegen

import (
	"strings"
	"testing"
)

func TestCinematicPromptPreservesIdeaAndAddsConcreteDirection(t *testing.T) {
	prompt := CinematicPrompt("a hero swinging through the city", 720, 480)
	for _, expected := range []string{
		"a hero swinging through the city", "action already in progress", "physically plausible pose", "landscape 720:480", "not a collage",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("CinematicPrompt() does not contain %q: %s", expected, prompt)
		}
	}
}

func TestCompactDiffusionPromptAvoidsMultiPanelLanguage(t *testing.T) {
	prompt := CompactDiffusionPrompt("a hero swinging through the city", 512, 512)
	for _, expected := range []string{"a hero swinging through the city", "Single square cinematic scene", "action in progress", "photorealistic"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("CompactDiffusionPrompt() does not contain %q: %s", expected, prompt)
		}
	}
	for _, unwanted := range []string{"GIF", "storyboard", "collage"} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("CompactDiffusionPrompt() contains %q: %s", unwanted, prompt)
		}
	}
}
