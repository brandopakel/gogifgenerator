// Package gif defines the renderer-independent description of an animation.
package gif

import (
	"errors"
	"fmt"
	"strings"
)

const (
	MinDimension = 128
	MaxDimension = 1024
	MinFrames    = 4
	MaxFrames    = 48
)

var allowedMotions = map[string]bool{
	"orbit":    true,
	"pulse":    true,
	"waves":    true,
	"confetti": true,
}

// Spec is the compact, validated contract shared by AI planners and renderers.
type Spec struct {
	Width      int      `json:"width"`
	Height     int      `json:"height"`
	Frames     int      `json:"frames"`
	DelayMS    int      `json:"delay_ms"`
	Caption    string   `json:"caption"`
	Palette    []string `json:"palette"`
	Motion     string   `json:"motion"`
	Seed       int64    `json:"seed"`
	ShowPrompt bool     `json:"show_prompt"`
}

// Defaults returns a safe square animation suitable for messaging apps.
func Defaults() Spec {
	return Spec{
		Width:      480,
		Height:     480,
		Frames:     18,
		DelayMS:    70,
		Caption:    "MAKE IT MOVE",
		Palette:    []string{"#111318", "#E8FF47", "#FF5C8A", "#70D6FF", "#F7F7F2"},
		Motion:     "orbit",
		Seed:       1,
		ShowPrompt: true,
	}
}

// Normalize fills optional values and rejects plans that could consume
// unreasonable memory or produce invalid GIFs.
func (s Spec) Normalize() (Spec, error) {
	d := Defaults()
	if s.Width == 0 {
		s.Width = d.Width
	}
	if s.Height == 0 {
		s.Height = d.Height
	}
	if s.Frames == 0 {
		s.Frames = d.Frames
	}
	if s.DelayMS == 0 {
		s.DelayMS = d.DelayMS
	}
	if strings.TrimSpace(s.Caption) == "" {
		s.Caption = d.Caption
	}
	s.Caption = strings.ToUpper(strings.TrimSpace(s.Caption))
	if len([]rune(s.Caption)) > 42 {
		s.Caption = string([]rune(s.Caption)[:42])
	}
	if len(s.Palette) == 0 {
		s.Palette = d.Palette
	}
	if s.Motion == "" {
		s.Motion = d.Motion
	}

	if s.Width < MinDimension || s.Width > MaxDimension || s.Height < MinDimension || s.Height > MaxDimension {
		return Spec{}, fmt.Errorf("dimensions must be between %d and %d pixels", MinDimension, MaxDimension)
	}
	if s.Frames < MinFrames || s.Frames > MaxFrames {
		return Spec{}, fmt.Errorf("frames must be between %d and %d", MinFrames, MaxFrames)
	}
	if s.DelayMS < 20 || s.DelayMS > 1000 {
		return Spec{}, errors.New("delay_ms must be between 20 and 1000")
	}
	if !allowedMotions[s.Motion] {
		return Spec{}, fmt.Errorf("unsupported motion %q", s.Motion)
	}
	if len(s.Palette) < 3 || len(s.Palette) > 8 {
		return Spec{}, errors.New("palette must contain between 3 and 8 colors")
	}
	for _, value := range s.Palette {
		if !validHexColor(value) {
			return Spec{}, fmt.Errorf("invalid palette color %q", value)
		}
	}
	return s, nil
}

func validHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
