package provider

import (
	"encoding/json"
	"testing"

	"github.com/brandopakel/gogifgenerator/internal/media"
)

func TestClipResultJSONCarriesPlaybackAndQuoteMetadata(t *testing.T) {
	result := Result{
		Provider: "clips", ExternalID: "clip-1", Title: "A matched line", Kind: media.KindClip,
		SourceURL: "https://example.com/clips/1", PreviewURL: "https://example.com/clips/1.gif",
		OriginalURL: "https://example.com/clips/1.mp4", ContentType: "video/mp4",
		DurationMS: 3200, HasAudio: true,
		Renditions: []Rendition{{
			Name: "360p", Format: "mp4", ContentType: "video/mp4",
			URL: "https://example.com/clips/1-360.mp4", Width: 640, Height: 360,
			DurationMS: 3200, HasAudio: true,
		}},
		Captions: []CaptionTrack{{Language: "en", Format: "vtt", URL: "https://example.com/clips/1.vtt"}},
		QuoteMatch: &QuoteMatch{
			Text: "A matched line", StartMS: 500, EndMS: 1800, Exact: true, Confidence: 0.98,
		},
		AllowedHandling: []HandlingMode{HandlingLink, HandlingDisplay},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["duration_ms"] != float64(3200) || decoded["has_audio"] != true {
		t.Fatalf("clip fields = %s", data)
	}
	if len(decoded["renditions"].([]any)) != 1 || len(decoded["captions"].([]any)) != 1 {
		t.Fatalf("media fields = %s", data)
	}
	match := decoded["quote_match"].(map[string]any)
	if match["text"] != "A matched line" || match["exact"] != true {
		t.Fatalf("quote match = %#v", match)
	}
}
