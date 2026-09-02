// Package intent holds the structured interpretation of a natural-language
// idea. It is deliberately lower-level than both planning and image
// generation so a single reading of the user's words can drive renderer
// settings, generator prompts, and catalog search without any layer
// re-guessing what the user meant.
package intent

import (
	"fmt"
	"sort"
	"strings"
)

const (
	MaxPhrase      = 96
	MaxNegative    = 320
	MaxKeywords    = 8
	MaxKeywordRune = 32
)

// Camera describes how the frame should move. The GIF renderer, the diffusion
// prompt, and the cinematic pipeline all read the same value.
const (
	CameraStatic   = "static"
	CameraOrbit    = "orbit"
	CameraPushIn   = "push-in"
	CameraPullBack = "pull-back"
	CameraPan      = "pan"
	CameraHandheld = "handheld"
)

const (
	StylePhotoreal    = "photoreal"
	StyleIllustration = "illustration"
	StyleAnime        = "anime"
	StyleRetro        = "retro"
	StylePainterly    = "painterly"
	StyleRender3D     = "3d-render"
	StylePixel        = "pixel"
)

const (
	MoodCalm      = "calm"
	MoodEnergetic = "energetic"
	MoodDramatic  = "dramatic"
	MoodJoyful    = "joyful"
	MoodEerie     = "eerie"
	MoodTender    = "tender"
)

// Cameras, Styles, and Moods are the closed vocabularies a remote model may
// return. Keeping them small makes strict structured output reliable and
// keeps an unknown value a validation failure rather than a silent default.
var (
	Cameras = []string{CameraStatic, CameraOrbit, CameraPushIn, CameraPullBack, CameraPan, CameraHandheld}
	Styles  = []string{StylePhotoreal, StyleIllustration, StyleAnime, StyleRetro, StylePainterly, StyleRender3D, StylePixel}
	Moods   = []string{MoodCalm, MoodEnergetic, MoodDramatic, MoodJoyful, MoodEerie, MoodTender}
)

func allowed(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// Brief is one reading of the user's idea. Every field is optional except the
// three closed vocabularies, which Normalize fills with safe defaults.
type Brief struct {
	Subject  string   `json:"subject,omitempty"`
	Action   string   `json:"action,omitempty"`
	Setting  string   `json:"setting,omitempty"`
	Camera   string   `json:"camera,omitempty"`
	Style    string   `json:"style,omitempty"`
	Mood     string   `json:"mood,omitempty"`
	Negative string   `json:"negative,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
	// Source records which interpreter produced this brief so responses,
	// logs, and evaluation datasets can separate offline reading from a
	// remote model's reading.
	Source string `json:"source,omitempty"`
}

// Normalize bounds every field and rejects values outside the closed
// vocabularies. A remote model that invents a camera move fails here and the
// caller falls back rather than silently rendering something else.
func (b Brief) Normalize() (Brief, error) {
	b.Subject = clampPhrase(b.Subject)
	b.Action = clampPhrase(b.Action)
	b.Setting = clampPhrase(b.Setting)
	b.Negative = clamp(strings.TrimSpace(b.Negative), MaxNegative)
	b.Source = clamp(strings.ToLower(strings.TrimSpace(b.Source)), 32)

	b.Camera = strings.ToLower(strings.TrimSpace(b.Camera))
	if b.Camera == "" {
		b.Camera = CameraStatic
	}
	if !allowed(Cameras, b.Camera) {
		return Brief{}, fmt.Errorf("intent: unsupported camera %q", b.Camera)
	}
	b.Style = strings.ToLower(strings.TrimSpace(b.Style))
	if b.Style == "" {
		b.Style = StylePhotoreal
	}
	if !allowed(Styles, b.Style) {
		return Brief{}, fmt.Errorf("intent: unsupported style %q", b.Style)
	}
	b.Mood = strings.ToLower(strings.TrimSpace(b.Mood))
	if b.Mood == "" {
		b.Mood = MoodEnergetic
	}
	if !allowed(Moods, b.Mood) {
		return Brief{}, fmt.Errorf("intent: unsupported mood %q", b.Mood)
	}

	seen := make(map[string]bool, len(b.Keywords))
	keywords := make([]string, 0, MaxKeywords)
	for _, keyword := range b.Keywords {
		keyword = clamp(strings.ToLower(strings.TrimSpace(keyword)), MaxKeywordRune)
		if keyword == "" || seen[keyword] {
			continue
		}
		seen[keyword] = true
		keywords = append(keywords, keyword)
		if len(keywords) == MaxKeywords {
			break
		}
	}
	if len(keywords) == 0 {
		keywords = nil
	}
	b.Keywords = keywords
	if b.Negative == "" {
		b.Negative = negativeFor(b.Style)
	}
	return b, nil
}

// Empty reports whether the brief carries no reading of the idea itself. A
// brief with only defaults should not override a caller's own prompt.
func (b Brief) Empty() bool {
	return strings.TrimSpace(b.Subject) == "" &&
		strings.TrimSpace(b.Action) == "" &&
		strings.TrimSpace(b.Setting) == "" &&
		len(b.Keywords) == 0
}

// Scene renders the subject, action, and setting as one sentence for an image
// model. It never invents content that the interpreter did not find.
func (b Brief) Scene() string {
	parts := make([]string, 0, 3)
	if subject := strings.TrimSpace(b.Subject); subject != "" {
		parts = append(parts, subject)
	}
	if action := strings.TrimSpace(b.Action); action != "" {
		parts = append(parts, action)
	}
	if setting := strings.TrimSpace(b.Setting); setting != "" {
		parts = append(parts, setting)
	}
	return strings.Join(parts, " ")
}

// Descriptors returns the style and mood as short prompt-ready phrases.
func (b Brief) Descriptors() []string {
	descriptors := make([]string, 0, 3)
	if phrase := styleDescriptors[b.Style]; phrase != "" {
		descriptors = append(descriptors, phrase)
	}
	if phrase := moodDescriptors[b.Mood]; phrase != "" {
		descriptors = append(descriptors, phrase)
	}
	if phrase := cameraDescriptors[b.Camera]; phrase != "" {
		descriptors = append(descriptors, phrase)
	}
	return descriptors
}

// SearchQuery is the catalog query implied by the idea. Archive catalogs match
// concrete nouns far better than whole sentences, so this drops filler and
// keeps the subject, its action, and its setting.
func (b Brief) SearchQuery(fallback string) string {
	terms := make([]string, 0, MaxKeywords)
	seen := make(map[string]bool, MaxKeywords)
	for _, phrase := range []string{b.Subject, b.Action, b.Setting} {
		for _, word := range strings.Fields(strings.ToLower(phrase)) {
			word = strings.Trim(word, ".,!?;:'\"")
			if word == "" || stopwords[word] || seen[word] {
				continue
			}
			seen[word] = true
			terms = append(terms, word)
		}
	}
	for _, keyword := range b.Keywords {
		if seen[keyword] || len(terms) >= MaxKeywords {
			continue
		}
		seen[keyword] = true
		terms = append(terms, keyword)
	}
	if len(terms) == 0 {
		return strings.TrimSpace(fallback)
	}
	return strings.Join(terms, " ")
}

// Terms returns the deduplicated lowercase vocabulary of the brief. Ranking
// uses it to score a catalog result without re-parsing the original prompt.
func (b Brief) Terms() []string {
	seen := make(map[string]bool)
	for _, phrase := range []string{b.Subject, b.Action, b.Setting} {
		for _, word := range strings.Fields(strings.ToLower(phrase)) {
			word = strings.Trim(word, ".,!?;:'\"")
			if word != "" && !stopwords[word] {
				seen[word] = true
			}
		}
	}
	for _, keyword := range b.Keywords {
		seen[keyword] = true
	}
	terms := make([]string, 0, len(seen))
	for term := range seen {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return terms
}

func clampPhrase(value string) string {
	return clamp(strings.Join(strings.Fields(value), " "), MaxPhrase)
}

func clamp(value string, limit int) string {
	if runes := []rune(value); len(runes) > limit {
		return strings.TrimSpace(string(runes[:limit]))
	}
	return value
}
