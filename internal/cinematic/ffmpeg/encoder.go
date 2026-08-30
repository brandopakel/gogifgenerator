// Package ffmpeg encodes a validated PNG frame sequence into a high-quality
// looping GIF with a per-animation palette.
package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"image/gif"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
)

type Options struct {
	Executable string
	Timeout    time.Duration
}

type Encoder struct {
	executable string
	timeout    time.Duration
}

func New(options Options) (*Encoder, error) {
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		executable = "ffmpeg"
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("%w: find FFmpeg: %v", cinematic.ErrUnavailable, err)
	}
	if options.Timeout <= 0 {
		options.Timeout = 2 * time.Minute
	}
	return &Encoder{executable: resolved, timeout: options.Timeout}, nil
}

func (e *Encoder) Descriptor() cinematic.StageDescriptor {
	return cinematic.StageDescriptor{
		ID: "ffmpeg", Label: "FFmpeg", Role: "palette and GIF encoding", Local: true, Available: e != nil && e.executable != "",
	}
}

func (e *Encoder) Encode(ctx context.Context, sequence cinematic.FrameSequence) ([]byte, error) {
	if e == nil || e.executable == "" {
		return nil, errors.New("FFmpeg is not configured")
	}
	if sequence.Width < 1 || sequence.Height < 1 || sequence.Frames < 1 || sequence.Frames > 48 || sequence.DelayMS < 20 || sequence.DelayMS > 1000 {
		return nil, errors.New("invalid frame sequence")
	}
	if filepath.Base(sequence.Pattern) != sequence.Pattern || !strings.Contains(sequence.Pattern, "%04d") {
		return nil, errors.New("frame pattern must be a local %04d filename")
	}
	for index := range sequence.Frames {
		filename := strings.ReplaceAll(sequence.Pattern, "%04d", fmt.Sprintf("%04d", index))
		path := filepath.Join(sequence.Directory, filename)
		if info, err := os.Stat(path); err != nil || info.IsDir() || info.Size() == 0 || info.Size() > 32<<20 {
			return nil, fmt.Errorf("frame %d is missing or exceeds safe bounds", index)
		}
	}
	directory, err := os.MkdirTemp("", "gogif-ffmpeg-encode-")
	if err != nil {
		return nil, fmt.Errorf("create encoder workspace: %w", err)
	}
	defer os.RemoveAll(directory)
	outputPath := filepath.Join(directory, "output.gif")
	fps := 1000.0 / float64(sequence.DelayMS)
	filter := "split[base][palette];[palette]palettegen=stats_mode=full[colors];[base][colors]paletteuse=dither=sierra2_4a:diff_mode=rectangle"
	encodeContext, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	command := exec.CommandContext(encodeContext, e.executable,
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-framerate", strconv.FormatFloat(fps, 'f', 6, 64), "-start_number", "0",
		"-i", filepath.Join(sequence.Directory, sequence.Pattern),
		"-frames:v", strconv.Itoa(sequence.Frames), "-filter_complex", filter, "-loop", "0", outputPath,
	)
	var diagnostics limitedBuffer
	command.Stderr = &diagnostics
	if err := command.Run(); err != nil {
		if encodeContext.Err() != nil {
			return nil, fmt.Errorf("encode timed out: %w", encodeContext.Err())
		}
		return nil, fmt.Errorf("encode frame sequence: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read encoded GIF: %w", err)
	}
	if len(data) == 0 || len(data) > cinematic.MaxOutputBytes {
		return nil, errors.New("encoded GIF exceeds safe bounds")
	}
	file, err := os.Open(outputPath)
	if err != nil {
		return nil, err
	}
	configuration, err := gif.DecodeConfig(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil || configuration.Width != sequence.Width || configuration.Height != sequence.Height {
		return nil, errors.New("encoded GIF dimensions do not match the frame sequence")
	}
	return data, nil
}

type limitedBuffer struct{ data []byte }

func (b *limitedBuffer) Write(data []byte) (int, error) {
	const limit = 16 << 10
	if remaining := limit - len(b.data); remaining > 0 {
		b.data = append(b.data, data[:min(len(data), remaining)]...)
	}
	return len(data), nil
}

func (b *limitedBuffer) String() string { return string(b.data) }
