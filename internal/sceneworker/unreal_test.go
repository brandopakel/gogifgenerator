package sceneworker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteMotionCreatesBoundedPortableContract(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "motion.json")
	if err := writeMotion(filename, 48, 24); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var contract motionContract
	if json.Unmarshal(data, &contract) != nil || contract.Version != 1 || len(contract.Frames) != 48 || contract.Frames[47].Yaw != 360 {
		t.Fatalf("motion = %#v", contract)
	}
}

func TestBoundedReferenceDimensionsPreservesBounds(t *testing.T) {
	width, height := boundedReferenceDimensions(3840, 2160)
	if width != 2048 || height != 1152 {
		t.Fatalf("dimensions = %dx%d", width, height)
	}
}
