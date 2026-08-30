// Package ffmpeg implements request-scoped short-video decoding with a local
// FFmpeg executable. FFmpeg is optional and is never downloaded by GoGIF.
package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/gif"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/video"
)

const (
	maxClipDurationMS  = 15_000
	maxClipStartMS     = 300_000
	maxFrames          = 48
	maxDecodedPixels   = 48_000_000
	maxDecodedGIFBytes = 64 << 20
)

type Options struct {
	Executable string
	TempDir    string
}

type Decoder struct {
	executable string
	tempDir    string
}

func New(options Options) (*Decoder, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "ffmpeg"
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("find FFmpeg executable: %w", err)
	}
	return &Decoder{executable: resolved, tempDir: options.TempDir}, nil
}

func (d *Decoder) Descriptor() video.Descriptor {
	return video.Descriptor{ID: "ffmpeg", Label: "Local FFmpeg", Local: true}
}

func (d *Decoder) Decode(ctx context.Context, request video.Request) (*gif.GIF, error) {
	if d == nil || d.executable == "" {
		return nil, errors.New("ffmpeg: decoder is not configured")
	}
	if len(request.Data) == 0 {
		return nil, errors.New("ffmpeg: video is empty")
	}
	if request.StartMS < 0 || request.StartMS > maxClipStartMS {
		return nil, fmt.Errorf("ffmpeg: trim start must be between 0 and %d milliseconds", maxClipStartMS)
	}
	if request.EndMS <= request.StartMS || request.EndMS-request.StartMS > maxClipDurationMS {
		return nil, fmt.Errorf("ffmpeg: clip duration must be between 1 and %d milliseconds", maxClipDurationMS)
	}
	if request.Frames < 1 || request.Frames > maxFrames {
		return nil, fmt.Errorf("ffmpeg: frames must be between 1 and %d", maxFrames)
	}

	directory, err := os.MkdirTemp(d.tempDir, "gogif-video-")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: create temporary directory: %w", err)
	}
	defer os.RemoveAll(directory)

	inputPath := filepath.Join(directory, "source"+safeExtension(request.Filename))
	outputPath := filepath.Join(directory, "decoded.gif")
	if err := os.WriteFile(inputPath, request.Data, 0o600); err != nil {
		return nil, fmt.Errorf("ffmpeg: write temporary input: %w", err)
	}
	durationMS := request.EndMS - request.StartMS
	fps := float64(request.Frames) * 1000 / float64(durationMS)
	filter := "scale=w='min(1024,iw)':h='min(1024,ih)':force_original_aspect_ratio=decrease,fps=" + strconv.FormatFloat(fps, 'f', 4, 64)
	decodeContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(decodeContext, d.executable,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-ss", milliseconds(request.StartMS), "-t", milliseconds(durationMS),
		"-i", inputPath, "-an", "-sn", "-dn", "-threads", "1",
		"-vf", filter, "-frames:v", strconv.Itoa(request.Frames), outputPath,
	)
	var diagnostics limitedBuffer
	command.Stdout = io.Discard
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		if decodeContext.Err() != nil {
			return nil, fmt.Errorf("ffmpeg: decode canceled: %w", decodeContext.Err())
		}
		return nil, fmt.Errorf("ffmpeg: decode video: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	information, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: inspect decoded animation: %w", err)
	}
	if information.Size() > maxDecodedGIFBytes {
		return nil, errors.New("ffmpeg: decoded animation exceeds safe byte bounds")
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: read decoded animation: %w", err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("ffmpeg: decode generated GIF: %w", err)
	}
	if len(animation.Image) == 0 || len(animation.Image) > request.Frames || animation.Config.Width < 1 || animation.Config.Height < 1 ||
		int64(animation.Config.Width)*int64(animation.Config.Height)*int64(len(animation.Image)) > maxDecodedPixels {
		return nil, errors.New("ffmpeg: decoded video exceeds safe frame bounds")
	}
	return animation, nil
}

func milliseconds(value int) string {
	return strconv.FormatFloat(float64(value)/1000, 'f', 3, 64)
}

func safeExtension(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".mov", ".m4v", ".webm":
		return strings.ToLower(filepath.Ext(filename))
	default:
		return ".video"
	}
}

type limitedBuffer struct {
	data []byte
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	const limit = 4096
	remaining := limit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, data[:min(len(data), remaining)]...)
	}
	return len(data), nil
}

func (b *limitedBuffer) String() string { return string(b.data) }
