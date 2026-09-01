// Package unreal invokes an Unreal Engine 5 editor project to render the
// Blender asset with the portable motion contract authored by Unity.
package unreal

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
	Script     string
	Timeout    time.Duration
}

func New(options Options) (cinematic.Stage, error) {
	project, version, err := validateProject(options.Project)
	if err != nil {
		return nil, err
	}
	script, err := validateScript(options.Script)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.Executable) == "" {
		options.Executable = "UnrealEditor-Cmd"
	}
	return cinematic.NewCommandStage(cinematic.CommandStageOptions{
		Descriptor: cinematic.StageDescriptor{
			ID: "unreal-5", Label: "Unreal Engine 5", Role: "cinematic beauty rendering", Version: version,
		},
		Executable: options.Executable,
		Directory:  filepath.Dir(project),
		Timeout:    options.Timeout,
		Arguments: func(job cinematic.Job) []string {
			return []string{
				project, "-unattended", "-nop4", "-nosplash", "-NoSound", "-RenderOffscreen",
				fmt.Sprintf("-ResX=%d", job.Manifest.Width), fmt.Sprintf("-ResY=%d", job.Manifest.Height), "-ForceRes", "-windowed",
				"-ExecutePythonScript=" + script, "-gogifManifest=" + job.ManifestPath,
			}
		},
		Validate: func(job cinematic.Job) error {
			if _, err := os.Stat(job.Manifest.Paths.UnityMotion); err != nil {
				return errors.New("portable motion contract is missing")
			}
			if _, err := os.Stat(job.Manifest.Paths.BlenderAsset); err != nil {
				return errors.New("Blender FBX asset is missing")
			}
			return cinematic.ValidatePNGSequence(job.Manifest.Paths.UnrealFrames, job.Manifest.Frames, job.Manifest.Width, job.Manifest.Height)
		},
	})
}

func validateProject(value string) (string, string, error) {
	if strings.TrimSpace(value) == "" {
		return "", "", fmt.Errorf("%w: GOGIF_UNREAL_PROJECT is required", cinematic.ErrUnavailable)
	}
	project, err := filepath.Abs(value)
	if err != nil {
		return "", "", fmt.Errorf("resolve Unreal project: %w", err)
	}
	if filepath.Ext(project) != ".uproject" {
		return "", "", errors.New("Unreal project must be a .uproject file")
	}
	data, err := os.ReadFile(project)
	if err != nil {
		return "", "", fmt.Errorf("%w: Unreal project does not exist", cinematic.ErrUnavailable)
	}
	var metadata struct {
		EngineAssociation string `json:"EngineAssociation"`
	}
	if json.Unmarshal(data, &metadata) != nil {
		return "", "", errors.New("Unreal .uproject metadata is invalid")
	}
	version := strings.TrimSpace(metadata.EngineAssociation)
	if version != "" && !strings.HasPrefix(version, "5") {
		return "", "", fmt.Errorf("Unreal project must target Engine 5; found %q", version)
	}
	if version == "" {
		version = "5.x"
	}
	return project, version, nil
}

func validateScript(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: GOGIF_UNREAL_SCRIPT is required", cinematic.ErrUnavailable)
	}
	script, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve Unreal script: %w", err)
	}
	info, err := os.Stat(script)
	if err != nil || info.IsDir() || filepath.Ext(script) != ".py" {
		return "", fmt.Errorf("%w: Unreal Python render script does not exist", cinematic.ErrUnavailable)
	}
	return script, nil
}
