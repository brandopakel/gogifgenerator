package render

import (
	"image"
	"image/color"
	"sort"

	"golang.org/x/image/draw"
)

const adaptiveImageColors = 254

type colorBin struct {
	red, green, blue uint64
	count            uint64
}

type colorSample struct {
	red, green, blue uint8
	count            uint64
}

type colorBox struct {
	start, end                 int
	count                      uint64
	minimumRed, maximumRed     uint8
	minimumGreen, maximumGreen uint8
	minimumBlue, maximumBlue   uint8
}

func palettedFrame(source image.Image) *image.Paletted {
	frame := image.NewPaletted(source.Bounds(), adaptivePalette(source, adaptiveImageColors))
	draw.FloydSteinberg.Draw(frame, frame.Rect, source, source.Bounds().Min)
	return frame
}

// adaptivePalette uses a bounded 5-bit RGB histogram and weighted median-cut
// boxes. Black and white are reserved for the caption background and glyphs.
// A source-derived palette avoids the colored noise that a fixed system
// palette introduces in photos, video, and rendered gradients.
func adaptivePalette(source image.Image, maximum int) color.Palette {
	maximum = max(1, min(adaptiveImageColors, maximum))
	bins := make([]colorBin, 1<<15)
	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			red := uint8(uint16(pixel.R) * uint16(pixel.A) / 255)
			green := uint8(uint16(pixel.G) * uint16(pixel.A) / 255)
			blue := uint8(uint16(pixel.B) * uint16(pixel.A) / 255)
			index := int(red>>3)<<10 | int(green>>3)<<5 | int(blue>>3)
			bin := &bins[index]
			bin.red += uint64(red)
			bin.green += uint64(green)
			bin.blue += uint64(blue)
			bin.count++
		}
	}

	samples := make([]colorSample, 0, len(bins))
	for _, bin := range bins {
		if bin.count == 0 {
			continue
		}
		samples = append(samples, colorSample{
			red: uint8(bin.red / bin.count), green: uint8(bin.green / bin.count), blue: uint8(bin.blue / bin.count), count: bin.count,
		})
	}
	if len(samples) == 0 {
		return color.Palette{color.Black, color.White}
	}

	boxes := []colorBox{measureColorBox(samples, 0, len(samples))}
	for len(boxes) < maximum {
		boxIndex := mostUsefulColorBox(boxes)
		if boxIndex < 0 {
			break
		}
		box := boxes[boxIndex]
		channel := widestColorChannel(box)
		sort.Slice(samples[box.start:box.end], func(left, right int) bool {
			return colorComponent(samples[box.start+left], channel) < colorComponent(samples[box.start+right], channel)
		})
		half := box.count / 2
		var accumulated uint64
		split := box.start + 1
		for index := box.start; index < box.end-1; index++ {
			accumulated += samples[index].count
			if accumulated >= half {
				split = index + 1
				break
			}
		}
		boxes[boxIndex] = measureColorBox(samples, box.start, split)
		boxes = append(boxes, measureColorBox(samples, split, box.end))
	}

	result := make(color.Palette, 0, len(boxes)+2)
	result = append(result, color.RGBA{A: 255})
	for _, box := range boxes {
		var red, green, blue, count uint64
		for _, sample := range samples[box.start:box.end] {
			red += uint64(sample.red) * sample.count
			green += uint64(sample.green) * sample.count
			blue += uint64(sample.blue) * sample.count
			count += sample.count
		}
		result = append(result, color.RGBA{
			R: uint8(red / count), G: uint8(green / count), B: uint8(blue / count), A: 255,
		})
	}
	result = append(result, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	return result
}

func measureColorBox(samples []colorSample, start, end int) colorBox {
	box := colorBox{
		start: start, end: end,
		minimumRed: 255, minimumGreen: 255, minimumBlue: 255,
	}
	for _, sample := range samples[start:end] {
		box.count += sample.count
		box.minimumRed = min(box.minimumRed, sample.red)
		box.maximumRed = max(box.maximumRed, sample.red)
		box.minimumGreen = min(box.minimumGreen, sample.green)
		box.maximumGreen = max(box.maximumGreen, sample.green)
		box.minimumBlue = min(box.minimumBlue, sample.blue)
		box.maximumBlue = max(box.maximumBlue, sample.blue)
	}
	return box
}

func mostUsefulColorBox(boxes []colorBox) int {
	selected := -1
	var selectedScore uint64
	for index, box := range boxes {
		if box.end-box.start < 2 {
			continue
		}
		rangeSize := uint64(max(
			box.maximumRed-box.minimumRed,
			box.maximumGreen-box.minimumGreen,
			box.maximumBlue-box.minimumBlue,
		))
		score := rangeSize * box.count
		if selected < 0 || score > selectedScore {
			selected, selectedScore = index, score
		}
	}
	return selected
}

func widestColorChannel(box colorBox) int {
	redRange := box.maximumRed - box.minimumRed
	greenRange := box.maximumGreen - box.minimumGreen
	blueRange := box.maximumBlue - box.minimumBlue
	if greenRange >= redRange && greenRange >= blueRange {
		return 1
	}
	if blueRange >= redRange {
		return 2
	}
	return 0
}

func colorComponent(sample colorSample, channel int) uint8 {
	switch channel {
	case 1:
		return sample.green
	case 2:
		return sample.blue
	default:
		return sample.red
	}
}
