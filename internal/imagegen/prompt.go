package imagegen

import (
	"fmt"
	"strings"
)

// CinematicPrompt turns a short user idea into a concrete keyframe brief. It
// deliberately asks the image model—not the downstream 3D renderers—to solve
// subject identity, action, setting, composition, and physical plausibility.
func CinematicPrompt(prompt string, width, height int) string {
	prompt = strings.TrimSpace(prompt)
	orientation := "square"
	if width > height {
		orientation = "landscape"
	} else if height > width {
		orientation = "portrait"
	}
	return fmt.Sprintf(`Create one highly detailed cinematic keyframe for an animated GIF.

User request: %s

Depict the requested subject, action, and environment literally and immediately recognizably. Show the action already in progress with a physically plausible pose, clear direction of travel, expressive body language, environmental interaction, and a strong silhouette. Use realistic materials, coherent anatomy and perspective, natural depth, cinematic lighting, detailed foreground and background, and a clear focal subject. Compose for a %s %d:%d canvas with safe room around the subject for camera motion and cropping. Make this a single continuous scene, not a collage, storyboard, poster, logo, or abstract visualization. Do not add captions, labels, borders, watermarks, or UI.`, prompt, orientation, width, height)
}
