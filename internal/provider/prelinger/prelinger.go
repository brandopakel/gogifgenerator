// Package prelinger searches and resolves reusable archival films from the
// Internet Archive's Prelinger collection.
package prelinger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
	"github.com/brandopakel/gogifgenerator/internal/subtitle"
)

const (
	defaultSearchEndpoint = "https://archive.org/advancedsearch.php"
	defaultMetadataBase   = "https://archive.org/metadata/"
	defaultDetailsBase    = "https://archive.org/details/"
	defaultDownloadBase   = "https://archive.org/download/"
	defaultImageBase      = "https://archive.org/services/img/"
	defaultUserAgent      = "GoGIF/0.1 (https://github.com/brandopakel/gogifgenerator)"
	maxSearchBytes        = 4 << 20
	maxMetadataBytes      = 12 << 20
	maxCaptionBytes       = 8 << 20
	maxRenditions         = 8
	maxCaptionTracks      = 16
)

type Options struct {
	SearchEndpoint string
	MetadataBase   string
	DetailsBase    string
	DownloadBase   string
	ImageBase      string
	UserAgent      string
	Client         *http.Client
}

type Prelinger struct {
	searchEndpoint *url.URL
	metadataBase   *url.URL
	detailsBase    *url.URL
	downloadBase   *url.URL
	imageBase      *url.URL
	userAgent      string
	client         *http.Client
	gate           chan struct{}
}

func New(options Options) (*Prelinger, error) {
	defaults := []struct {
		value    *string
		fallback string
		label    string
	}{
		{&options.SearchEndpoint, defaultSearchEndpoint, "search endpoint"},
		{&options.MetadataBase, defaultMetadataBase, "metadata base"},
		{&options.DetailsBase, defaultDetailsBase, "details base"},
		{&options.DownloadBase, defaultDownloadBase, "download base"},
		{&options.ImageBase, defaultImageBase, "image base"},
	}
	parsed := make([]*url.URL, 0, len(defaults))
	for _, candidate := range defaults {
		if *candidate.value == "" {
			*candidate.value = candidate.fallback
		}
		value, err := absoluteHTTPURL(*candidate.value)
		if err != nil {
			return nil, fmt.Errorf("prelinger: %s must be an absolute HTTP(S) URL", candidate.label)
		}
		parsed = append(parsed, value)
	}
	if options.UserAgent == "" {
		options.UserAgent = defaultUserAgent
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Prelinger{
		searchEndpoint: parsed[0], metadataBase: parsed[1], detailsBase: parsed[2],
		downloadBase: parsed[3], imageBase: parsed[4], userAgent: options.UserAgent,
		client: options.Client, gate: make(chan struct{}, 1),
	}, nil
}

func (p *Prelinger) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "prelinger", Label: "Prelinger Archive"}
}

func (p *Prelinger) Search(ctx context.Context, query provider.Query) (provider.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return provider.Page{}, err
	}
	pageNumber, err := parseCursor(query.Cursor)
	if err != nil {
		return provider.Page{}, err
	}
	requestURL := *p.searchEndpoint
	params := requestURL.Query()
	params.Set("q", `collection:prelinger AND mediatype:movies AND text:"`+escapeSearchPhrase(query.Text)+`"`)
	for _, field := range []string{"identifier", "title", "creator", "licenseurl", "date"} {
		params.Add("fl[]", field)
	}
	params.Set("rows", strconv.Itoa(query.Limit))
	params.Set("page", strconv.Itoa(pageNumber))
	params.Set("output", "json")
	requestURL.RawQuery = params.Encode()

	var payload searchResponse
	if err := p.getJSON(ctx, requestURL.String(), maxSearchBytes, &payload); err != nil {
		return provider.Page{}, err
	}
	if payload.ResponseHeader.Status != 0 {
		return provider.Page{}, fmt.Errorf("%w: Internet Archive search status %d", provider.ErrUnavailable, payload.ResponseHeader.Status)
	}
	page := provider.Page{Provider: p.Descriptor().ID, Results: make([]provider.Result, 0, len(payload.Response.Docs))}
	for _, item := range payload.Response.Docs {
		if result, ok := p.normalizeSearch(item); ok {
			page.Results = append(page.Results, result)
		}
	}
	if payload.Response.Start+len(payload.Response.Docs) < payload.Response.NumFound {
		page.Cursor = strconv.Itoa(pageNumber + 1)
	}
	return page, nil
}

func (p *Prelinger) Resolve(ctx context.Context, externalID, locale string) (provider.Result, error) {
	externalID = strings.TrimSpace(externalID)
	if !validIdentifier(externalID) {
		return provider.Result{}, fmt.Errorf("%w: invalid Internet Archive identifier", provider.ErrInvalidQuery)
	}
	if locale == "" {
		locale = "en"
	}
	if len(locale) > 16 {
		return provider.Result{}, fmt.Errorf("%w: locale is too long", provider.ErrInvalidQuery)
	}
	var payload metadataResponse
	if err := p.getJSON(ctx, joinedURL(p.metadataBase, externalID), maxMetadataBytes, &payload); err != nil {
		return provider.Result{}, err
	}
	identifier := payload.Metadata.Identifier.First()
	if identifier == "" {
		identifier = externalID
	}
	if identifier != externalID || !validIdentifier(identifier) || !payload.Metadata.Collection.Contains("prelinger") || !payload.Metadata.MediaType.Contains("movies") {
		return provider.Result{}, provider.ErrNotFound
	}
	result, ok := p.normalizeMetadata(payload.Metadata, payload.Files, locale)
	if !ok {
		return provider.Result{}, provider.ErrNotFound
	}
	return result, nil
}

func (p *Prelinger) ResolveQuote(ctx context.Context, externalID, locale, quote string) (provider.Result, error) {
	quote = strings.TrimSpace(quote)
	if quote == "" || len(quote) > 200 {
		return provider.Result{}, fmt.Errorf("%w: quote must contain between 1 and 200 characters", provider.ErrInvalidQuery)
	}
	result, err := p.Resolve(ctx, externalID, locale)
	if err != nil {
		return provider.Result{}, err
	}
	caption, ok := preferredCaption(result.Captions, locale)
	if !ok {
		return result, nil
	}
	data, err := p.get(ctx, caption.URL, "text/vtt, text/plain;q=0.9, */*;q=0.1", maxCaptionBytes)
	if err != nil {
		return provider.Result{}, err
	}
	cues, err := subtitle.Parse(strings.NewReader(string(data)), caption.Format)
	if err != nil {
		// A malformed optional transcript must not hide a playable item.
		return result, nil
	}
	match, ok := subtitle.Find(cues, quote)
	if ok {
		result.QuoteMatch = &provider.QuoteMatch{
			Text: match.Text, StartMS: match.StartMS, EndMS: match.EndMS,
			Exact: match.Exact, Confidence: match.Confidence,
		}
	}
	return result, nil
}

func (p *Prelinger) getJSON(ctx context.Context, requestURL string, maxBytes int64, destination any) error {
	data, err := p.get(ctx, requestURL, "application/json", maxBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("%w: decode Internet Archive response: %v", provider.ErrUnavailable, err)
	}
	return nil
}

func (p *Prelinger) get(ctx context.Context, requestURL, accept string, maxBytes int64) ([]byte, error) {
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("prelinger: build request: %w", err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", p.userAgent)
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: Internet Archive request: %v", provider.ErrUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, provider.ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, fmt.Errorf("%w: Internet Archive returned HTTP %d", provider.ErrUnavailable, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read Internet Archive response: %v", provider.ErrUnavailable, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: Internet Archive response exceeded %d bytes", provider.ErrUnavailable, maxBytes)
	}
	return data, nil
}

type searchResponse struct {
	ResponseHeader struct {
		Status int `json:"status"`
	} `json:"responseHeader"`
	Response struct {
		NumFound int          `json:"numFound"`
		Start    int          `json:"start"`
		Docs     []searchItem `json:"docs"`
	} `json:"response"`
}

type searchItem struct {
	Identifier textValues `json:"identifier"`
	Title      textValues `json:"title"`
	Creator    textValues `json:"creator"`
	LicenseURL textValues `json:"licenseurl"`
	Date       textValues `json:"date"`
}

type metadataResponse struct {
	Metadata itemMetadata `json:"metadata"`
	Files    []itemFile   `json:"files"`
}

type itemMetadata struct {
	Identifier textValues `json:"identifier"`
	MediaType  textValues `json:"mediatype"`
	Collection textValues `json:"collection"`
	Title      textValues `json:"title"`
	Creator    textValues `json:"creator"`
	LicenseURL textValues `json:"licenseurl"`
	Runtime    textValues `json:"runtime"`
	Sound      textValues `json:"sound"`
}

type itemFile struct {
	Name     scalarText `json:"name"`
	Format   scalarText `json:"format"`
	Size     scalarText `json:"size"`
	Length   scalarText `json:"length"`
	Width    scalarText `json:"width"`
	Height   scalarText `json:"height"`
	Source   scalarText `json:"source"`
	Original scalarText `json:"original"`
}

func (p *Prelinger) normalizeSearch(item searchItem) (provider.Result, bool) {
	identifier := item.Identifier.First()
	if !validIdentifier(identifier) {
		return provider.Result{}, false
	}
	title := cleanText(item.Title.First())
	if title == "" {
		title = identifier
	}
	author := cleanText(strings.Join(item.Creator, ", "))
	rights := classifyLicense(item.LicenseURL.First())
	return provider.Result{
		Provider: "prelinger", ExternalID: identifier, Title: title, Kind: media.KindVideo,
		SourceURL:  joinedURL(p.detailsBase, identifier),
		PreviewURL: joinedURL(p.imageBase, identifier),
		Author:     author, Attribution: buildAttribution(title, author, rights.Name),
		LicenseID: rights.ID, LicenseName: rights.Name, LicenseURL: rights.URL,
		Restrictions: rights.Restrictions, CommercialUse: rights.Commercial,
		Derivatives: rights.Derivatives, ShareAlike: rights.ShareAlike,
		TransformPolicy: rights.TransformPolicy,
		AllowedHandling: []provider.HandlingMode{provider.HandlingLink, provider.HandlingDisplay},
	}, true
}

func (p *Prelinger) normalizeMetadata(metadata itemMetadata, files []itemFile, locale string) (provider.Result, bool) {
	identifier := metadata.Identifier.First()
	if !validIdentifier(identifier) {
		return provider.Result{}, false
	}
	title := cleanText(metadata.Title.First())
	if title == "" {
		title = identifier
	}
	author := cleanText(strings.Join(metadata.Creator, ", "))
	rights := classifyLicense(metadata.LicenseURL.First())
	renditions := p.videoRenditions(identifier, files)
	if len(renditions) == 0 {
		return provider.Result{}, false
	}
	captions := p.captionTracks(identifier, files, locale)
	primary := renditions[0]
	durationMS := primary.DurationMS
	if durationMS == 0 {
		durationMS = parseRuntime(metadata.Runtime.First())
	}
	hasAudio := hasSound(metadata.Sound.First())
	for index := range renditions {
		if renditions[index].DurationMS == 0 {
			renditions[index].DurationMS = durationMS
		}
		renditions[index].HasAudio = hasAudio
	}
	return provider.Result{
		Provider: "prelinger", ExternalID: identifier, Title: title, Kind: media.KindVideo,
		SourceURL: joinedURL(p.detailsBase, identifier), PreviewURL: joinedURL(p.imageBase, identifier),
		OriginalURL: primary.URL, ReferenceURL: primary.URL, ContentType: primary.ContentType,
		Width: primary.Width, Height: primary.Height, DurationMS: durationMS,
		SizeBytes: primary.SizeBytes, HasAudio: hasAudio, Renditions: renditions, Captions: captions,
		AllowedHandling: []provider.HandlingMode{provider.HandlingLink, provider.HandlingDisplay},
		Author:          author, Attribution: buildAttribution(title, author, rights.Name),
		LicenseID: rights.ID, LicenseName: rights.Name, LicenseURL: rights.URL,
		Restrictions: rights.Restrictions, CommercialUse: rights.Commercial,
		Derivatives: rights.Derivatives, ShareAlike: rights.ShareAlike,
		TransformPolicy: rights.TransformPolicy,
	}, true
}

func (p *Prelinger) videoRenditions(identifier string, files []itemFile) []provider.Rendition {
	type ranked struct {
		rank      int
		rendition provider.Rendition
	}
	values := make([]ranked, 0, len(files))
	for _, file := range files {
		name := string(file.Name)
		if !safeFileName(name) {
			continue
		}
		contentType, format, ok := videoType(name)
		if !ok {
			continue
		}
		formatLabel := string(file.Format)
		values = append(values, ranked{rank: videoRank(formatLabel, string(file.Source)), rendition: provider.Rendition{
			Name: renditionName(formatLabel, name), Format: format, ContentType: contentType,
			URL: joinedURL(p.downloadBase, identifier, name), Width: parseInt(string(file.Width)),
			Height: parseInt(string(file.Height)), DurationMS: parseSeconds(string(file.Length)),
			SizeBytes: parseInt64(string(file.Size)),
		}})
	}
	sort.SliceStable(values, func(left, right int) bool {
		if values[left].rank != values[right].rank {
			return values[left].rank < values[right].rank
		}
		return values[left].rendition.SizeBytes < values[right].rendition.SizeBytes
	})
	if len(values) > maxRenditions {
		values = values[:maxRenditions]
	}
	result := make([]provider.Rendition, 0, len(values))
	for _, value := range values {
		result = append(result, value.rendition)
	}
	return result
}

func (p *Prelinger) captionTracks(identifier string, files []itemFile, fallbackLocale string) []provider.CaptionTrack {
	result := make([]provider.CaptionTrack, 0)
	for _, file := range files {
		name := string(file.Name)
		if !safeFileName(name) {
			continue
		}
		extension := strings.ToLower(path.Ext(name))
		if extension != ".srt" && extension != ".vtt" {
			continue
		}
		language := captionLanguage(name, fallbackLocale)
		result = append(result, provider.CaptionTrack{
			Language: language, Format: strings.TrimPrefix(extension, "."), URL: joinedURL(p.downloadBase, identifier, name),
		})
		if len(result) == maxCaptionTracks {
			break
		}
	}
	return result
}

func preferredCaption(captions []provider.CaptionTrack, locale string) (provider.CaptionTrack, bool) {
	wantedLanguage := strings.ToLower(strings.TrimSpace(locale))
	if separator := strings.IndexByte(wantedLanguage, '-'); separator >= 0 {
		wantedLanguage = wantedLanguage[:separator]
	}
	bestScore := -1
	var best provider.CaptionTrack
	for _, caption := range captions {
		score := 0
		if strings.EqualFold(caption.Format, "vtt") {
			score += 2
		}
		language := strings.ToLower(strings.TrimSpace(caption.Language))
		if wantedLanguage != "" && (language == wantedLanguage || strings.HasPrefix(language, wantedLanguage+"-")) {
			score += 4
		}
		if score > bestScore {
			best, bestScore = caption, score
		}
	}
	return best, bestScore >= 0
}

type licenseClassification struct {
	ID              string
	Name            string
	URL             string
	Commercial      media.Permission
	Derivatives     media.Permission
	ShareAlike      bool
	TransformPolicy provider.TransformPolicy
	Restrictions    []string
}

func classifyLicense(rawURL string) licenseClassification {
	unknown := licenseClassification{
		Commercial: media.PermissionUnknown, Derivatives: media.PermissionUnknown,
		TransformPolicy: provider.TransformReview,
		Restrictions: []string{
			"verify the item license on Internet Archive before reuse",
			"Prelinger descriptions and shot lists are not licensed for commercial reuse",
		},
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || !strings.EqualFold(strings.TrimPrefix(parsed.Hostname(), "www."), "creativecommons.org") {
		return unknown
	}
	parsed.Scheme = "https"
	parsed.Host = "creativecommons.org"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	licensePath := strings.ToLower(strings.Trim(parsed.Path, "/"))
	parts := strings.Split(licensePath, "/")
	classification := unknown
	classification.URL = parsed.String()
	classification.Restrictions = []string{"preserve the item license and source attribution"}
	switch {
	case strings.Contains(licensePath, "publicdomain"), strings.Contains(licensePath, "public-domain"):
		classification.ID, classification.Name = "public-domain", "Public Domain"
		classification.Commercial, classification.Derivatives = media.PermissionAllowed, media.PermissionAllowed
		classification.TransformPolicy = provider.TransformAllowed
	case len(parts) >= 2 && parts[0] == "licenses":
		code := parts[1]
		version := ""
		if len(parts) >= 3 {
			version = parts[2]
		}
		classification.ID = "cc-" + code
		classification.Name = "CC " + strings.ToUpper(code)
		if version != "" {
			classification.ID += "-" + version
			classification.Name += " " + version
		}
		classification.Commercial, classification.Derivatives = media.PermissionAllowed, media.PermissionAllowed
		classification.TransformPolicy = provider.TransformAllowed
		if strings.Contains(code, "nc") {
			classification.Commercial = media.PermissionProhibited
			classification.TransformPolicy = provider.TransformReview
		}
		if strings.Contains(code, "nd") {
			classification.Derivatives = media.PermissionProhibited
			classification.TransformPolicy = provider.TransformReference
		}
		classification.ShareAlike = strings.Contains(code, "sa")
	default:
		return unknown
	}
	return classification
}

func parseCursor(cursor string) (int, error) {
	if cursor == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(cursor)
	if err != nil || page < 1 || page > 10000 {
		return 0, fmt.Errorf("%w: invalid Prelinger cursor", provider.ErrInvalidQuery)
	}
	return page, nil
}

func escapeSearchPhrase(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func validIdentifier(value string) bool {
	if len(value) < 1 || len(value) > 100 || !isAlphaNumeric(rune(value[0])) {
		return false
	}
	for _, char := range value {
		if !isAlphaNumeric(char) && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func isAlphaNumeric(char rune) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

func safeFileName(value string) bool {
	if value == "" || len(value) > 1024 || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func videoType(name string) (string, string, bool) {
	switch strings.ToLower(path.Ext(name)) {
	case ".mp4", ".m4v":
		return "video/mp4", "mp4", true
	case ".webm":
		return "video/webm", "webm", true
	case ".ogv":
		return "video/ogg", "ogg", true
	default:
		return "", "", false
	}
}

func videoRank(format, source string) int {
	format = strings.ToLower(format)
	switch {
	case strings.Contains(format, "h.264"), strings.Contains(format, "h264"):
		return 0
	case strings.Contains(format, "512kb"):
		return 1
	case strings.Contains(format, "webm"):
		return 2
	case strings.EqualFold(source, "derivative"):
		return 3
	default:
		return 4
	}
}

func renditionName(format, name string) string {
	format = strings.TrimSpace(format)
	if format != "" {
		return format
	}
	return path.Base(name)
}

func captionLanguage(name, fallback string) string {
	base := strings.TrimSuffix(name, path.Ext(name))
	candidate := strings.ToLower(path.Ext(base))
	candidate = strings.TrimPrefix(candidate, ".")
	if len(candidate) == 2 || len(candidate) == 3 {
		for _, char := range candidate {
			if char < 'a' || char > 'z' {
				candidate = ""
				break
			}
		}
		if candidate != "" && candidate != "asr" {
			return candidate
		}
	}
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if fallback != "" {
		return fallback
	}
	return "und"
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	if parsed < 0 {
		return 0
	}
	return parsed
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if parsed < 0 {
		return 0
	}
	return parsed
}

func parseSeconds(value string) int64 {
	seconds, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if seconds <= 0 {
		return 0
	}
	return int64(seconds * 1000)
}

func parseRuntime(value string) int64 {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	seconds := 0.0
	for _, part := range parts {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil || value < 0 {
			return 0
		}
		seconds = seconds*60 + value
	}
	return int64(seconds * 1000)
}

func hasSound(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value != "" && value != "silent" && value != "none" && value != "no"
}

func buildAttribution(title, author, license string) string {
	parts := make([]string, 0, 4)
	for _, value := range []string{title, author, license, "Internet Archive / Prelinger Archives"} {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, " · ")
}

func cleanText(value string) string {
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

func absoluteHTTPURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("not an absolute HTTP(S) URL")
	}
	return parsed, nil
}

func joinedURL(base *url.URL, segments ...string) string {
	result := *base
	parts := append([]string{strings.TrimSuffix(base.Path, "/")}, segments...)
	result.Path = path.Join(parts...)
	return result.String()
}

type textValues []string

func (v *textValues) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*v = nil
		return nil
	}
	var single string
	if json.Unmarshal(data, &single) == nil {
		*v = textValues{single}
		return nil
	}
	var multiple []string
	if err := json.Unmarshal(data, &multiple); err != nil {
		return err
	}
	*v = multiple
	return nil
}

func (v textValues) First() string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func (v textValues) Contains(want string) bool {
	for _, value := range v {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

type scalarText string

func (s *scalarText) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = ""
		return nil
	}
	var value string
	if json.Unmarshal(data, &value) == nil {
		*s = scalarText(value)
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return err
	}
	*s = scalarText(number.String())
	return nil
}
