package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
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

func TestEditedImageGIFAppliesEditorOptions(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 40, 30))
	draw.Draw(source, source.Bounds(), &image.Uniform{C: color.RGBA{R: 200, G: 40, B: 80, A: 255}}, image.Point{}, draw.Src)
	spec := gifdomain.Defaults()
	spec.Width, spec.Height, spec.Frames = 128, 128, 4
	spec.Caption = "top caption"
	var output bytes.Buffer
	if err := EditedImageGIF(&output, source, spec, EditOptions{
		CropX: .5, CropY: -.5, Zoom: 1.4, CaptionPosition: "top", Loop: false,
	}); err != nil {
		t.Fatalf("EditedImageGIF() error = %v", err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != 4 || decoded.LoopCount != -1 {
		t.Fatalf("frames = %d; loop = %d", len(decoded.Image), decoded.LoopCount)
	}
}

func TestEditedGIFBoundsFrameCount(t *testing.T) {
	source := &gif.GIF{Config: image.Config{Width: 20, Height: 20}, LoopCount: 0}
	for frameNumber := range 8 {
		frame := image.NewPaletted(image.Rect(0, 0, 20, 20), color.Palette{color.Black, color.White})
		frame.SetColorIndex(frameNumber, frameNumber, 1)
		source.Image = append(source.Image, frame)
		source.Delay = append(source.Delay, 5)
		source.Disposal = append(source.Disposal, gif.DisposalNone)
	}
	spec := gifdomain.Defaults()
	spec.Width, spec.Height, spec.Frames = 128, 128, 4
	var output bytes.Buffer
	if err := EditedGIF(&output, source, spec, EditOptions{Zoom: 1, CaptionPosition: "bottom", Loop: true}); err != nil {
		t.Fatalf("EditedGIF() error = %v", err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != 4 || decoded.LoopCount != 0 {
		t.Fatalf("frames = %d; loop = %d", len(decoded.Image), decoded.LoopCount)
	}
}

func TestEditedImageGIFRejectsInvalidEditorOptions(t *testing.T) {
	var output bytes.Buffer
	err := EditedImageGIF(&output, image.NewRGBA(image.Rect(0, 0, 10, 10)), gifdomain.Defaults(), EditOptions{Zoom: 4})
	if err == nil {
		t.Fatal("EditedImageGIF() expected an error")
	}
}

func TestAdaptivePalettePreservesSmoothColorDetail(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 128, 64))
	for y := range source.Bounds().Dy() {
		for x := range source.Bounds().Dx() {
			source.SetNRGBA(x, y, color.NRGBA{
				R: uint8(40 + x), G: uint8(30 + y*2), B: uint8(210 - x), A: 255,
			})
		}
	}
	frame := palettedFrame(source)
	if len(frame.Palette) < 128 {
		t.Fatalf("adaptive palette contains %d colors", len(frame.Palette))
	}
	var squaredError uint64
	for y := range source.Bounds().Dy() {
		for x := range source.Bounds().Dx() {
			expected := source.NRGBAAt(x, y)
			actual := color.NRGBAModel.Convert(frame.At(x, y)).(color.NRGBA)
			for _, difference := range []int{int(expected.R) - int(actual.R), int(expected.G) - int(actual.G), int(expected.B) - int(actual.B)} {
				squaredError += uint64(difference * difference)
			}
		}
	}
	meanSquaredError := squaredError / uint64(source.Bounds().Dx()*source.Bounds().Dy()*3)
	if meanSquaredError > 80 {
		t.Fatalf("mean squared color error = %d", meanSquaredError)
	}
}
