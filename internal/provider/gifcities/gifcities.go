// Package gifcities searches Internet Archive's GifCities index without
// copying its GeoCities GIF corpus into GoGIF.
package gifcities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const (
	defaultEndpoint  = "https://gifcities.archive.org/api/v1/gifsearch"
	defaultBlobBase  = "https://blob.gifcities.org/gifcities/"
	defaultUserAgent = "GoGIF/0.1 (https://github.com/brandopakel/gogifgenerator)"
	maxResponseBytes = 4 << 20
)

type Options struct {
	Endpoint  string
	BlobBase  string
	UserAgent string
	Client    *http.Client
}

// GifCities serializes upstream searches from this process. Repeated searches
// are cached by provider.Cached when the application composes the provider.
type GifCities struct {
	endpoint  *url.URL
	blobBase  *url.URL
	userAgent string
	client    *http.Client
	gate      chan struct{}
}

func New(options Options) (*GifCities, error) {
	if options.Endpoint == "" {
		options.Endpoint = defaultEndpoint
	}
	endpoint, err := absoluteHTTPURL(options.Endpoint)
	if err != nil {
		return nil, errors.New("gifcities: endpoint must be an absolute HTTP(S) URL")
	}
	if options.BlobBase == "" {
		options.BlobBase = defaultBlobBase
	}
	blobBase, err := absoluteHTTPURL(options.BlobBase)
	if err != nil {
		return nil, errors.New("gifcities: blob base must be an absolute HTTP(S) URL")
	}
	if options.UserAgent == "" {
		options.UserAgent = defaultUserAgent
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 12 * time.Second}
	}
	return &GifCities{
		endpoint: endpoint, blobBase: blobBase, userAgent: options.UserAgent,
		client: options.Client, gate: make(chan struct{}, 1),
	}, nil
}

func (g *GifCities) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "gifcities", Label: "GifCities"}
}

func (g *GifCities) Search(ctx context.Context, query provider.Query) (provider.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return provider.Page{}, err
	}
	if query.Cursor != "" {
		return provider.Page{}, fmt.Errorf("%w: GifCities API does not expose a cursor", provider.ErrInvalidQuery)
	}

	select {
	case g.gate <- struct{}{}:
		defer func() { <-g.gate }()
	case <-ctx.Done():
		return provider.Page{}, ctx.Err()
	}

	requestURL := *g.endpoint
	params := requestURL.Query()
	params.Set("q", query.Text)
	requestURL.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return provider.Page{}, fmt.Errorf("gifcities: build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", g.userAgent)
	response, err := g.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return provider.Page{}, ctx.Err()
		}
		return provider.Page{}, fmt.Errorf("%w: GifCities request: %v", provider.ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return provider.Page{}, fmt.Errorf("%w: GifCities returned HTTP %d", provider.ErrUnavailable, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return provider.Page{}, fmt.Errorf("%w: read GifCities response: %v", provider.ErrUnavailable, err)
	}
	if len(data) > maxResponseBytes {
		return provider.Page{}, fmt.Errorf("%w: GifCities response exceeded %d bytes", provider.ErrUnavailable, maxResponseBytes)
	}
	var payload []apiItem
	if err := json.Unmarshal(data, &payload); err != nil {
		return provider.Page{}, fmt.Errorf("%w: decode GifCities response: %v", provider.ErrUnavailable, err)
	}

	page := provider.Page{Provider: g.Descriptor().ID, Results: make([]provider.Result, 0, min(query.Limit, len(payload)))}
	for _, item := range payload {
		result, ok := g.normalize(item)
		if !ok {
			continue
		}
		page.Results = append(page.Results, result)
		if len(page.Results) == query.Limit {
			break
		}
	}
	return page, nil
}

type apiItem struct {
	URLText  string `json:"url_text"`
	GIF      string `json:"gif"`
	Checksum string `json:"checksum"`
	Height   int    `json:"height"`
	Width    int    `json:"width"`
	Page     string `json:"page"`
}

func (g *GifCities) normalize(item apiItem) (provider.Result, bool) {
	checksum := strings.TrimSpace(item.Checksum)
	if !validChecksum(checksum) || item.Width < 0 || item.Height < 0 || !allowedArchivedPage(item.Page) {
		return provider.Result{}, false
	}
	blobURL := *g.blobBase
	blobURL.Path = path.Join(strings.TrimSuffix(blobURL.Path, "/"), checksum+".gif")
	if !sameOrigin(&blobURL, g.blobBase) {
		return provider.Result{}, false
	}
	title := strings.Join(strings.Fields(item.URLText), " ")
	if title == "" {
		title = "Archived GeoCities GIF"
	}
	mediaURL := blobURL.String()
	return provider.Result{
		Provider: "gifcities", ExternalID: checksum, Title: title, Kind: media.KindGIF,
		SourceURL: item.Page, PreviewURL: mediaURL, OriginalURL: mediaURL,
		ContentType: "image/gif", Width: item.Width, Height: item.Height,
		Renditions: []provider.Rendition{{
			Name: "original", Format: "gif", ContentType: "image/gif", URL: mediaURL,
			Width: item.Width, Height: item.Height,
		}},
		AllowedHandling: []provider.HandlingMode{provider.HandlingLink, provider.HandlingDisplay},
		Attribution:     "GifCities · Internet Archive",
		Restrictions: []string{
			"copyright and license are not supplied by GifCities",
			"link to the archived GeoCities source page",
		},
		CommercialUse:   media.PermissionUnknown,
		Derivatives:     media.PermissionUnknown,
		TransformPolicy: provider.TransformReview,
	}, true
}

func absoluteHTTPURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("not an absolute HTTP(S) URL")
	}
	return parsed, nil
}

func validChecksum(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if !((char >= 'A' && char <= 'Z') || (char >= '2' && char <= '7')) {
			return false
		}
	}
	return true
}

func allowedArchivedPage(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), "web.archive.org")
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}
