package sceneworker

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateRenderedFrameRejectsUnrealPlaceholder(t *testing.T) {
	referenceFilename := filepath.Join(t.TempDir(), "reference.png")
	reference := visibleTestFrame()
	writeTestPNG(t, referenceFilename, reference)
	filename := filepath.Join(t.TempDir(), "placeholder.png")
	frame := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			shade := uint8(220)
			if (x/16+y/16)%2 == 0 {
				shade = 232
			}
			frame.SetNRGBA(x, y, color.NRGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}
	writeTestPNG(t, filename, frame)
	if err := validateRenderedFrame(filename, referenceFilename, 256, 256); err == nil {
		t.Fatal("expected placeholder frame to be rejected")
	}
}

func TestValidateRenderedFrameAcceptsVisibleScene(t *testing.T) {
	referenceFilename := filepath.Join(t.TempDir(), "reference.png")
	reference := visibleTestFrame()
	writeTestPNG(t, referenceFilename, reference)
	filename := filepath.Join(t.TempDir(), "scene.png")
	frame := visibleTestFrame()
	writeTestPNG(t, filename, frame)
	if err := validateRenderedFrame(filename, referenceFilename, 256, 256); err != nil {
		t.Fatalf("visible scene rejected: %v", err)
	}
}

func TestValidateRenderedFrameRejectsUnrelatedWorld(t *testing.T) {
	referenceFilename := filepath.Join(t.TempDir(), "reference.png")
	writeTestPNG(t, referenceFilename, visibleTestFrame())
	filename := filepath.Join(t.TempDir(), "world.png")
	frame := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			frame.SetNRGBA(x, y, color.NRGBA{R: 165, G: uint8(190 + y/16), B: 232, A: 255})
		}
	}
	writeTestPNG(t, filename, frame)
	if err := validateRenderedFrame(filename, referenceFilename, 256, 256); err == nil {
		t.Fatal("expected unrelated default world to be rejected")
	}
}

func visibleTestFrame() *image.NRGBA {
	frame := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			frame.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y / 3), B: uint8(255 - x), A: 255})
		}
	}
	return frame
}

func writeTestPNG(t *testing.T, filename string, frame image.Image) {
	t.Helper()
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, frame); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
