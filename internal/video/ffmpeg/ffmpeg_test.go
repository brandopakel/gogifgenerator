package ffmpeg

import (
	"context"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/video"
)

func TestNewRejectsMissingExecutable(t *testing.T) {
	if _, err := New(Options{Executable: "/definitely/not/an/ffmpeg"}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestDecodeValidatesTrimBeforeStartingProcess(t *testing.T) {
	decoder := &Decoder{executable: "/definitely/not/an/ffmpeg"}
	_, err := decoder.Decode(context.Background(), video.Request{Data: []byte("video"), StartMS: 1000, EndMS: 1000, Frames: 4})
	if err == nil || !strings.Contains(err.Error(), "clip duration") {
		t.Fatalf("Decode() error = %v", err)
	}
}

func TestSafeExtension(t *testing.T) {
	if got := safeExtension("../../clip.MOV"); got != ".mov" {
		t.Fatalf("safeExtension() = %q", got)
	}
	if got := safeExtension("clip.sh"); got != ".video" {
		t.Fatalf("safeExtension() = %q", got)
	}
}
