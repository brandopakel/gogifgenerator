package unity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRejectsNon63Project(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "ProjectSettings"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "ProjectSettings", "ProjectVersion.txt"), []byte("m_EditorVersion: 6000.2.1f1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{Project: project, Executable: "missing-unity"}); err == nil {
		t.Fatal("New() error = nil")
	}
}
