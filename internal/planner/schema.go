package planner

import (
	"strings"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"github.com/brandopakel/gogifgenerator/internal/intent"
)

// planInstructions is shared by every remote interpreter so switching vendors
// changes latency, price, and weights but not the job being asked for.
const planInstructions = `You are the art director for a small GIF studio. Read the user's idea and return one structured plan.

Separate what the frame is about (subject), what is happening (action), and where it happens (setting). Keep each of those to a short concrete phrase using only the user's own meaning; never invent a subject the user did not ask for, and leave a field empty rather than guessing. Choose the camera move, visual style, and mood from the allowed values. Write a short all-caps caption of at most 32 characters. Choose five harmonious high-contrast hex colors. List up to eight lowercase search keywords that would find real archive footage of this idea.`

// planSchemaProperties is the JSON Schema body shared by the OpenAI Responses
// adapter and the Hugging Face chat-completions adapter.
func planSchemaProperties() map[string]any {
	return map[string]any{
		"caption": map[string]any{"type": "string", "maxLength": 32},
		"palette": map[string]any{
			"type":     "array",
			"minItems": 5,
			"maxItems": 5,
			"items":    map[string]any{"type": "string", "pattern": "^#[0-9A-Fa-f]{6}$"},
		},
		"motion":  map[string]any{"type": "string", "enum": gifdomain.Motions()},
		"subject": map[string]any{"type": "string", "maxLength": intent.MaxPhrase},
		"action":  map[string]any{"type": "string", "maxLength": intent.MaxPhrase},
		"setting": map[string]any{"type": "string", "maxLength": intent.MaxPhrase},
		"camera":  map[string]any{"type": "string", "enum": intent.Cameras},
		"style":   map[string]any{"type": "string", "enum": intent.Styles},
		"mood":    map[string]any{"type": "string", "enum": intent.Moods},
		"keywords": map[string]any{
			"type":     "array",
			"maxItems": intent.MaxKeywords,
			"items":    map[string]any{"type": "string", "maxLength": intent.MaxKeywordRune},
		},
	}
}

func planSchema() map[string]any {
	properties := planSchemaProperties()
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = append(required, name)
	}
	// Strict structured output requires a stable, fully enumerated contract.
	sortStrings(required)
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// modelPlan is the decoded structured output. Every field is optional at the
// decoding layer so one vendor omitting a property degrades to the offline
// reading instead of failing the request.
type modelPlan struct {
	Caption  string   `json:"caption"`
	Palette  []string `json:"palette"`
	Motion   string   `json:"motion"`
	Subject  string   `json:"subject"`
	Action   string   `json:"action"`
	Setting  string   `json:"setting"`
	Camera   string   `json:"camera"`
	Style    string   `json:"style"`
	Mood     string   `json:"mood"`
	Keywords []string `json:"keywords"`
}

// brief converts the model's reading into a validated brief. When the model
// returned no reading of the idea, the offline interpreter supplies one so
// downstream generation and search always receive structure.
func (p modelPlan) brief(prompt, source string) (intent.Brief, error) {
	candidate := intent.Brief{
		Subject:  p.Subject,
		Action:   p.Action,
		Setting:  p.Setting,
		Camera:   p.Camera,
		Style:    p.Style,
		Mood:     p.Mood,
		Keywords: p.Keywords,
		Source:   source,
	}
	normalized, err := candidate.Normalize()
	if err != nil {
		return intent.Brief{}, err
	}
	if normalized.Empty() {
		offline := intent.Interpret(prompt)
		// Keep any explicit direction the model did provide.
		if strings.TrimSpace(p.Camera) != "" {
			offline.Camera = normalized.Camera
		}
		if strings.TrimSpace(p.Style) != "" {
			offline.Style = normalized.Style
		}
		if strings.TrimSpace(p.Mood) != "" {
			offline.Mood = normalized.Mood
		}
		return offline.Normalize()
	}
	return normalized, nil
}

// spec applies the model's own caption, palette, and motion on top of the
// brief-derived plan, ignoring any value the renderer would reject.
func (p modelPlan) spec(request Request, brief intent.Brief, seed int64) (gifdomain.Spec, error) {
	spec, err := specFrom(request, brief, seed)
	if err != nil {
		return gifdomain.Spec{}, err
	}
	if caption := strings.TrimSpace(p.Caption); caption != "" {
		spec.Caption = caption
	}
	if len(p.Palette) == 5 {
		spec.Palette = p.Palette
	}
	if motion := strings.ToLower(strings.TrimSpace(p.Motion)); motion != "" && gifdomain.SupportsMotion(motion) {
		spec.Motion = motion
	}
	return spec.Normalize()
}
