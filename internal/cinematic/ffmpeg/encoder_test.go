package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
)

func TestEncoderCreatesOneFramePerInput(t *testing.T) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}
	encoder, err := New(Options{Executable: path})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for index := range 4 {
		frame := image.NewNRGBA(image.Rect(0, 0, 128, 128))
		for y := range 128 {
			for x := range 128 {
				frame.SetNRGBA(x, y, color.NRGBA{R: uint8(x + index*24), G: uint8(y), B: uint8(index * 50), A: 255})
			}
		}
		file, err := os.Create(filepath.Join(directory, fmt.Sprintf("frame-%04d.png", index)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, frame); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := encoder.Encode(context.Background(), cinematic.FrameSequence{
		Directory: directory, Pattern: "frame-%04d.png", Width: 128, Height: 128, Frames: 4, DelayMS: 70,
	})
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(animation.Image) != 4 || animation.Config.Width != 128 || animation.Config.Height != 128 {
		t.Fatalf("animation = %#v, %v", animation, err)
	}
}
