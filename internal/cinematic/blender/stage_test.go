package blender

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/cinematic"
)

func TestStageCreatesPreviewAndPortableAssetWhenBlenderIsInstalled(t *testing.T) {
	executable, err := exec.LookPath("blender")
	if err != nil {
		t.Skip("Blender is not installed")
	}
	workspace := t.TempDir()
	outputDirectory := filepath.Join(workspace, "blender")
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := cinematic.Manifest{
		Version: 1, Prompt: "polished orbit", Width: 128, Height: 128, Frames: 4, DelayMS: 70, Seed: 42, Motion: "orbit",
		Paths: cinematic.ManifestPaths{
			BlenderAsset: filepath.Join(outputDirectory, "asset.fbx"), BlenderPreview: filepath.Join(outputDirectory, "preview.png"),
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stage, err := New(Options{Executable: executable, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Run(context.Background(), cinematic.Job{Workspace: workspace, ManifestPath: manifestPath, Manifest: manifest}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{manifest.Paths.BlenderAsset, manifest.Paths.BlenderPreview} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("output %s = %#v, %v", path, info, err)
		}
	}
}
