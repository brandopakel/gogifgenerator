package blender

import (
	"errors"
	"testing"
)

func TestNewRejectsMissingExecutable(t *testing.T) {
	_, err := New(Options{Executable: "gogif-definitely-missing-blender"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New() error = %v", err)
	}
}

func TestCappedBuffer(t *testing.T) {
	var buffer cappedBuffer
	input := make([]byte, (64<<10)+500)
	written, err := buffer.Write(input)
	if err != nil || written != len(input) || len(buffer.data) != 64<<10 {
		t.Fatalf("Write() = %d, %v; retained = %d", written, err, len(buffer.data))
	}
}
