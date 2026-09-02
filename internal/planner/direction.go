package planner

import (
	"strings"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"github.com/brandopakel/gogifgenerator/internal/intent"
)

// moodPalettes ties colour to the reading of the idea instead of to a hash of
// its characters. Each mood keeps more than one family member so a reroll
// still visibly changes the art without contradicting the interpretation.
var moodPalettes = map[string][][]string{
	intent.MoodCalm: {
		{"#0E1B23", "#2E6E8E", "#6BBFC9", "#BFE3E0", "#F2F7F5"},
		{"#101A2B", "#3D5A80", "#98C1D9", "#E0FBFC", "#EE6C4D"},
	},
	intent.MoodEnergetic: {
		{"#10131A", "#E8FF47", "#FF5C8A", "#70D6FF", "#F7F7F2"},
		{"#0B0C10", "#45A29E", "#66FCF1", "#FF2E63", "#F5F5F5"},
	},
	intent.MoodDramatic: {
		{"#0D1B2A", "#1B263B", "#415A77", "#E0E1DD", "#FF6B6B"},
		{"#12100E", "#2B2118", "#8C6A4A", "#D9C2A7", "#F25C05"},
	},
	intent.MoodJoyful: {
		{"#171219", "#FF8A00", "#FFC900", "#E04B8D", "#FFF4DA"},
		{"#1B1035", "#FF4D9D", "#FFD23F", "#3BCEAC", "#FFFCF2"},
	},
	intent.MoodEerie: {
		{"#140F2D", "#2C2A4A", "#4F518C", "#907AD6", "#DABFFF"},
		{"#0A0F0D", "#1E2D2B", "#3F6C51", "#8FB996", "#C7EFCF"},
	},
	intent.MoodTender: {
		{"#2B1B1F", "#7A4B4B", "#E8A0A0", "#F7C1BB", "#FFE3D8"},
		{"#071E22", "#1D7874", "#6EEB83", "#FFB800", "#F4C095"},
	},
}

// cameraMotions map the interpreted camera move onto the renderer's motion
// vocabulary. The renderer stays small; the interpretation decides which of
// its motions communicates the idea.
var cameraMotions = map[string]string{
	intent.CameraStatic:   "pulse",
	intent.CameraOrbit:    "orbit",
	intent.CameraPushIn:   "pulse",
	intent.CameraPullBack: "pulse",
	intent.CameraPan:      "waves",
	intent.CameraHandheld: "waves",
}

// celebrationTerms trigger the confetti overlay. It is the one motion that
// adds content to the frame, so it stays opt-in by meaning, not by chance.
var celebrationTerms = map[string]bool{
	"confetti": true, "celebrate": true, "celebrating": true, "celebration": true,
	"party": true, "birthday": true, "wedding": true, "graduation": true,
	"congrats": true, "congratulations": true, "victory": true, "champagne": true,
}

func paletteFor(brief intent.Brief, seed int64) []string {
	family, ok := moodPalettes[brief.Mood]
	if !ok || len(family) == 0 {
		return gifdomain.Defaults().Palette
	}
	index := seed % int64(len(family))
	if index < 0 {
		index += int64(len(family))
	}
	return append([]string(nil), family[index]...)
}

func motionFor(brief intent.Brief) string {
	for _, term := range brief.Terms() {
		if celebrationTerms[term] {
			return "confetti"
		}
	}
	if motion, ok := cameraMotions[brief.Camera]; ok {
		return motion
	}
	return gifdomain.Defaults().Motion
}

// captionFor prefers the interpreted subject and action over a truncated
// sentence. "GOLDEN RETRIEVER RUNNING" reads better in a small GIF than the
// first 32 characters of a request that opened with filler.
func captionFor(prompt string, brief intent.Brief) string {
	caption := strings.TrimSpace(strings.Join(strings.Fields(brief.Subject+" "+brief.Action), " "))
	if caption == "" {
		caption = strings.TrimSpace(prompt)
	}
	if runes := []rune(caption); len(runes) > 32 {
		caption = strings.TrimSpace(string(runes[:32]))
	}
	return caption
}

// specFrom assembles the renderer contract from an interpretation plus the
// caller's explicit overrides. Every planner shares it so a remote model and
// the offline reader cannot drift apart.
func specFrom(request Request, brief intent.Brief, seed int64) (gifdomain.Spec, error) {
	spec := gifdomain.Defaults()
	spec.Caption = captionFor(request.Prompt, brief)
	spec.Palette = paletteFor(brief, seed)
	spec.Motion = motionFor(brief)
	spec.Seed = seed
	if request.Width != 0 {
		spec.Width = request.Width
	}
	if request.Height != 0 {
		spec.Height = request.Height
	}
	if request.Frames != 0 {
		spec.Frames = request.Frames
	}
	if request.DelayMS != 0 {
		spec.DelayMS = request.DelayMS
	}
	return spec.Normalize()
}
