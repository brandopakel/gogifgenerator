// Package nasa searches NASA's Image and Video Library while preserving the
// agency's media-usage restrictions in GoGIF's provider-neutral result shape.
package nasa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const (
	defaultSearchEndpoint = "https://images-api.nasa.gov/search"
	defaultAssetBase      = "https://images-api.nasa.gov/asset/"
	defaultDetailsBase    = "https://images.nasa.gov/details/"
	defaultUserAgent      = "GoGIF/0.1 (https://github.com/brandopakel/gogifgenerator)"
	usageGuidelinesURL    = "https://www.nasa.gov/nasa-brand-center/images-and-media/"
	maxResponseBytes      = 6 << 20
	maxRenditions         = 12
)

var usageRestrictions = []string{
	"acknowledge NASA as the source without implying endorsement",
	"check the item for identified third-party copyright",
	"review logos, identifiable people, promotional use, and merchandise separately",
	"do not attribute an AI-generated output to NASA or imply NASA reviewed it",
}

type Options struct {
	SearchEndpoint string
	AssetBase      string
	DetailsBase    string
	UserAgent      string
	Client         *http.Client
}

type Library struct {
	searchEndpoint *url.URL
	assetBase      *url.URL
	detailsBase    *url.URL
	userAgent      string
	client         *http.Client
	gate           chan struct{}
}

func New(options Options) (*Library, error) {
	defaults := []struct {
		value    *string
		fallback string
		label    string
	}{
		{&options.SearchEndpoint, defaultSearchEndpoint, "search endpoint"},
		{&options.AssetBase, defaultAssetBase, "asset base"},
		{&options.DetailsBase, defaultDetailsBase, "details base"},
	}
	parsed := make([]*url.URL, 0, len(defaults))
	for _, candidate := range defaults {
		if *candidate.value == "" {
			*candidate.value = candidate.fallback
		}
		value, err := absoluteHTTPURL(*candidate.value)
		if err != nil {
			return nil, fmt.Errorf("nasa: %s must be an absolute HTTP(S) URL", candidate.label)
		}
		parsed = append(parsed, value)
	}
	if options.UserAgent == "" {
		options.UserAgent = defaultUserAgent
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 12 * time.Second}
	}
	return &Library{
		searchEndpoint: parsed[0], assetBase: parsed[1], detailsBase: parsed[2],
		userAgent: options.UserAgent, client: options.Client, gate: make(chan struct{}, 1),
	}, nil
}

func (l *Library) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "nasa", Label: "NASA Image and Video Library"}
}

func (l *Library) Search(ctx context.Context, query provider.Query) (provider.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return provider.Page{}, err
	}
	pageNumber, err := parseCursor(query.Cursor)
	if err != nil {
		return provider.Page{}, err
	}
	requestURL := *l.searchEndpoint
	params := requestURL.Query()
	params.Set("q", query.Text)
	params.Set("media_type", "image,video")
	params.Set("page_size", strconv.Itoa(query.Limit))
	params.Set("page", strconv.Itoa(pageNumber))
	requestURL.RawQuery = params.Encode()

	var payload collectionResponse
	if err := l.getJSON(ctx, requestURL.String(), &payload); err != nil {
		return provider.Page{}, err
	}
	page := provider.Page{Provider: l.Descriptor().ID, Results: make([]provider.Result, 0, len(payload.Collection.Items))}
	for _, item := range payload.Collection.Items {
		if result, ok := l.normalizeSearch(item); ok {
			page.Results = append(page.Results, result)
		}
	}
	if pageNumber*query.Limit < payload.Collection.Metadata.TotalHits || hasNext(payload.Collection.Links) {
		page.Cursor = strconv.Itoa(pageNumber + 1)
	}
	return page, nil
}

func (l *Library) Resolve(ctx context.Context, externalID, locale string) (provider.Result, error) {
	externalID = strings.TrimSpace(externalID)
	if !validNASAID(externalID) {
		return provider.Result{}, fmt.Errorf("%w: invalid NASA asset ID", provider.ErrInvalidQuery)
	}
	if locale == "" {
		locale = "en"
	}
	if len(locale) > 16 {
		return provider.Result{}, fmt.Errorf("%w: locale is too long", provider.ErrInvalidQuery)
	}

	requestURL := *l.searchEndpoint
	params := requestURL.Query()
	params.Set("nasa_id", externalID)
	params.Set("media_type", "image,video")
	params.Set("page_size", "10")
	requestURL.RawQuery = params.Encode()
	var search collectionResponse
	if err := l.getJSON(ctx, requestURL.String(), &search); err != nil {
		return provider.Result{}, err
	}
	var result provider.Result
	found := false
	for _, item := range search.Collection.Items {
		candidate, ok := l.normalizeSearch(item)
		if ok && candidate.ExternalID == externalID {
			result, found = candidate, true
			break
		}
	}
	if !found {
		return provider.Result{}, provider.ErrNotFound
	}

	var manifest collectionResponse
	if err := l.getJSON(ctx, joinedURL(l.assetBase, externalID), &manifest); err != nil {
		return provider.Result{}, err
	}
	result.Renditions = normalizeRenditions(manifest.Collection.Items, result.Kind)
	if len(result.Renditions) == 0 {
		return provider.Result{}, provider.ErrNotFound
	}
	primary := result.Renditions[0]
	result.OriginalURL = primary.URL
	result.ContentType = primary.ContentType
	return result, nil
}

func (l *Library) normalizeSearch(item collectionItem) (provider.Result, bool) {
	if len(item.Data) == 0 {
		return provider.Result{}, false
	}
	data := item.Data[0]
	externalID := strings.TrimSpace(data.NASAID.First())
	if !validNASAID(externalID) {
		return provider.Result{}, false
	}
	kind, ok := nasaMediaKind(data.MediaType.First())
	if !ok {
		return provider.Result{}, false
	}
	preview := ""
	for _, link := range item.Links {
		if normalized, safe := canonicalAssetURL(link.Href); strings.EqualFold(link.Rel, "preview") && strings.EqualFold(link.Render, "image") && safe {
			preview = normalized
			break
		}
	}
	if preview == "" {
		return provider.Result{}, false
	}
	title := cleanText(data.Title.First())
	if title == "" {
		title = externalID
	}
	description := cleanText(firstNonEmpty(data.Description.First(), data.Description508.First()))
	center := cleanText(data.Center.First())
	author := cleanText(firstNonEmpty(data.Photographer.First(), data.SecondaryCreator.First(), center))
	attribution := "NASA"
	if center != "" && !strings.EqualFold(center, "NASA") {
		attribution += " · " + center
	}
	if author != "" && !strings.EqualFold(author, center) && !strings.EqualFold(author, "NASA") {
		attribution += " · " + author
	}
	return provider.Result{
		Provider: "nasa", ExternalID: externalID, Title: title, Description: description, Kind: kind,
		SourceURL: joinedURL(l.detailsBase, externalID), PreviewURL: preview, OriginalURL: preview,
		ContentType:     contentTypeForPath(preview),
		AllowedHandling: []provider.HandlingMode{provider.HandlingLink, provider.HandlingDisplay},
		Author:          author, Attribution: attribution,
		LicenseID: "nasa-media-usage-guidelines", LicenseName: "NASA Media Usage Guidelines", LicenseURL: usageGuidelinesURL,
		Restrictions: append([]string(nil), usageRestrictions...), CommercialUse: media.PermissionUnknown,
		Derivatives: media.PermissionUnknown, TransformPolicy: provider.TransformReview,
	}, true
}

func (l *Library) getJSON(ctx context.Context, requestURL string, destination any) error {
	select {
	case l.gate <- struct{}{}:
		defer func() { <-l.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("nasa: build request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", l.userAgent)
	response, err := l.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: NASA request: %v", provider.ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return provider.ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("%w: NASA returned HTTP %d", provider.ErrUnavailable, response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: decode NASA response: %v", provider.ErrUnavailable, err)
	}
	return nil
}

type collectionResponse struct {
	Collection struct {
		Items    []collectionItem `json:"items"`
		Links    []collectionLink `json:"links"`
		Metadata struct {
			TotalHits int `json:"total_hits"`
		} `json:"metadata"`
	} `json:"collection"`
}

type collectionItem struct {
	Href  string           `json:"href"`
	Data  []assetData      `json:"data"`
	Links []collectionLink `json:"links"`
}

type collectionLink struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Render string `json:"render"`
}

type assetData struct {
	NASAID           textValue `json:"nasa_id"`
	Title            textValue `json:"title"`
	Description      textValue `json:"description"`
	Description508   textValue `json:"description_508"`
	MediaType        textValue `json:"media_type"`
	Center           textValue `json:"center"`
	Photographer     textValue `json:"photographer"`
	SecondaryCreator textValue `json:"secondary_creator"`
}

type textValue []string

func (v *textValue) UnmarshalJSON(data []byte) error {
	var single string
	if json.Unmarshal(data, &single) == nil {
		*v = textValue{single}
		return nil
	}
	var multiple []string
	if json.Unmarshal(data, &multiple) == nil {
		*v = textValue(multiple)
		return nil
	}
	if string(data) == "null" {
		*v = nil
		return nil
	}
	return errors.New("nasa: expected text metadata")
}

func (v textValue) First() string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func normalizeRenditions(items []collectionItem, kind media.Kind) []provider.Rendition {
	result := make([]provider.Rendition, 0, min(len(items), maxRenditions))
	seen := make(map[string]bool)
	for _, item := range items {
		assetURL, safe := canonicalAssetURL(item.Href)
		if !safe || seen[assetURL] {
			continue
		}
		contentType := contentTypeForPath(assetURL)
		if !contentTypeMatchesKind(contentType, kind) {
			continue
		}
		seen[assetURL] = true
		result = append(result, provider.Rendition{
			Name: renditionName(assetURL), Format: formatForContentType(contentType),
			ContentType: contentType, URL: assetURL,
		})
	}
	sort.SliceStable(result, func(i, j int) bool { return renditionScore(result[i]) > renditionScore(result[j]) })
	if len(result) > maxRenditions {
		result = result[:maxRenditions]
	}
	return result
}

func renditionScore(rendition provider.Rendition) int {
	score := 0
	if rendition.ContentType == "video/mp4" || rendition.ContentType == "image/jpeg" || rendition.ContentType == "image/png" {
		score += 10
	}
	if rendition.Name == "original" {
		score += 100
	}
	return score
}

func renditionName(value string) string {
	name := strings.ToLower(path.Base(value))
	for _, candidate := range []string{"orig", "large", "medium", "small", "thumb"} {
		if strings.Contains(name, "~"+candidate) {
			if candidate == "orig" {
				return "original"
			}
			return candidate
		}
	}
	return "provider"
}

func contentTypeForPath(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	default:
		return ""
	}
}

func contentTypeMatchesKind(contentType string, kind media.Kind) bool {
	return kind == media.KindImage && strings.HasPrefix(contentType, "image/") ||
		kind == media.KindVideo && strings.HasPrefix(contentType, "video/")
}

func nasaMediaKind(value string) (media.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image":
		return media.KindImage, true
	case "video":
		return media.KindVideo, true
	default:
		return "", false
	}
}

func parseCursor(cursor string) (int, error) {
	if cursor == "" {
		return 1, nil
	}
	pageNumber, err := strconv.Atoi(cursor)
	if err != nil || pageNumber < 1 || pageNumber > 10000 {
		return 0, fmt.Errorf("%w: invalid NASA cursor", provider.ErrInvalidQuery)
	}
	return pageNumber, nil
}

func hasNext(links []collectionLink) bool {
	for _, link := range links {
		if strings.EqualFold(link.Rel, "next") {
			return true
		}
	}
	return false
}

func validNASAID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character <= ' ' || character == '/' || character == '\\' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func canonicalAssetURL(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.Port() != "" ||
		!strings.EqualFold(parsed.Hostname(), "images-assets.nasa.gov") {
		return "", false
	}
	parsed.Scheme = "https"
	parsed.Host = "images-assets.nasa.gov"
	parsed.Fragment = ""
	return parsed.String(), true
}

func joinedURL(base *url.URL, segment string) string {
	result := *base
	result.Path = path.Join(strings.TrimSuffix(result.Path, "/"), segment)
	return result.String()
}

func absoluteHTTPURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("not an absolute HTTP(S) URL")
	}
	return parsed, nil
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatForContentType(value string) string {
	_, format, _ := strings.Cut(value, "/")
	return format
}
