package render

import (
	"errors"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"io"
	"math"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
)

// ImageGIF turns an original generated still into a lightweight animated GIF.
// A subtle crop/zoom follows the planner motion while the caption remains in
// GoGIF's portable bitmap type system.
func ImageGIF(w io.Writer, source image.Image, input gifdomain.Spec) error {
	if source == nil || source.Bounds().Empty() {
		return errors.New("render: source image is empty")
	}
	spec, err := input.Normalize()
	if err != nil {
		return err
	}
	outputPalette := imagePalette()
	animation := &gif.GIF{
		Image:     make([]*image.Paletted, 0, spec.Frames),
		Delay:     make([]int, 0, spec.Frames),
		Disposal:  make([]byte, 0, spec.Frames),
		LoopCount: 0,
	}
	for frameNumber := range spec.Frames {
		progress := float64(frameNumber) / float64(spec.Frames)
		zoom, panX, panY := imageMotion(spec.Motion, progress)
		trueColor := image.NewNRGBA(image.Rect(0, 0, spec.Width, spec.Height))
		drawCover(trueColor, source, zoom, panX, panY)
		frame := image.NewPaletted(trueColor.Bounds(), outputPalette)
		draw.FloydSteinberg.Draw(frame, frame.Rect, trueColor, image.Point{})
		if spec.Motion == "confetti" {
			drawConfetti(frame, frameNumber, spec.Frames, spec.Seed)
		}
		if spec.ShowPrompt {
			drawCaption(frame, spec.Caption, frameNumber, spec.Frames)
		}
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, max(2, spec.DelayMS/10))
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	return gif.EncodeAll(w, animation)
}

func imageMotion(motion string, progress float64) (zoom, panX, panY float64) {
	phase := 2 * math.Pi * progress
	switch motion {
	case "orbit":
		return 1.06, math.Cos(phase), math.Sin(phase)
	case "pulse":
		return 1.02 + 0.07*(0.5+0.5*math.Sin(phase)), 0, 0
	case "waves":
		return 1.06, math.Sin(phase), 0.35 * math.Sin(phase*2)
	case "confetti":
		return 1.035, 0.25 * math.Sin(phase), 0
	default:
		return 1, 0, 0
	}
}

func drawCover(destination *image.NRGBA, source image.Image, zoom, panX, panY float64) {
	sourceBounds := source.Bounds()
	sourceWidth, sourceHeight := float64(sourceBounds.Dx()), float64(sourceBounds.Dy())
	destinationWidth, destinationHeight := float64(destination.Bounds().Dx()), float64(destination.Bounds().Dy())
	destinationAspect := destinationWidth / destinationHeight
	cropWidth, cropHeight := sourceWidth, sourceHeight
	if sourceWidth/sourceHeight > destinationAspect {
		cropWidth = sourceHeight * destinationAspect
	} else {
		cropHeight = sourceWidth / destinationAspect
	}
	cropWidth /= zoom
	cropHeight /= zoom
	marginX, marginY := sourceWidth-cropWidth, sourceHeight-cropHeight
	startX := float64(sourceBounds.Min.X) + marginX*(panX+1)/2
	startY := float64(sourceBounds.Min.Y) + marginY*(panY+1)/2
	for y := 0; y < destination.Bounds().Dy(); y++ {
		sourceY := startY + (float64(y)+0.5)*cropHeight/destinationHeight
		for x := 0; x < destination.Bounds().Dx(); x++ {
			sourceX := startX + (float64(x)+0.5)*cropWidth/destinationWidth
			destination.Set(x, y, source.At(
				min(sourceBounds.Max.X-1, max(sourceBounds.Min.X, int(sourceX))),
				min(sourceBounds.Max.Y-1, max(sourceBounds.Min.Y, int(sourceY))),
			))
		}
	}
}

func imagePalette() color.Palette {
	result := make(color.Palette, 0, 256)
	result = append(result, color.RGBA{A: 255})
	result = append(result, palette.Plan9[:254]...)
	result = append(result, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	return result
}
