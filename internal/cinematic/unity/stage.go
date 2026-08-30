// Package unity invokes a Unity 6.3 project in batch mode to produce a portable
// motion contract plus transparent animation/VFX frames.
package unity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
)

type Options struct {
	Executable string
	Project    string
	Method     string
	Timeout    time.Duration
}

func New(options Options) (cinematic.Stage, error) {
	project, version, err := validateProject(options.Project)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Method) == "" {
		options.Method = "GoGIF.Editor.BatchRenderer.Render"
	}
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "Unity"
	}
	return cinematic.NewCommandStage(cinematic.CommandStageOptions{
		Descriptor: cinematic.StageDescriptor{
			ID: "unity-6.3", Label: "Unity 6.3", Role: "portable motion and transparent VFX", Version: version,
		},
		Executable: options.Executable,
		Directory:  project,
		Timeout:    options.Timeout,
		Arguments: func(job cinematic.Job) []string {
			return []string{
				"-batchmode", "-quit", "-projectPath", project,
				"-executeMethod", options.Method, "-gogifManifest", job.ManifestPath, "-logFile", "-",
			}
		},
		Validate: func(job cinematic.Job) error {
			if err := cinematic.ValidatePNGSequence(job.Manifest.Paths.UnityFrames, job.Manifest.Frames, job.Manifest.Width, job.Manifest.Height); err != nil {
				return fmt.Errorf("validate VFX sequence: %w", err)
			}
			data, err := os.ReadFile(job.Manifest.Paths.UnityMotion)
			if err != nil || len(data) == 0 || len(data) > 1<<20 {
				return errors.New("Unity did not create a bounded motion contract")
			}
			var motion struct {
				Version int               `json:"version"`
				Frames  []json.RawMessage `json:"frames"`
			}
			if json.Unmarshal(data, &motion) != nil || motion.Version != cinematic.ManifestVersion || len(motion.Frames) != job.Manifest.Frames {
				return errors.New("Unity motion contract is invalid")
			}
			return nil
		},
	})
}

func validateProject(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("%w: GOGIF_UNITY_PROJECT is required", cinematic.ErrUnavailable)
	}
	project, err := filepath.Abs(value)
	if err != nil {
		return "", "", fmt.Errorf("resolve Unity project: %w", err)
	}
	info, err := os.Stat(project)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("%w: Unity project does not exist", cinematic.ErrUnavailable)
	}
	data, err := os.ReadFile(filepath.Join(project, "ProjectSettings", "ProjectVersion.txt"))
	if err != nil {
		return "", "", errors.New("Unity project is missing ProjectSettings/ProjectVersion.txt")
	}
	version := ""
	for _, line := range strings.Split(string(data), "\n") {
		if candidate, ok := strings.CutPrefix(strings.TrimSpace(line), "m_EditorVersion:"); ok {
			version = strings.TrimSpace(candidate)
			break
		}
	}
	if !strings.HasPrefix(version, "6000.3") {
		return "", "", fmt.Errorf("Unity project must target 6000.3; found %q", version)
	}
	return project, version, nil
}
