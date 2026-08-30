package render

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

func TestGIFProducesDecodableAnimation(t *testing.T) {
	motions := []string{"orbit", "pulse", "waves", "confetti"}
	for _, motion := range motions {
		t.Run(motion, func(t *testing.T) {
			spec := gifdomain.Defaults()
			spec.Width = 128
			spec.Height = 128
			spec.Frames = 4
			spec.Motion = motion
			var output bytes.Buffer
			if err := GIF(&output, spec); err != nil {
				t.Fatalf("GIF() error = %v", err)
			}
			animation, err := gif.DecodeAll(bytes.NewReader(output.Bytes()))
			if err != nil {
				t.Fatalf("DecodeAll() error = %v", err)
			}
			if len(animation.Image) != spec.Frames {
				t.Fatalf("frames = %d, want %d", len(animation.Image), spec.Frames)
			}
			if got := animation.Image[0].Bounds().Dx(); got != spec.Width {
				t.Fatalf("width = %d, want %d", got, spec.Width)
			}
		})
	}
}

func TestImageGIFProducesDecodableAnimation(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 32, 20))
	for y := range 20 {
		for x := range 32 {
			source.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 11), B: 140, A: 255})
		}
	}
	spec := gifdomain.Defaults()
	spec.Width = 128
	spec.Height = 128
	spec.Frames = 4
	var output bytes.Buffer
	if err := ImageGIF(&output, source, spec); err != nil {
		t.Fatalf("ImageGIF() error = %v", err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("DecodeAll() error = %v", err)
	}
	if len(animation.Image) != spec.Frames || animation.Image[0].Bounds().Dx() != spec.Width {
		t.Fatalf("animation = %d frames at %dx%d", len(animation.Image), animation.Image[0].Bounds().Dx(), animation.Image[0].Bounds().Dy())
	}
}
