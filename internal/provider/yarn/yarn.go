// Package yarn exposes GetYarn as a link-only discovery provider.
//
// GetYarn does not publish a supported search API and currently protects both
// its HTML search and media CDN with a browser challenge. This adapter therefore
// validates input and constructs canonical provider pages without scraping,
// downloading, hotlinking, or rehosting movie and television clips.
package yarn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/brandopakel/gogifgenerator/internal/media"
	"github.com/brandopakel/gogifgenerator/internal/provider"
)

const (
	searchBase = "https://getyarn.io/yarn-find"
	clipBase   = "https://getyarn.io/yarn-clip/"
)

var clipIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Yarn is intentionally network-free. It gives the browser an official Yarn
// destination while keeping unsupported scraping out of GoGIF's backend.
type Yarn struct{}

func (Yarn) Descriptor() provider.Descriptor {
	return provider.Descriptor{ID: "yarn", Label: "Yarn movie & TV clips"}
}

func (Yarn) Search(_ context.Context, query provider.Query) (provider.Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return provider.Page{}, err
	}
	if query.Cursor != "" {
		return provider.Page{}, fmt.Errorf("%w: Yarn link search does not paginate inside GoGIF", provider.ErrInvalidQuery)
	}
	if clipID, ok := clipIDFromInput(query.Text); ok {
		return provider.Page{Provider: "yarn", Results: []provider.Result{clipResult(clipID)}}, nil
	}
	return provider.Page{Provider: "yarn", Results: []provider.Result{searchResult(query.Text)}}, nil
}

func (Yarn) Resolve(_ context.Context, externalID, locale string) (provider.Result, error) {
	if locale != "" && len(locale) > 16 {
		return provider.Result{}, fmt.Errorf("%w: locale is too long", provider.ErrInvalidQuery)
	}
	externalID = strings.ToLower(strings.TrimSpace(externalID))
	if !clipIDPattern.MatchString(externalID) {
		return provider.Result{}, fmt.Errorf("%w: invalid Yarn clip ID", provider.ErrInvalidQuery)
	}
	return clipResult(externalID), nil
}

func searchResult(query string) provider.Result {
	searchURL, _ := url.Parse(searchBase)
	parameters := searchURL.Query()
	parameters.Set("text", query)
	searchURL.RawQuery = parameters.Encode()
	digest := sha256.Sum256([]byte(strings.ToLower(query)))
	return linkResult(
		"search-"+hex.EncodeToString(digest[:8]),
		`Search Yarn for “`+query+`”`,
		"Opens Yarn's provider-hosted movie and television quote results in a new tab.",
		searchURL.String(),
	)
}

func clipResult(clipID string) provider.Result {
	return linkResult(
		strings.ToLower(clipID),
		"Open Yarn clip",
		"Opens this provider-hosted movie or television clip on Yarn.",
		clipBase+strings.ToLower(clipID),
	)
}

func linkResult(externalID, title, description, sourceURL string) provider.Result {
	return provider.Result{
		Provider: "yarn", ExternalID: externalID, Title: title, Description: description,
		Kind: media.KindClip, SourceURL: sourceURL, ContentType: "text/html",
		AllowedHandling: []provider.HandlingMode{provider.HandlingLink},
		Attribution:     "Yarn",
		Restrictions: []string{
			"Link-only: GoGIF does not download, proxy, hotlink, transform, or rehost Yarn media.",
			"Movie and television rights remain with their respective owners.",
			"Search availability does not grant commercial-use or derivative-work permission.",
		},
		CommercialUse: media.PermissionUnknown, Derivatives: media.PermissionUnknown,
		TransformPolicy: provider.TransformReference,
	}
}

func clipIDFromInput(input string) (string, bool) {
	input = strings.TrimSpace(input)
	if clipIDPattern.MatchString(input) {
		return strings.ToLower(input), true
	}
	parsed, err := url.Parse(input)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "getyarn.io" && host != "www.getyarn.io" {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "yarn-clip" {
		return "", false
	}
	clipID, err := url.PathUnescape(parts[1])
	if err != nil || !clipIDPattern.MatchString(clipID) {
		return "", false
	}
	return strings.ToLower(clipID), true
}
