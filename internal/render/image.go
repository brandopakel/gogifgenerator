package render

import (
	"errors"
	"image"
	"image/gif"
	"io"
	"math"

	gifdomain "github.com/brandopakel/gogifgenerator/internal/gif"
	"golang.org/x/image/draw"
)

// ImageGIF turns an original generated still into a lightweight animated GIF.
// A subtle crop/zoom follows the planner motion while the caption remains in
// GoGIF's portable bitmap type system.
func ImageGIF(w io.Writer, source image.Image, input gifdomain.Spec) error {
	return EditedImageGIF(w, source, input, EditOptions{Zoom: 1, CaptionPosition: "bottom", Loop: true})
}

// EditOptions contains user-directed crop and caption choices for uploaded
// media. CropX and CropY range from -1 to 1; Zoom ranges from 1 to 3.
type EditOptions struct {
	CropX           float64
	CropY           float64
	Zoom            float64
	CaptionPosition string
	Loop            bool
}

func (o EditOptions) normalize() (EditOptions, error) {
	if o.Zoom == 0 {
		o.Zoom = 1
	}
	if o.CaptionPosition == "" {
		o.CaptionPosition = "bottom"
	}
	if o.CropX < -1 || o.CropX > 1 || o.CropY < -1 || o.CropY > 1 {
		return EditOptions{}, errors.New("render: crop position must be between -1 and 1")
	}
	if o.Zoom < 1 || o.Zoom > 3 {
		return EditOptions{}, errors.New("render: zoom must be between 1 and 3")
	}
	if o.CaptionPosition != "top" && o.CaptionPosition != "middle" && o.CaptionPosition != "bottom" {
		return EditOptions{}, errors.New("render: caption position must be top, middle, or bottom")
	}
	return o, nil
}

// EditedImageGIF animates a still image with explicit editor choices.
func EditedImageGIF(w io.Writer, source image.Image, input gifdomain.Spec, options EditOptions) error {
	if source == nil || source.Bounds().Empty() {
		return errors.New("render: source image is empty")
	}
	spec, err := input.Normalize()
	if err != nil {
		return err
	}
	options, err = options.normalize()
	if err != nil {
		return err
	}
	animation := &gif.GIF{
		Image:     make([]*image.Paletted, 0, spec.Frames),
		Delay:     make([]int, 0, spec.Frames),
		Disposal:  make([]byte, 0, spec.Frames),
		LoopCount: loopCount(options.Loop),
	}
	for frameNumber := range spec.Frames {
		progress := float64(frameNumber) / float64(spec.Frames)
		zoom, panX, panY := imageMotion(spec.Motion, progress)
		trueColor := image.NewNRGBA(image.Rect(0, 0, spec.Width, spec.Height))
		drawCover(trueColor, source, zoom*options.Zoom, combinePan(options.CropX, panX), combinePan(options.CropY, panY))
		frame := palettedFrame(trueColor)
		if spec.Motion == "confetti" {
			drawConfetti(frame, frameNumber, spec.Frames, spec.Seed)
		}
		if spec.ShowPrompt {
			drawCaptionAt(frame, spec.Caption, frameNumber, spec.Frames, options.CaptionPosition)
		}
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, max(2, spec.DelayMS/10))
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	return gif.EncodeAll(w, animation)
}

// EditedGIF crops, captions, and re-times an uploaded GIF. It preserves the
// source animation while bounding the exported frame count through Spec.
func EditedGIF(w io.Writer, source *gif.GIF, input gifdomain.Spec, options EditOptions) error {
	if source == nil || len(source.Image) == 0 || source.Config.Width < 1 || source.Config.Height < 1 {
		return errors.New("render: source GIF is empty")
	}
	spec, err := input.Normalize()
	if err != nil {
		return err
	}
	options, err = options.normalize()
	if err != nil {
		return err
	}
	frameCount := min(len(source.Image), spec.Frames)
	if frameCount < 1 {
		return errors.New("render: source GIF has no frames")
	}
	composited := compositeGIF(source, frameCount)
	animation := &gif.GIF{
		Image: make([]*image.Paletted, 0, frameCount), Delay: make([]int, 0, frameCount),
		Disposal: make([]byte, 0, frameCount), LoopCount: loopCount(options.Loop),
	}
	for frameNumber := range frameCount {
		progress := float64(frameNumber) / float64(frameCount)
		zoom, panX, panY := imageMotion(spec.Motion, progress)
		trueColor := image.NewNRGBA(image.Rect(0, 0, spec.Width, spec.Height))
		drawCover(trueColor, composited[frameNumber], zoom*options.Zoom, combinePan(options.CropX, panX), combinePan(options.CropY, panY))
		frame := palettedFrame(trueColor)
		if spec.Motion == "confetti" {
			drawConfetti(frame, frameNumber, frameCount, spec.Seed)
		}
		if spec.ShowPrompt {
			drawCaptionAt(frame, spec.Caption, frameNumber, frameCount, options.CaptionPosition)
		}
		animation.Image = append(animation.Image, frame)
		animation.Delay = append(animation.Delay, max(2, spec.DelayMS/10))
		animation.Disposal = append(animation.Disposal, gif.DisposalNone)
	}
	return gif.EncodeAll(w, animation)
}

func compositeGIF(source *gif.GIF, outputFrames int) []image.Image {
	bounds := image.Rect(0, 0, source.Config.Width, source.Config.Height)
	canvas := image.NewNRGBA(bounds)
	result := make([]image.Image, 0, outputFrames)
	var restore *image.NRGBA
	nextOutput := 0
	for index, frame := range source.Image {
		if index > 0 && index-1 < len(source.Disposal) {
			switch source.Disposal[index-1] {
			case gif.DisposalBackground:
				draw.Draw(canvas, source.Image[index-1].Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				if restore != nil {
					draw.Draw(canvas, bounds, restore, image.Point{}, draw.Src)
				}
			}
		}
		if index < len(source.Disposal) && source.Disposal[index] == gif.DisposalPrevious {
			restore = cloneNRGBA(canvas)
		} else {
			restore = nil
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		if nextOutput < outputFrames && index == nextOutput*len(source.Image)/outputFrames {
			result = append(result, cloneNRGBA(canvas))
			nextOutput++
		}
	}
	return result
}

func cloneNRGBA(source *image.NRGBA) *image.NRGBA {
	clone := image.NewNRGBA(source.Bounds())
	copy(clone.Pix, source.Pix)
	return clone
}

func combinePan(base, motion float64) float64 {
	return max(-1.0, min(1.0, base+motion*(1-math.Abs(base))))
}

func loopCount(loop bool) int {
	if loop {
		return 0
	}
	return -1
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
	crop := image.Rect(
		max(sourceBounds.Min.X, int(math.Floor(startX))),
		max(sourceBounds.Min.Y, int(math.Floor(startY))),
		min(sourceBounds.Max.X, int(math.Ceil(startX+cropWidth))),
		min(sourceBounds.Max.Y, int(math.Ceil(startY+cropHeight))),
	)
	if crop.Dx() < 1 {
		crop.Max.X = min(sourceBounds.Max.X, crop.Min.X+1)
	}
	if crop.Dy() < 1 {
		crop.Max.Y = min(sourceBounds.Max.Y, crop.Min.Y+1)
	}
	draw.CatmullRom.Scale(destination, destination.Bounds(), source, crop, draw.Src, nil)
}
