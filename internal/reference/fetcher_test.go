package reference

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

var tinyPNG = func() []byte {
	var output bytes.Buffer
	still := image.NewRGBA(image.Rect(0, 0, 2, 2))
	still.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&output, still); err != nil {
		panic(err)
	}
	return output.Bytes()
}()

func TestFetcherCreatesAndDeletesTemporaryReference(t *testing.T) {
	var userAgent string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG)
	}))
	defer server.Close()
	serverURL, _ := url.Parse(server.URL)
	fetcher, err := New(Options{
		Client:       server.Client(),
		TempDir:      t.TempDir(),
		AllowedHosts: map[string][]string{"test": {serverURL.Hostname()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := fetcher.Fetch(context.Background(), transformableResult(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	path := file.Path()
	if file.ContentType() != "image/png" || file.Size() != int64(len(tinyPNG)) || len(file.SHA256()) != 64 {
		t.Fatalf("file metadata = type %q, size %d, digest %q", file.ContentType(), file.Size(), file.SHA256())
	}
	input, err := file.Input()
	if err != nil || string(input.Data) != string(tinyPNG) || input.SourceID != "test:42" {
		t.Fatalf("Input() = %#v, %v", input, err)
	}
	if !strings.HasPrefix(userAgent, "GoGIF/") {
		t.Fatalf("User-Agent = %q", userAgent)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

func TestFetcherRejectsUnapprovedTransformationBeforeNetwork(t *testing.T) {
	fetcher, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	result := transformableResult("https://upload.wikimedia.org/file.png")
	result.TransformPolicy = provider.TransformReview
	if _, err := fetcher.Fetch(context.Background(), result); !errors.Is(err, ErrNotTransformable) {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestFetcherRejectsLargeOrUnsupportedResponses(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
		maxBytes    int64
		want        error
	}{
		{"too-large", "image/png", append(tinyPNG, make([]byte, 64)...), int64(len(tinyPNG)), ErrTooLarge},
		{"not-an-image", "text/plain", []byte("not an image"), 128, ErrUnsupportedMedia},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				_, _ = w.Write(test.body)
			}))
			defer server.Close()
			serverURL, _ := url.Parse(server.URL)
			fetcher, err := New(Options{Client: server.Client(), TempDir: t.TempDir(), MaxBytes: test.maxBytes, AllowedHosts: map[string][]string{"test": {serverURL.Hostname()}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fetcher.Fetch(context.Background(), transformableResult(server.URL)); !errors.Is(err, test.want) {
				t.Fatalf("Fetch() error = %v, want %v", err, test.want)
			}
		})
	}
}

func transformableResult(originalURL string) provider.Result {
	return provider.Result{
		Provider:        "test",
		ExternalID:      "42",
		Kind:            media.KindImage,
		OriginalURL:     originalURL,
		Derivatives:     media.PermissionAllowed,
		TransformPolicy: provider.TransformAllowed,
	}
}
