package render

import (
	"bytes"
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
