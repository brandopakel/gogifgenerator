package imagegen

import (
	"fmt"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/intent"
)

// CinematicPrompt turns a short user idea into a concrete keyframe brief. It
// deliberately asks the image model—not the downstream 3D renderers—to solve
// subject identity, action, setting, composition, and physical plausibility.
//
// When the request carries a structured interpretation, the scene sentence and
// art direction come from that reading instead of from the raw sentence, so
// the model receives the same subject/action/setting the renderer and the
// catalog search are working from.
func CinematicPrompt(request Request) string {
	scene := ExpandConcept(request.Prompt)
	if brief := request.Brief.Scene(); brief != "" {
		scene = brief
	}
	if strings.Contains(strings.ToLower(request.Prompt), "backrooms") || strings.EqualFold(request.Brief.Subject, "backrooms") {
		scene = backroomsConcept
	}
	direction := ""
	if descriptors := request.Brief.Descriptors(); len(descriptors) > 0 {
		direction = "\n\nArt direction: " + strings.Join(descriptors, "; ") + "."
	}
	avoid := ""
	if negative := strings.TrimSpace(request.Brief.Negative); negative != "" {
		avoid = "\n\nAvoid: " + negative + "."
	}
	return fmt.Sprintf(`Create one highly detailed cinematic source frame for a short seamless video.

User request: %s%s%s

Interpret the request literally and preserve whether it is about an environment, object, character, or action. Do not invent a person, action, landmark, exterior, or unrelated genre when the request describes an environment. When action is requested, show it already in progress with physically plausible anatomy, direction, and environmental interaction. Use realistic materials, coherent geometry and perspective, natural depth, cinematic lighting, detailed foreground and background, and one immediately recognizable focal idea. Compose edge-to-edge for a %s %d:%d canvas with safe room for subtle motion and cropping. Make this one continuous scene, never a collage, storyboard, poster, logo, or abstract visualization. Fill the entire canvas: no letterbox bars, pillarbox bars, borders, frames, captions, labels, logos, signatures, watermarks, subtitles, or UI.`,
		scene, direction, avoid, orientationOf(request.Width, request.Height), request.Width, request.Height)
}

// CompactDiffusionPrompt stays within the short CLIP context used by classic
// Stable Diffusion checkpoints. Mentioning GIF frames or storyboards tends to
// make those models draw multiple panels, so this brief describes only the
// source photograph that GoGIF will animate later.
//
// The interpreted subject/action/setting is the most valuable part of a short
// context, so filler words from the original sentence are dropped first.
func CompactDiffusionPrompt(request Request) string {
	scene := strings.TrimSpace(request.Prompt)
	if brief := request.Brief.Scene(); brief != "" {
		scene = brief
	}
	parts := []string{scene}
	parts = append(parts, request.Brief.Descriptors()...)
	parts = append(parts, fmt.Sprintf("single %s cinematic scene", orientationOf(request.Width, request.Height)))
	parts = append(parts,
		"one clear focal subject",
		"action in progress",
		"dynamic pose",
		"detailed environment",
		"realistic materials",
		"coherent anatomy",
		"natural perspective",
		"dramatic lighting",
		"high detail",
	)
	return strings.Join(parts, ", ")
}

// NegativePrompt returns the interpreted negative direction, falling back to
// the caller's default when the request carries no interpretation.
func NegativePrompt(request Request, fallback string) string {
	if negative := strings.TrimSpace(request.Brief.Negative); negative != "" {
		return negative
	}
	return fallback
}

func orientationOf(width, height int) string {
	switch {
	case width > height:
		return "landscape"
	case height > width:
		return "portrait"
	default:
		return "square"
	}
}

// ExpandConcept restates a short idea as the fuller concept GoGIF actually
// asked the generator for. It is shown back to the user as the revised
// prompt, so it must describe the same idea in the user's own terms rather
// than advertise renderer settings.
func ExpandConcept(prompt string) string {
	if strings.EqualFold(strings.TrimSpace(prompt), "backrooms") || strings.EqualFold(strings.TrimSpace(prompt), "the backrooms") {
		return backroomsConcept
	}
	brief := intent.Interpret(prompt)
	scene := brief.Scene()
	if scene == "" {
		return strings.TrimSpace(prompt)
	}
	descriptors := brief.Descriptors()
	if len(descriptors) == 0 {
		return scene
	}
	return scene + " — " + strings.Join(descriptors, "; ")
}

const backroomsConcept = "The Backrooms: an endless, empty maze of connected windowless rooms with stained yellow wallpaper, buzzing fluorescent ceiling panels, damp beige carpet, low ceilings, repeating rectangular openings, unsettling liminal office architecture, no people, entirely indoors, no windows, no city, no street, no castle"

// ShouldUpsamplePrompt reports whether a provider's own prompt expander should
// run. A sparse idea benefits from it; a prompt the user already wrote in
// detail should reach the model as written, because provider upsampling tends
// to drift away from a specific request.
func ShouldUpsamplePrompt(prompt string) bool {
	brief := intent.Interpret(prompt)
	if brief.Setting != "" && brief.Action != "" {
		return false
	}
	return len(strings.Fields(strings.TrimSpace(prompt))) < 8
}
