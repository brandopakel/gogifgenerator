package unreal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRejectsNonUE5Project(t *testing.T) {
	directory := t.TempDir()
	project := filepath.Join(directory, "GoGIF.uproject")
	script := filepath.Join(directory, "render.py")
	if err := os.WriteFile(project, []byte(`{"FileVersion":3,"EngineAssociation":"4.27"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Project: project, Script: script, Executable: "missing-unreal"}); err == nil {
		t.Fatal("New() error = nil")
	}
}
