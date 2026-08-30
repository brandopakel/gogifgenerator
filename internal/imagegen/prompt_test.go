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
