// Package reference safely fetches short-lived generation inputs from known
// media providers. It never promotes provider bytes into managed storage.
package reference

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/imagegen"
	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	_ "golang.org/x/image/webp"
)

var (
	ErrNotTransformable = errors.New("reference: source is not approved for transformation")
	ErrUntrustedSource  = errors.New("reference: source host is not trusted")
	ErrUnsupportedMedia = errors.New("reference: media type is not supported")
	ErrTooLarge         = errors.New("reference: source is too large")
)

const (
	defaultMaxBytes = 20 << 20
	maxDimension    = 8192
	maxPixels       = 32 * 1024 * 1024
	maxRedirects    = 3
)

type Options struct {
	Client       *http.Client
	TempDir      string
	MaxBytes     int64
	AllowedHosts map[string][]string
	UserAgent    string
}

type Fetcher struct {
	client       *http.Client
	tempDir      string
	maxBytes     int64
	allowedHosts map[string]map[string]struct{}
	userAgent    string
}

func New(options Options) (*Fetcher, error) {
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if options.TempDir == "" {
		options.TempDir = os.TempDir()
	}
	absoluteTempDir, err := filepath.Abs(options.TempDir)
	if err != nil {
		return nil, fmt.Errorf("reference: resolve temporary directory: %w", err)
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = defaultMaxBytes
	}
	if options.MaxBytes < 1 || options.MaxBytes > imagegen.MaxInputBytes {
		return nil, fmt.Errorf("reference: max bytes must be between 1 and %d", imagegen.MaxInputBytes)
	}
	if options.AllowedHosts == nil {
		options.AllowedHosts = map[string][]string{
			"wikimedia": {"upload.wikimedia.org"},
		}
	}
	allowedHosts := make(map[string]map[string]struct{}, len(options.AllowedHosts))
	for providerID, hosts := range options.AllowedHosts {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" || len(hosts) == 0 {
			return nil, errors.New("reference: every provider allowlist needs an ID and host")
		}
		allowedHosts[providerID] = make(map[string]struct{}, len(hosts))
		for _, host := range hosts {
			host = strings.ToLower(strings.TrimSpace(host))
			if host == "" || strings.ContainsAny(host, "/:") {
				return nil, fmt.Errorf("reference: invalid allowed host %q", host)
			}
			allowedHosts[providerID][host] = struct{}{}
		}
	}
	if options.UserAgent == "" {
		options.UserAgent = "GoGIF/0.2 (https://github.com/brandopakel/gogifgenerator)"
	}
	return &Fetcher{
		client:       options.Client,
		tempDir:      absoluteTempDir,
		maxBytes:     options.MaxBytes,
		allowedHosts: allowedHosts,
		userAgent:    options.UserAgent,
	}, nil
}

type File struct {
	path        string
	contentType string
	sourceID    string
	size        int64
	digest      string
	closeOnce   sync.Once
	closeErr    error
}

func (f *File) Path() string        { return f.path }
func (f *File) ContentType() string { return f.contentType }
func (f *File) Size() int64         { return f.size }
func (f *File) SHA256() string      { return f.digest }

func (f *File) Input() (imagegen.Input, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return imagegen.Input{}, fmt.Errorf("reference: read temporary input: %w", err)
	}
	return imagegen.Input{Data: data, ContentType: f.contentType, SourceID: f.sourceID}, nil
}

func (f *File) Close() error {
	f.closeOnce.Do(func() {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			f.closeErr = fmt.Errorf("reference: remove temporary input: %w", err)
		}
	})
	return f.closeErr
}

func (f *Fetcher) Fetch(ctx context.Context, result provider.Result) (*File, error) {
	if result.TransformPolicy != provider.TransformAllowed || result.Derivatives != media.PermissionAllowed {
		return nil, ErrNotTransformable
	}
	allowed, ok := f.allowedHosts[result.Provider]
	if !ok {
		return nil, ErrUntrustedSource
	}
	referenceURL := result.ReferenceURL
	if referenceURL == "" {
		referenceURL = result.OriginalURL
	}
	sourceURL, err := url.Parse(referenceURL)
	if err != nil || !trustedURL(sourceURL, allowed) {
		return nil, ErrUntrustedSource
	}
	if result.Kind != media.KindImage && result.Kind != media.KindGIF {
		return nil, ErrUnsupportedMedia
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("reference: build request: %w", err)
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp")
	request.Header.Set("User-Agent", f.userAgent)
	client := *f.client
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("reference: too many redirects")
		}
		if !trustedURL(request.URL, allowed) {
			return ErrUntrustedSource
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("reference: fetch source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, fmt.Errorf("reference: source returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > f.maxBytes {
		return nil, ErrTooLarge
	}

	prefix := make([]byte, 512)
	prefixLength, readErr := io.ReadFull(response.Body, prefix)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("reference: inspect source: %w", readErr)
	}
	prefix = prefix[:prefixLength]
	contentType := http.DetectContentType(prefix)
	if !supportedContentType(contentType) {
		return nil, ErrUnsupportedMedia
	}
	if headerType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err == nil && headerType != "" && !supportedContentType(headerType) {
		return nil, ErrUnsupportedMedia
	}

	temporary, err := os.CreateTemp(f.tempDir, "gogif-reference-*")
	if err != nil {
		return nil, fmt.Errorf("reference: create temporary input: %w", err)
	}
	path := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()
	reader := io.LimitReader(io.MultiReader(bytes.NewReader(prefix), response.Body), f.maxBytes+1)
	size, err := io.Copy(io.MultiWriter(temporary, hash), reader)
	if err != nil {
		return nil, fmt.Errorf("reference: write temporary input: %w", err)
	}
	if size > f.maxBytes {
		return nil, ErrTooLarge
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reference: inspect temporary input: %w", err)
	}
	configuration, _, err := image.DecodeConfig(temporary)
	if err != nil {
		return nil, ErrUnsupportedMedia
	}
	pixels := int64(configuration.Width) * int64(configuration.Height)
	if configuration.Width < 1 || configuration.Height < 1 || configuration.Width > maxDimension || configuration.Height > maxDimension || pixels > maxPixels {
		return nil, fmt.Errorf("%w: image dimensions exceed the safety limit", ErrTooLarge)
	}
	if err := temporary.Close(); err != nil {
		return nil, fmt.Errorf("reference: close temporary input: %w", err)
	}
	keep = true
	return &File{
		path:        path,
		contentType: contentType,
		sourceID:    result.Provider + ":" + result.ExternalID,
		size:        size,
		digest:      hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func trustedURL(candidate *url.URL, allowed map[string]struct{}) bool {
	if candidate == nil || candidate.Scheme != "https" || candidate.User != nil {
		return false
	}
	_, ok := allowed[strings.ToLower(candidate.Hostname())]
	return ok
}

func supportedContentType(value string) bool {
	value, _, _ = mime.ParseMediaType(value)
	switch value {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
