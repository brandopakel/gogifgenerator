package cinematic

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
)

// ValidatePNGSequence verifies an engine-owned frame directory before another
// stage may consume it. Engine projects never get to choose paths supplied by
// an HTTP client; every path comes from GoGIF's private job manifest.
func ValidatePNGSequence(directory string, frames, width, height int) error {
	if frames < 1 || frames > 1800 || width < 1 || height < 1 {
		return errors.New("invalid expected frame sequence")
	}
	for index := range frames {
		path := filepath.Join(directory, fmt.Sprintf("frame-%04d.png", index))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 || info.Size() > 32<<20 {
			return fmt.Errorf("frame %d is missing or exceeds safe bounds", index)
		}
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open frame %d: %w", index, err)
		}
		configuration, decodeErr := png.DecodeConfig(file)
		closeErr := file.Close()
		if decodeErr != nil || closeErr != nil || configuration.Width != width || configuration.Height != height {
			return fmt.Errorf("frame %d is not a %dx%d PNG", index, width, height)
		}
	}
	return nil
}
