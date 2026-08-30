package modelgen

import (
	"errors"
	"strings"
	"testing"
)

func TestRequestValidation(t *testing.T) {
	for _, request := range []Request{{}, {Prompt: "ok", Recipe: strings.Repeat("x", 65)}, {Prompt: strings.Repeat("x", 1025)}} {
		if err := request.Validate(); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Validate(%#v) error = %v", request, err)
		}
	}
	if err := (Request{Prompt: "a detailed mechanical bird", Recipe: "tripo-3.1"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
