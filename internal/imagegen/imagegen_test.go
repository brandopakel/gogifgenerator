package imagegen

import "testing"

func TestRequestValidate(t *testing.T) {
	valid := Request{
		Prompt: "turn this into a celebratory poster",
		Width:  512,
		Height: 512,
		Inputs: []Input{{Data: []byte("image"), ContentType: "image/png", SourceID: "wikimedia:42"}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	invalid := valid
	invalid.Inputs[0].ContentType = "video/mp4"
	if err := invalid.Validate(); err == nil {
		t.Fatal("Validate() error = nil for non-image input")
	}
}
