package intent

import (
	"strings"
	"testing"
)

func TestInterpretReadsSubjectActionAndSetting(t *testing.T) {
	brief := Interpret("a golden retriever puppy running through a sprinkler in a sunny backyard")
	if brief.Subject != "golden retriever puppy" {
		t.Fatalf("Subject = %q", brief.Subject)
	}
	if brief.Action != "running" {
		t.Fatalf("Action = %q", brief.Action)
	}
	if !strings.HasPrefix(brief.Setting, "through a sprinkler") {
		t.Fatalf("Setting = %q", brief.Setting)
	}
	if brief.Source != "local" {
		t.Fatalf("Source = %q", brief.Source)
	}
}

func TestInterpretDetectsStyleMoodAndCamera(t *testing.T) {
	brief := Interpret("spooky anime skeleton dancing, zoom in")
	if brief.Style != StyleAnime {
		t.Fatalf("Style = %q", brief.Style)
	}
	if brief.Mood != MoodEerie {
		t.Fatalf("Mood = %q", brief.Mood)
	}
	if brief.Camera != CameraPushIn {
		t.Fatalf("Camera = %q", brief.Camera)
	}
	if strings.Contains(brief.Subject, "anime") || strings.Contains(brief.Subject, "spooky") {
		t.Fatalf("Subject kept a style or mood marker: %q", brief.Subject)
	}
	if brief.Subject != "skeleton" {
		t.Fatalf("Subject = %q", brief.Subject)
	}
}

func TestInterpretHandlesLeadingGerund(t *testing.T) {
	brief := Interpret("spinning vinyl record")
	if brief.Subject != "vinyl record" {
		t.Fatalf("Subject = %q", brief.Subject)
	}
	if brief.Action != "spinning" {
		t.Fatalf("Action = %q", brief.Action)
	}
	if brief.Camera != CameraOrbit {
		t.Fatalf("Camera = %q", brief.Camera)
	}
}

func TestInterpretKeepsBareNounPrompts(t *testing.T) {
	brief := Interpret("a red sports car")
	if brief.Subject != "red sports car" {
		t.Fatalf("Subject = %q", brief.Subject)
	}
	if brief.Action != "" || brief.Setting != "" {
		t.Fatalf("invented action %q or setting %q", brief.Action, brief.Setting)
	}
	if brief.Camera != CameraStatic {
		t.Fatalf("Camera = %q", brief.Camera)
	}
}

func TestInterpretStopsPhrasesAtPunctuation(t *testing.T) {
	brief := Interpret("astronaut floating in zero gravity. neon lights everywhere")
	if strings.Contains(brief.Setting, "neon") {
		t.Fatalf("Setting crossed a clause boundary: %q", brief.Setting)
	}
}

func TestInterpretIsDeterministic(t *testing.T) {
	first := Interpret("a chef flipping pancakes in a diner")
	second := Interpret("a chef flipping pancakes in a diner")
	if first.Scene() != second.Scene() || first.Mood != second.Mood {
		t.Fatalf("Interpret is not deterministic: %#v vs %#v", first, second)
	}
}

func TestInterpretEmptyPromptStillNormalizes(t *testing.T) {
	brief := Interpret("   ")
	if brief.Camera != CameraStatic || brief.Style != StylePhotoreal || brief.Mood != MoodEnergetic {
		t.Fatalf("empty prompt = %#v", brief)
	}
	if !brief.Empty() {
		t.Fatal("empty prompt produced a non-empty brief")
	}
}

func TestSearchQueryDropsFillerAndKeepsNouns(t *testing.T) {
	brief := Interpret("please make me a gif of a rocket launching from a desert pad")
	query := brief.SearchQuery("fallback")
	for _, unwanted := range []string{"please", "gif", "make", "from"} {
		if strings.Contains(query, unwanted) {
			t.Fatalf("SearchQuery kept filler %q: %q", unwanted, query)
		}
	}
	if !strings.Contains(query, "rocket") || !strings.Contains(query, "launching") {
		t.Fatalf("SearchQuery lost the idea: %q", query)
	}
}

func TestSearchQueryFallsBackWhenNothingSurvives(t *testing.T) {
	brief := Brief{}
	normalized, err := brief.Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got := normalized.SearchQuery("original words"); got != "original words" {
		t.Fatalf("SearchQuery() = %q", got)
	}
}

func TestNormalizeRejectsUnknownVocabulary(t *testing.T) {
	for _, brief := range []Brief{
		{Camera: "barrel-roll"},
		{Style: "claymation-deluxe"},
		{Mood: "hangry"},
	} {
		if _, err := brief.Normalize(); err == nil {
			t.Fatalf("Normalize(%#v) expected an error", brief)
		}
	}
}

func TestNormalizeBoundsKeywordsAndPhrases(t *testing.T) {
	brief := Brief{
		Subject:  strings.Repeat("x", MaxPhrase+40),
		Keywords: []string{"one", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"},
	}
	normalized, err := brief.Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if len([]rune(normalized.Subject)) > MaxPhrase {
		t.Fatalf("Subject was not bounded: %d runes", len([]rune(normalized.Subject)))
	}
	if len(normalized.Keywords) != MaxKeywords {
		t.Fatalf("Keywords = %v", normalized.Keywords)
	}
	if normalized.Keywords[0] != "one" || normalized.Keywords[1] != "two" {
		t.Fatalf("Keywords were not deduplicated in order: %v", normalized.Keywords)
	}
}

func TestNegativePromptVariesByStyle(t *testing.T) {
	photoreal, _ := Brief{Style: StylePhotoreal}.Normalize()
	anime, _ := Brief{Style: StyleAnime}.Normalize()
	if !strings.Contains(photoreal.Negative, "cartoon") {
		t.Fatalf("photoreal negative = %q", photoreal.Negative)
	}
	if !strings.Contains(anime.Negative, "photorealistic") {
		t.Fatalf("anime negative = %q", anime.Negative)
	}
	if photoreal.Negative == anime.Negative {
		t.Fatal("style did not change the negative prompt")
	}
}
