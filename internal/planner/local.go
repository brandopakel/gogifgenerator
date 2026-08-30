package planner

import (
	"context"
	"hash/fnv"
	"strings"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

var localPalettes = [][]string{
	{"#10131A", "#E8FF47", "#FF5C8A", "#70D6FF", "#F7F7F2"},
	{"#171219", "#FF8A00", "#FFC900", "#E04B8D", "#FFF4DA"},
	{"#071E22", "#1D7874", "#6EEB83", "#FFB800", "#F4C095"},
	{"#140F2D", "#2C2A4A", "#4F518C", "#907AD6", "#DABFFF"},
	{"#0D1B2A", "#1B263B", "#415A77", "#E0E1DD", "#FF6B6B"},
}

var motions = []string{"orbit", "pulse", "waves", "confetti"}

// Local is fast, free, offline, and deterministic for the same prompt and seed.
type Local struct{}

func (Local) Plan(_ context.Context, request Request) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(request.Prompt))))
	seed := int64(h.Sum64())
	if request.Seed != 0 {
		seed ^= request.Seed
	}

	caption := strings.TrimSpace(request.Prompt)
	if runes := []rune(caption); len(runes) > 32 {
		caption = string(runes[:32])
	}
	spec := gifdomain.Defaults()
	spec.Caption = caption
	spec.Palette = append([]string(nil), localPalettes[uint64(seed)%uint64(len(localPalettes))]...)
	spec.Motion = motions[(uint64(seed)/uint64(len(localPalettes)))%uint64(len(motions))]
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
	normalized, err := spec.Normalize()
	if err != nil {
		return Result{}, err
	}
	return Result{Spec: normalized, Engine: "local"}, nil
}
