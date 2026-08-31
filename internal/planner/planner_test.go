package planner

import "testing"

func TestRequestAcceptsDocumentedGenerationModes(t *testing.T) {
	for _, mode := range []string{"", "fast", "semantic", "studio"} {
		if err := (Request{Prompt: "test", GenerationMode: mode}).Validate(); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
	if err := (Request{Prompt: "test", GenerationMode: "surprise"}).Validate(); err == nil {
		t.Fatal("unknown generation mode was accepted")
	}
}
