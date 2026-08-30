package render

import (
	"image"
	"math"
	"strings"
)

// glyphs is a deliberately tiny 5x7 bitmap font. The generated GIF remains
// self-contained and the renderer has no font files or native dependencies.
var glyphs = map[rune][7]uint8{
	'A': {14, 17, 17, 31, 17, 17, 17}, 'B': {30, 17, 17, 30, 17, 17, 30},
	'C': {14, 17, 16, 16, 16, 17, 14}, 'D': {30, 17, 17, 17, 17, 17, 30},
	'E': {31, 16, 16, 30, 16, 16, 31}, 'F': {31, 16, 16, 30, 16, 16, 16},
	'G': {14, 17, 16, 23, 17, 17, 15}, 'H': {17, 17, 17, 31, 17, 17, 17},
	'I': {31, 4, 4, 4, 4, 4, 31}, 'J': {7, 2, 2, 2, 18, 18, 12},
	'K': {17, 18, 20, 24, 20, 18, 17}, 'L': {16, 16, 16, 16, 16, 16, 31},
	'M': {17, 27, 21, 21, 17, 17, 17}, 'N': {17, 25, 21, 19, 17, 17, 17},
	'O': {14, 17, 17, 17, 17, 17, 14}, 'P': {30, 17, 17, 30, 16, 16, 16},
	'Q': {14, 17, 17, 17, 21, 18, 13}, 'R': {30, 17, 17, 30, 20, 18, 17},
	'S': {15, 16, 16, 14, 1, 1, 30}, 'T': {31, 4, 4, 4, 4, 4, 4},
	'U': {17, 17, 17, 17, 17, 17, 14}, 'V': {17, 17, 17, 17, 17, 10, 4},
	'W': {17, 17, 17, 21, 21, 21, 10}, 'X': {17, 17, 10, 4, 10, 17, 17},
	'Y': {17, 17, 10, 4, 4, 4, 4}, 'Z': {31, 1, 2, 4, 8, 16, 31},
	'0': {14, 17, 19, 21, 25, 17, 14}, '1': {4, 12, 4, 4, 4, 4, 14},
	'2': {14, 17, 1, 2, 4, 8, 31}, '3': {30, 1, 1, 14, 1, 1, 30},
	'4': {2, 6, 10, 18, 31, 2, 2}, '5': {31, 16, 16, 30, 1, 1, 30},
	'6': {14, 16, 16, 30, 17, 17, 14}, '7': {31, 1, 2, 4, 8, 8, 8},
	'8': {14, 17, 17, 14, 17, 17, 14}, '9': {14, 17, 17, 15, 1, 1, 14},
	'!': {4, 4, 4, 4, 4, 0, 4}, '?': {14, 17, 1, 2, 4, 0, 4},
	'.': {0, 0, 0, 0, 0, 0, 4}, ',': {0, 0, 0, 0, 0, 4, 8},
	'-': {0, 0, 0, 31, 0, 0, 0}, '+': {0, 4, 4, 31, 4, 4, 0},
	':': {0, 4, 0, 0, 4, 0, 0}, '#': {10, 31, 10, 10, 31, 10, 0},
	'&': {12, 18, 20, 8, 21, 18, 13}, '/': {1, 2, 2, 4, 8, 8, 16},
	' ': {},
}

func drawCaption(frame *image.Paletted, value string, frameNumber, frames int) {
	drawCaptionAt(frame, value, frameNumber, frames, "bottom")
}

func drawCaptionAt(frame *image.Paletted, value string, frameNumber, frames int, position string) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return
	}
	runes := []rune(value)
	width, height := frame.Bounds().Dx(), frame.Bounds().Dy()
	available := width - max(24, width/12)
	scale := min(6, max(1, available/max(1, len(runes)*6-1)))
	textWidth := (len(runes)*6 - 1) * scale
	textHeight := 7 * scale
	x := (width - textWidth) / 2
	bounce := int(math.Sin(2*math.Pi*float64(frameNumber)/float64(frames)) * float64(max(1, scale/2)))
	y := height - textHeight - max(18, height/14) + bounce
	switch position {
	case "top":
		y = max(18, height/14) + bounce
	case "middle":
		y = (height-textHeight)/2 + bounce
	}
	padding := max(6, scale*2)
	drawRect(frame, x-padding, y-padding, x+textWidth+padding, y+textHeight+padding, 0)
	foreground := uint8(len(frame.Palette) - 1)
	for _, character := range runes {
		glyph, ok := glyphs[character]
		if !ok {
			glyph = glyphs['?']
		}
		for row, bits := range glyph {
			for column := 0; column < 5; column++ {
				if bits&(1<<uint(4-column)) == 0 {
					continue
				}
				drawRect(frame, x+column*scale, y+row*scale, x+(column+1)*scale, y+(row+1)*scale, foreground)
			}
		}
		x += 6 * scale
	}
}
