package render

import (
	"image"
	"image/color"
	"testing"
)

func TestCaptionImageDrawsOnTrueColorFrame(t *testing.T) {
	frame := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	CaptionImage(frame, "SHIP IT", 0, 4, "bottom")
	changed := false
	for y := range 128 {
		for x := range 128 {
			if frame.NRGBAAt(x, y) != (color.NRGBA{}) {
				changed = true
				break
			}
		}
		if changed {
			break
		}
	}
	if !changed {
		t.Fatal("CaptionImage() left the frame unchanged")
	}
}
