// Package yarn searches GetYarn's public HTML and normalizes its clip cards.
// It parses metadata and constructs provider-authorized embeds only; it never
// downloads, proxies, transforms, or rehosts movie and television media.
package yarn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const (
	defaultSearchEndpoint = "https://getyarn.io/yarn-find"
	defaultClipBase       = "https://getyarn.io/yarn-clip/"
	defaultUserAgent      = "GoGIF/0.1 (+https://github.com/brandopakel/gogifgenerator)"
	maxSearchBytes        = 6 << 20
	minRequestInterval    = 750 * time.Millisecond
)

var (
	clipIDPattern  = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	durationSecond = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:s|sec|secs|second|seconds)\b`)
	durationClock  = regexp.MustCompile(`\b(\d{1,2}):(\d{2})\b`)
)

type Options struct {
	SearchEndpoint string
	ClipBase       string
	UserAgent      string
	Client         *http.Client
}

type Yarn struct {
	searchEndpoint *url.URL
	clipBase       *url.URL
	userAgent      string
	client         *http.Client
	gate           chan struct{}
	lastRequest    time.Time
}

func New(options Options) (*Yarn, error) {
	if options.SearchEndpoint == "" {
		options.SearchEndpoint = defaultSearchEndpoint
	}
	if options.ClipBase == "" {
		options.ClipBase = defaultClipBase
	}
	searchEndpoint, err := absoluteHTTPURL(options.SearchEndpoint)
	if err != nil {
		return nil, errors.New("yarn: search endpoint must be an absolute HTTP(S) URL")
	}
	clipBase, err := absoluteHTTPURL(options.ClipBase)
	if err != nil {
		return nil, errors.New("yarn: clip base must be an absolute HTTP(S) URL")
	}
	if options.UserAgent == "" {
		options.UserAgent = defaultUserAgent
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Yarn{
		searchEndpoint: searchEndpoint, clipBase: clipBase, userAgent: options.UserAgent,
		client: options.Client, gate: make(chan struct{}, 1),
	}, nil
}

func (y *Yarn) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "yarn", Label: "Yarn movie & TV clips"}
}

func (y *Yarn) Search(ctx context.Context, query provider.Query) (provider.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return provider.Page{}, err
	}
	requestURL := *y.searchEndpoint
	parameters := requestURL.Query()
	parameters.Set("text", query.Text)
	if query.Cursor != "" {
		key, value, cursorErr := parseCursor(query.Cursor)
		if cursorErr != nil {
			return provider.Page{}, cursorErr
		}
		parameters.Set(key, value)
	}
	requestURL.RawQuery = parameters.Encode()

	document, err := y.fetchSearch(ctx, requestURL.String())
	if err != nil {
		return provider.Page{}, err
	}
	results, cursor := y.parseSearch(document, query.Limit)
	return provider.Page{Provider: "yarn", Results: results, Cursor: cursor}, nil
}

func (y *Yarn) Resolve(_ context.Context, externalID, locale string) (provider.Result, error) {
	if locale != "" && len(locale) > 16 {
		return provider.Result{}, fmt.Errorf("%w: locale is too long", provider.ErrInvalidQuery)
	}
	externalID = strings.ToLower(strings.TrimSpace(externalID))
	if !clipIDPattern.MatchString(externalID) {
		return provider.Result{}, fmt.Errorf("%w: invalid Yarn clip ID", provider.ErrInvalidQuery)
	}
	return y.result(parsedClip{ID: externalID}), nil
}

func (y *Yarn) fetchSearch(ctx context.Context, requestURL string) (*html.Node, error) {
	select {
	case y.gate <- struct{}{}:
		defer func() { <-y.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if wait := minRequestInterval - time.Since(y.lastRequest); !y.lastRequest.IsZero() && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	y.lastRequest = time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build Yarn request: %v", provider.ErrUnavailable, err)
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("User-Agent", y.userAgent)
	response, err := y.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: Yarn search request: %v", provider.ErrUnavailable, err)
	}
	defer response.Body.Close()
	challenge := strings.EqualFold(response.Header.Get("cf-mitigated"), "challenge")
	if response.StatusCode != http.StatusOK || challenge {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		if challenge || response.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: Yarn blocked automated search with a browser challenge", provider.ErrUnavailable)
		}
		return nil, fmt.Errorf("%w: Yarn returned HTTP %d", provider.ErrUnavailable, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSearchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read Yarn search HTML: %v", provider.ErrUnavailable, err)
	}
	if len(body) > maxSearchBytes {
		return nil, fmt.Errorf("%w: Yarn search HTML exceeds %d bytes", provider.ErrUnavailable, maxSearchBytes)
	}
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: parse Yarn search HTML: %v", provider.ErrUnavailable, err)
	}
	if hasChallengeMarkup(document) {
		return nil, fmt.Errorf("%w: Yarn returned a browser challenge", provider.ErrUnavailable)
	}
	return document, nil
}

type parsedClip struct {
	ID         string
	Transcript string
	Work       string
	Creator    string
	PosterURL  string
	DurationMS int64
}

func (y *Yarn) parseSearch(document *html.Node, limit int) ([]provider.Result, string) {
	clips := make([]parsedClip, 0, limit)
	seen := make(map[string]struct{}, limit)
	walk(document, func(node *html.Node) bool {
		if len(clips) >= limit || node.Type != html.ElementNode || node.Data != "a" {
			return true
		}
		clipID, ok := clipIDFromURL(attribute(node, "href"))
		if !ok {
			return true
		}
		if _, duplicate := seen[clipID]; duplicate {
			return true
		}
		seen[clipID] = struct{}{}
		container := cardContainer(node)
		transcript := firstClassText(container, "clip-transcript", "transcript", "quote", "subtitle")
		work := firstClassText(container, "movie-title", "series-title", "episode-title", "clip-title", "title")
		creator := firstClassText(container, "creator", "show", "movie", "series", "episode")
		if transcript == "" {
			transcript = firstNonEmpty(attribute(node, "title"), attribute(node, "aria-label"), imageText(container))
		}
		if work == "" {
			work = firstNonEmpty(attribute(container, "data-title"), attribute(container, "data-movie"), attribute(container, "data-show"))
		}
		clips = append(clips, parsedClip{
			ID: clipID, Transcript: cleanText(transcript), Work: cleanText(work), Creator: cleanText(creator),
			PosterURL: safePosterURL(firstImageURL(container)), DurationMS: parseDurationMS(nodeText(container)),
		})
		return true
	})
	results := make([]provider.Result, 0, len(clips))
	for _, clip := range clips {
		results = append(results, y.result(clip))
	}
	return results, nextCursor(document)
}

func (y *Yarn) result(clip parsedClip) provider.Result {
	title := firstNonEmpty(clip.Work, clip.Creator, clip.Transcript, "Yarn clip")
	sourceURL := joinedURL(y.clipBase, clip.ID)
	attribution := "Yarn"
	if context := firstNonEmpty(clip.Work, clip.Creator); context != "" {
		attribution += " · " + context
	}
	result := provider.Result{
		Provider: "yarn", ExternalID: clip.ID, Title: title, Description: clip.Transcript,
		Kind: media.KindClip, SourceURL: sourceURL, PreviewURL: clip.PosterURL,
		EmbedURL: sourceURL + "/embed?autoplay=false&responsive=true", ContentType: "text/html", DurationMS: clip.DurationMS,
		AllowedHandling: []provider.HandlingMode{provider.HandlingLink, provider.HandlingDisplay},
		Author:          clip.Creator, Attribution: attribution,
		Restrictions: []string{
			"Provider embed only: GoGIF does not download, proxy, transform, or rehost Yarn media.",
			"Movie and television rights remain with their respective owners.",
			"Search availability does not grant commercial-use or derivative-work permission.",
		},
		CommercialUse: media.PermissionUnknown, Derivatives: media.PermissionUnknown,
		TransformPolicy: provider.TransformReference,
	}
	if clip.Transcript != "" {
		result.QuoteMatch = &provider.QuoteMatch{Text: clip.Transcript, Exact: true, Confidence: 1}
	}
	return result
}

func cardContainer(node *html.Node) *html.Node {
	fallback := node.Parent
	for parent, depth := node.Parent, 0; parent != nil && depth < 7; parent, depth = parent.Parent, depth+1 {
		classes := strings.ToLower(attribute(parent, "class"))
		if containsAny(classes, "clip-wrap", "clip-card", "yarn-card", "search-result", "result-card") {
			return parent
		}
	}
	if fallback != nil {
		return fallback
	}
	return node
}

func firstClassText(root *html.Node, classes ...string) string {
	result := ""
	walk(root, func(node *html.Node) bool {
		if result != "" || node.Type != html.ElementNode {
			return result == ""
		}
		className := strings.ToLower(attribute(node, "class"))
		for _, class := range classes {
			if classToken(className, class) {
				result = nodeText(node)
				return false
			}
		}
		return true
	})
	return result
}

func imageText(root *html.Node) string {
	result := ""
	walk(root, func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "img" {
			result = firstNonEmpty(attribute(node, "alt"), attribute(node, "title"))
			return false
		}
		return true
	})
	return result
}

func firstImageURL(root *html.Node) string {
	result := ""
	walk(root, func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "img" {
			result = firstNonEmpty(attribute(node, "src"), attribute(node, "data-src"), attribute(node, "data-lazy-src"))
			return false
		}
		return true
	})
	return result
}

func nextCursor(document *html.Node) string {
	cursor := ""
	walk(document, func(node *html.Node) bool {
		if cursor != "" || node.Type != html.ElementNode || node.Data != "a" {
			return cursor == ""
		}
		rel := strings.ToLower(attribute(node, "rel"))
		className := strings.ToLower(attribute(node, "class"))
		if !containsAny(rel, "next") && !containsAny(className, "next", "load-more") {
			return true
		}
		parsed, err := url.Parse(attribute(node, "href"))
		if err != nil {
			return true
		}
		for _, key := range []string{"page", "p", "offset", "from"} {
			if value := parsed.Query().Get(key); validCursorNumber(value) {
				cursor = key + "=" + value
				return false
			}
		}
		return true
	})
	return cursor
}

func parseCursor(cursor string) (string, string, error) {
	parts := strings.Split(cursor, "=")
	if len(parts) != 2 || !oneOf(parts[0], "page", "p", "offset", "from") || !validCursorNumber(parts[1]) {
		return "", "", fmt.Errorf("%w: invalid Yarn cursor", provider.ErrInvalidQuery)
	}
	return parts[0], parts[1], nil
}

func validCursorNumber(value string) bool {
	number, err := strconv.Atoi(value)
	return err == nil && number >= 0 && number <= 100000
}

func clipIDFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	if parsed.IsAbs() {
		host := strings.ToLower(parsed.Hostname())
		if parsed.Scheme != "https" || !oneOf(host, "getyarn.io", "www.getyarn.io", "yarn.co", "www.yarn.co") {
			return "", false
		}
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for index := 0; index+1 < len(parts); index++ {
		if parts[index] == "yarn-clip" && clipIDPattern.MatchString(parts[index+1]) {
			return strings.ToLower(parts[index+1]), true
		}
	}
	return "", false
}

func safePosterURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if !oneOf(host, "y.yarn.co", "yarn.co", "www.yarn.co", "getyarn.io", "www.getyarn.io") {
		return ""
	}
	parsed.User, parsed.Fragment = nil, ""
	return parsed.String()
}

func parseDurationMS(text string) int64 {
	if match := durationSecond.FindStringSubmatch(text); len(match) == 2 {
		seconds, _ := strconv.ParseFloat(match[1], 64)
		return int64(seconds * 1000)
	}
	if match := durationClock.FindStringSubmatch(text); len(match) == 3 {
		minutes, _ := strconv.ParseInt(match[1], 10, 64)
		seconds, _ := strconv.ParseInt(match[2], 10, 64)
		return (minutes*60 + seconds) * 1000
	}
	return 0
}

func hasChallengeMarkup(document *html.Node) bool {
	challenged := false
	walk(document, func(node *html.Node) bool {
		if node.Type == html.ElementNode && (strings.HasPrefix(attribute(node, "id"), "cf-") || strings.Contains(attribute(node, "class"), "cf-chl")) {
			challenged = true
			return false
		}
		return true
	})
	return challenged
}

func walk(node *html.Node, visit func(*html.Node) bool) {
	if node == nil || !visit(node) {
		return
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}

func nodeText(node *html.Node) string {
	var builder strings.Builder
	walk(node, func(candidate *html.Node) bool {
		if candidate.Type == html.TextNode {
			builder.WriteByte(' ')
			builder.WriteString(candidate.Data)
		}
		return true
	})
	return cleanText(builder.String())
}

func attribute(node *html.Node, key string) string {
	if node == nil {
		return ""
	}
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) {
			return strings.TrimSpace(attribute.Val)
		}
	}
	return ""
}

func classToken(classes, wanted string) bool {
	for _, class := range strings.Fields(classes) {
		if class == wanted {
			return true
		}
	}
	return false
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate || strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func oneOf(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = cleanText(value); value != "" {
			return value
		}
	}
	return ""
}

func joinedURL(base *url.URL, parts ...string) string {
	result := *base
	path := strings.TrimRight(result.Path, "/")
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	result.Path, result.RawPath, result.RawQuery, result.Fragment = path, "", "", ""
	return result.String()
}

func absoluteHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return nil, errors.New("invalid URL")
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed, nil
}
