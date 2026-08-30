// Package render converts a validated animation spec into a GIF using only the
// Go standard library. This keeps the core portable and easy to deploy.
package render

import (
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"math"
	"math/rand"
	"strconv"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

func GIF(w io.Writer, input gifdomain.Spec) error {
	spec, err := input.Normalize()
	if err != nil {
		return err
	}
	palette, err := parsePalette(spec.Palette)
	if err != nil {
		return err
	}
	animation := &gif.GIF{
		Image:     make([]*image.Paletted, 0, spec.Frames),
		Delay:     make([]int, 0, spec.Frames),
		Disposal:  make([]byte, 0, spec.Frames),
		LoopCount: 0,
	}
	for frameNumber := 0; frameNumber < spec.Frames; frameNumber++ {
		frame := image.NewPaletted(image.Rect(0, 0, spec.Width, spec.Height), palette)
		drawBackground(frame, frameNumber, spec.Frames)
		switch spec.Motion {
		case "orbit":
			drawOrbit(frame, frameNumber, spec.Frames)
		case "pulse":
			drawPulse(frame, frameNumber, spec.Frames)
		case "waves":
			drawWaves(frame, frameNumber, spec.Frames)
		case "confetti":
			drawConfetti(frame, frameNumber, spec.Frames, spec.Seed)
		}
		drawCaption(frame, spec.Caption, frameNumber, spec.Frames)
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, max(2, spec.DelayMS/10))
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	return gif.EncodeAll(w, animation)
}

func parsePalette(values []string) (color.Palette, error) {
	palette := make(color.Palette, 0, len(values))
	for _, value := range values {
		r, err := strconv.ParseUint(value[1:3], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("parse palette: %w", err)
		}
		g, _ := strconv.ParseUint(value[3:5], 16, 8)
		b, _ := strconv.ParseUint(value[5:7], 16, 8)
		palette = append(palette, color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255})
	}
	return palette, nil
}

func drawBackground(frame *image.Paletted, frameNumber, frames int) {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	colors := len(frame.Palette)
	phase := float64(frameNumber) / float64(frames)
	for y := 0; y < height; y++ {
		band := int(float64(y)/float64(height)*4+phase*2) % max(1, colors-1)
		index := uint8(band)
		for x := 0; x < width; x++ {
			frame.SetColorIndex(x, y, index)
		}
	}
	// A sparse checker texture keeps flat areas lively without inflating files.
	for y := 0; y < height; y += 18 {
		for x := (y/18)%2*9 + frameNumber%6; x < width; x += 18 {
			frame.SetColorIndex(x, y, uint8((int(frame.ColorIndexAt(x, y))+1)%colors))
		}
	}
}

func drawOrbit(frame *image.Paletted, frameNumber, frames int) {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	angle := 2 * math.Pi * float64(frameNumber) / float64(frames)
	radius := float64(min(width, height)) * 0.23
	cx := width/2 + int(math.Cos(angle)*radius)
	cy := height/2 + int(math.Sin(angle)*radius)
	blob := max(24, min(width, height)/7)
	drawCircle(frame, cx, cy, blob, 2%uint8(len(frame.Palette)))
	drawCircle(frame, width-cx, height-cy, max(12, blob/2), 3%uint8(len(frame.Palette)))
	drawCircleOutline(frame, width/2, height/2, int(radius), max(2, width/120), uint8(len(frame.Palette)-1))
}

func drawPulse(frame *image.Paletted, frameNumber, frames int) {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	unit := min(width, height)
	phase := float64(frameNumber) / float64(frames)
	for ring := 0; ring < 4; ring++ {
		progress := math.Mod(phase+float64(ring)/4, 1)
		radius := int(progress * float64(unit) * 0.48)
		thickness := max(2, unit/70)
		drawCircleOutline(frame, width/2, height/2, radius, thickness, uint8((ring+1)%len(frame.Palette)))
	}
	centerRadius := int(float64(unit) * (0.08 + 0.025*math.Sin(2*math.Pi*phase)))
	drawCircle(frame, width/2, height/2, centerRadius, uint8(len(frame.Palette)-1))
}

func drawWaves(frame *image.Paletted, frameNumber, frames int) {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	phase := 2 * math.Pi * float64(frameNumber) / float64(frames)
	for wave := 0; wave < 4; wave++ {
		baseY := height * (wave + 1) / 5
		amplitude := float64(height) * (0.045 + float64(wave)*0.008)
		for x := 0; x < width; x++ {
			y := baseY + int(math.Sin(float64(x)/float64(width)*3*math.Pi+phase+float64(wave))*amplitude)
			for thickness := -max(2, height/100); thickness <= max(2, height/100); thickness++ {
				set(frame, x, y+thickness, uint8((wave+1)%len(frame.Palette)))
			}
		}
	}
}

func drawConfetti(frame *image.Paletted, frameNumber, frames int, seed int64) {
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	rng := rand.New(rand.NewSource(seed))
	count := max(28, width*height/6000)
	for i := 0; i < count; i++ {
		x := rng.Intn(width)
		startY := rng.Intn(height)
		speed := 2 + rng.Intn(7)
		y := (startY + frameNumber*speed*height/frames) % height
		size := max(3, min(width, height)/80+rng.Intn(max(2, min(width, height)/45)))
		colorIndex := uint8(1 + rng.Intn(max(1, len(frame.Palette)-1)))
		drawRect(frame, x, y, x+size, y+max(2, size/2), colorIndex)
	}
}

func drawCircle(frame *image.Paletted, cx, cy, radius int, index uint8) {
	radiusSquared := radius * radius
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= radiusSquared {
				set(frame, x, y, index)
			}
		}
	}
}

func drawCircleOutline(frame *image.Paletted, cx, cy, radius, thickness int, index uint8) {
	outer := radius * radius
	innerRadius := max(0, radius-thickness)
	inner := innerRadius * innerRadius
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			dx, dy := x-cx, y-cy
			distance := dx*dx + dy*dy
			if distance <= outer && distance >= inner {
				set(frame, x, y, index)
			}
		}
	}
}

func drawRect(frame *image.Paletted, x0, y0, x1, y1 int, index uint8) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			set(frame, x, y, index)
		}
	}
}

func set(frame *image.Paletted, x, y int, index uint8) {
	if image.Pt(x, y).In(frame.Rect) {
		frame.SetColorIndex(x, y, index)
	}
}
