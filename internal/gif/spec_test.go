package gif

import "testing"

func TestNormalizeDefaults(t *testing.T) {
	spec, err := (Spec{}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if spec.Width != 480 || spec.Height != 480 || spec.Motion != "orbit" {
		t.Fatalf("Normalize() = %#v", spec)
	}
}

func TestNormalizeRejectsUnsafeDimensions(t *testing.T) {
	_, err := (Spec{Width: 4096}).Normalize()
	if err == nil {
		t.Fatal("Normalize() expected an error")
	}
}

func TestNormalizeRejectsInvalidPalette(t *testing.T) {
	_, err := (Spec{Palette: []string{"#000000", "nope", "#FFFFFF"}}).Normalize()
	if err == nil {
		t.Fatal("Normalize() expected an error")
	}
}
