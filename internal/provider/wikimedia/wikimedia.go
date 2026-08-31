// Package wikimedia searches Wikimedia Commons and normalizes its rights
// metadata into GoGIF's provider-neutral result format.
package wikimedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const (
	defaultEndpoint  = "https://commons.wikimedia.org/w/api.php"
	defaultUserAgent = "GoGIF/0.1 (https://github.com/brandopakel/gogifgenerator)"
	maxResponseBytes = 4 << 20
)

type Options struct {
	Endpoint  string
	UserAgent string
	Client    *http.Client
}

// Commons serializes calls from this process as requested by Wikimedia's API
// etiquette. Repeated searches are cached by provider.Cached at composition.
type Commons struct {
	endpoint  *url.URL
	userAgent string
	client    *http.Client
	gate      chan struct{}
}

func New(options Options) (*Commons, error) {
	if options.Endpoint == "" {
		options.Endpoint = defaultEndpoint
	}
	endpoint, err := url.Parse(options.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, errors.New("wikimedia: endpoint must be an absolute URL")
	}
	if options.UserAgent == "" {
		options.UserAgent = defaultUserAgent
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 12 * time.Second}
	}
	return &Commons{
		endpoint:  endpoint,
		userAgent: options.UserAgent,
		client:    options.Client,
		gate:      make(chan struct{}, 1),
	}, nil
}

func (c *Commons) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "wikimedia", Label: "Wikimedia Commons"}
}

func (c *Commons) Search(ctx context.Context, query provider.Query) (provider.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return provider.Page{}, err
	}
	offset, err := parseCursor(query.Cursor)
	if err != nil {
		return provider.Page{}, err
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("generator", "search")
	params.Set("gsrsearch", mediaSearchQuery(query.Text))
	params.Set("gsrnamespace", "6")
	params.Set("gsrwhat", "text")
	params.Set("gsrinfo", "totalhits|suggestion|rewrittenquery")
	params.Set("gsrenablerewrites", "1")
	params.Set("mediasearch", "true")
	params.Set("uselang", query.Locale)
	params.Set("gsrlimit", strconv.Itoa(query.Limit))
	if offset > 0 {
		params.Set("gsroffset", strconv.Itoa(offset))
	}
	imageInfoParams(params, query.Locale)
	payload, err := c.query(ctx, params)
	if err != nil {
		return provider.Page{}, err
	}

	page := provider.Page{Provider: c.Descriptor().ID, Results: make([]provider.Result, 0, len(payload.Query.Pages))}
	if payload.Continue.Offset > 0 {
		page.Cursor = strconv.Itoa(payload.Continue.Offset)
	}
	for _, item := range payload.Query.Pages {
		if result, ok := normalize(item); ok {
			page.Results = append(page.Results, result)
		}
	}
	return page, nil
}

func mediaSearchQuery(query string) string {
	return "filetype:bitmap|drawing|video " + strings.TrimSpace(query)
}

func (c *Commons) Resolve(ctx context.Context, externalID, locale string) (provider.Result, error) {
	pageID, err := strconv.ParseInt(strings.TrimSpace(externalID), 10, 64)
	if err != nil || pageID < 1 {
		return provider.Result{}, fmt.Errorf("%w: invalid Wikimedia page ID", provider.ErrInvalidQuery)
	}
	if locale == "" {
		locale = "en"
	}
	if len(locale) > 16 {
		return provider.Result{}, fmt.Errorf("%w: locale is too long", provider.ErrInvalidQuery)
	}
	params := url.Values{}
	params.Set("action", "query")
	params.Set("pageids", strconv.FormatInt(pageID, 10))
	imageInfoParams(params, locale)
	payload, err := c.query(ctx, params)
	if err != nil {
		return provider.Result{}, err
	}
	for _, item := range payload.Query.Pages {
		if item.PageID != pageID {
			continue
		}
		if result, ok := normalize(item); ok {
			return result, nil
		}
	}
	return provider.Result{}, provider.ErrNotFound
}

func imageInfoParams(params url.Values, locale string) {
	params.Set("prop", "imageinfo")
	params.Set("iiprop", "url|mime|thumbmime|size|mediatype|extmetadata")
	params.Set("iiurlwidth", "480")
	params.Set("iiextmetadatalanguage", locale)
}

func (c *Commons) query(ctx context.Context, params url.Values) (apiResponse, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return apiResponse{}, ctx.Err()
	}
	requestURL := *c.endpoint
	params.Set("format", "json")
	params.Set("formatversion", "2")
	requestURL.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return apiResponse{}, fmt.Errorf("wikimedia: build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return apiResponse{}, ctx.Err()
		}
		return apiResponse{}, fmt.Errorf("%w: wikimedia request: %v", provider.ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return apiResponse{}, fmt.Errorf("%w: wikimedia returned HTTP %d", provider.ErrUnavailable, response.StatusCode)
	}
	var payload apiResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := decoder.Decode(&payload); err != nil {
		return apiResponse{}, fmt.Errorf("%w: decode wikimedia response: %v", provider.ErrUnavailable, err)
	}
	if payload.Error.Code != "" {
		return apiResponse{}, fmt.Errorf("%w: wikimedia %s: %s", provider.ErrUnavailable, payload.Error.Code, payload.Error.Info)
	}
	return payload, nil
}

type apiResponse struct {
	Continue struct {
		Offset int `json:"gsroffset"`
	} `json:"continue"`
	Query struct {
		Pages []apiPage `json:"pages"`
	} `json:"query"`
	Error struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

type apiPage struct {
	PageID    int64       `json:"pageid"`
	Title     string      `json:"title"`
	ImageInfo []imageInfo `json:"imageinfo"`
}

type imageInfo struct {
	URL         string                   `json:"url"`
	Description string                   `json:"descriptionurl"`
	ThumbURL    string                   `json:"thumburl"`
	ThumbWidth  int                      `json:"thumbwidth"`
	ThumbHeight int                      `json:"thumbheight"`
	MIME        string                   `json:"mime"`
	ThumbMIME   string                   `json:"thumbmime"`
	MediaType   string                   `json:"mediatype"`
	Width       int                      `json:"width"`
	Height      int                      `json:"height"`
	Size        int64                    `json:"size"`
	ExtMetadata map[string]metadataValue `json:"extmetadata"`
}

type metadataValue struct {
	Value any `json:"value"`
}

func parseCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 || offset > 100000 {
		return 0, fmt.Errorf("%w: invalid Wikimedia cursor", provider.ErrInvalidQuery)
	}
	return offset, nil
}

func normalize(item apiPage) (provider.Result, bool) {
	if len(item.ImageInfo) == 0 {
		return provider.Result{}, false
	}
	info := item.ImageInfo[0]
	kind, ok := mediaKind(info.MIME, info.MediaType)
	if !ok {
		return provider.Result{}, false
	}
	preview := info.ThumbURL
	previewMIME := info.ThumbMIME
	if preview == "" {
		preview = info.URL
		previewMIME = info.MIME
	}
	if !allowedURL(info.Description, "commons.wikimedia.org") ||
		!allowedURL(info.URL, "upload.wikimedia.org") ||
		!allowedURL(preview, "upload.wikimedia.org") {
		return provider.Result{}, false
	}

	licenseID := metadata(info.ExtMetadata, "License")
	licenseName := metadata(info.ExtMetadata, "LicenseShortName")
	commercial, derivatives, shareAlike, transformPolicy := classifyLicense(licenseID, licenseName)
	author := metadata(info.ExtMetadata, "Artist")
	credit := metadata(info.ExtMetadata, "Credit")
	title := strings.TrimPrefix(strings.TrimSpace(item.Title), "File:")
	attribution := buildAttribution(title, author, credit, licenseName)
	referenceURL := info.URL
	if !supportedReferenceMIME(info.MIME) && supportedReferenceMIME(previewMIME) {
		referenceURL = preview
	} else if !supportedReferenceMIME(info.MIME) {
		transformPolicy = provider.TransformReview
	}
	if kind != media.KindImage && kind != media.KindGIF {
		transformPolicy = provider.TransformReview
	}
	allowedHandling := []provider.HandlingMode{provider.HandlingLink, provider.HandlingDisplay}
	if transformPolicy == provider.TransformAllowed && derivatives == media.PermissionAllowed {
		allowedHandling = append(allowedHandling, provider.HandlingTemporaryTransform)
	}
	renditions := []provider.Rendition{{
		Name: "original", Format: formatForMIME(info.MIME), ContentType: info.MIME,
		URL: info.URL, Width: info.Width, Height: info.Height, SizeBytes: info.Size,
	}}
	if preview != info.URL {
		renditions = append([]provider.Rendition{{
			Name: "preview", Format: formatForMIME(previewMIME), ContentType: previewMIME,
			URL: preview, Width: info.ThumbWidth, Height: info.ThumbHeight,
		}}, renditions...)
	}

	return provider.Result{
		Provider:        "wikimedia",
		ExternalID:      strconv.FormatInt(item.PageID, 10),
		Title:           title,
		Description:     metadata(info.ExtMetadata, "ImageDescription"),
		Kind:            kind,
		SourceURL:       info.Description,
		PreviewURL:      preview,
		OriginalURL:     info.URL,
		ReferenceURL:    referenceURL,
		ContentType:     firstNonEmpty(previewMIME, info.MIME),
		Width:           info.Width,
		Height:          info.Height,
		SizeBytes:       info.Size,
		Renditions:      renditions,
		AllowedHandling: allowedHandling,
		Author:          author,
		Attribution:     attribution,
		LicenseID:       licenseID,
		LicenseName:     licenseName,
		LicenseURL:      safeLicenseURL(metadata(info.ExtMetadata, "LicenseUrl")),
		Restrictions:    restrictions(metadata(info.ExtMetadata, "Restrictions")),
		CommercialUse:   commercial,
		Derivatives:     derivatives,
		ShareAlike:      shareAlike,
		TransformPolicy: transformPolicy,
	}, true
}

func formatForMIME(value string) string {
	if _, format, ok := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "/"); ok {
		return format
	}
	return ""
}

func supportedReferenceMIME(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func mediaKind(mime, mediaType string) (media.Kind, bool) {
	mime = strings.ToLower(strings.TrimSpace(mime))
	mediaType = strings.ToUpper(strings.TrimSpace(mediaType))
	switch {
	case mime == "image/gif":
		return media.KindGIF, true
	case strings.HasPrefix(mime, "image/") || mediaType == "BITMAP" || mediaType == "DRAWING":
		return media.KindImage, true
	case strings.HasPrefix(mime, "video/") || mediaType == "VIDEO":
		return media.KindVideo, true
	default:
		return "", false
	}
}

func classifyLicense(id, name string) (media.Permission, media.Permission, bool, provider.TransformPolicy) {
	license := strings.ToLower(strings.Join([]string{id, name}, " "))
	license = strings.NewReplacer("_", "-", " ", "-").Replace(license)
	switch {
	case strings.Contains(license, "public-domain"), strings.Contains(license, "public domain"), strings.Contains(license, "cc0"):
		return media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed
	case strings.Contains(license, "by-nc-nd"):
		return media.PermissionProhibited, media.PermissionProhibited, false, provider.TransformReference
	case strings.Contains(license, "by-nd"):
		return media.PermissionAllowed, media.PermissionProhibited, false, provider.TransformReference
	case strings.Contains(license, "by-nc-sa"):
		return media.PermissionProhibited, media.PermissionAllowed, true, provider.TransformReview
	case strings.Contains(license, "by-nc"):
		return media.PermissionProhibited, media.PermissionAllowed, false, provider.TransformReview
	case strings.Contains(license, "by-sa"):
		return media.PermissionAllowed, media.PermissionAllowed, true, provider.TransformAllowed
	case strings.Contains(license, "cc-by"), strings.Contains(license, "cc by"):
		return media.PermissionAllowed, media.PermissionAllowed, false, provider.TransformAllowed
	default:
		return media.PermissionUnknown, media.PermissionUnknown, false, provider.TransformReview
	}
}

func metadata(values map[string]metadataValue, key string) string {
	value, ok := values[key]
	if !ok || value.Value == nil {
		return ""
	}
	return cleanHTML(fmt.Sprint(value.Value))
}

func cleanHTML(value string) string {
	var output strings.Builder
	insideTag := false
	for _, char := range value {
		switch char {
		case '<':
			insideTag = true
			output.WriteByte(' ')
		case '>':
			insideTag = false
			output.WriteByte(' ')
		default:
			if !insideTag {
				output.WriteRune(char)
			}
		}
	}
	return strings.Join(strings.FieldsFunc(html.UnescapeString(output.String()), unicode.IsSpace), " ")
}

func buildAttribution(title, author, credit, license string) string {
	parts := make([]string, 0, 4)
	for _, part := range []string{title, author, credit, license} {
		part = strings.TrimSpace(part)
		if part != "" && !containsFold(parts, part) {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " · ")
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}

func restrictions(value string) []string {
	fields := strings.FieldsFunc(value, func(char rune) bool { return char == '|' || char == ',' || char == ';' })
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			result = append(result, field)
		}
	}
	return result
}

func safeLicenseURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func allowedURL(value, host string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Hostname(), host)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
