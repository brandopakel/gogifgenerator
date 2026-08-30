package subtitle

import (
	"errors"
	"strings"
	"testing"
)

func TestParseWebVTT(t *testing.T) {
	data := `WEBVTT transcript
Kind: captions

NOTE generated automatically
this block is ignored

intro
00:01.200 --> 00:03.400 align:start
We <b>actually</b> shipped &amp; celebrated.

00:03.500 --> 00:05.000
That tiny product today!
`
	cues, err := Parse(strings.NewReader(data), "vtt")
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 {
		t.Fatalf("cues = %#v", cues)
	}
	if cues[0].StartMS != 1200 || cues[0].EndMS != 3400 || cues[0].Text != "We actually shipped & celebrated." {
		t.Fatalf("first cue = %#v", cues[0])
	}
}

func TestParseSubRip(t *testing.T) {
	data := `1
00:00:01,050 --> 00:00:03,900
Hello from <i>GoGIF</i>.

2
00:00:04,000 --> 00:00:05,000
Ship it.
`
	cues, err := Parse(strings.NewReader(data), ".srt")
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 || cues[0].StartMS != 1050 || cues[1].Text != "Ship it." {
		t.Fatalf("cues = %#v", cues)
	}
}

func TestFindExactQuoteAcrossCues(t *testing.T) {
	cues := []Cue{
		{StartMS: 1000, EndMS: 2500, Text: "We actually shipped"},
		{StartMS: 2500, EndMS: 4100, Text: "the tiny product today"},
		{StartMS: 5000, EndMS: 6000, Text: "and celebrated"},
	}
	match, ok := Find(cues, "Actually shipped the tiny product!")
	if !ok || !match.Exact || match.StartMS != 1000 || match.EndMS != 4100 || match.Confidence != 1 {
		t.Fatalf("match = %#v, %v", match, ok)
	}
}

func TestFindFuzzyASRQuote(t *testing.T) {
	cues := []Cue{
		{StartMS: 9000, EndMS: 11000, Text: "we ship the product"},
		{StartMS: 11000, EndMS: 13000, Text: "right on time"},
	}
	match, ok := Find(cues, "we shipped the product")
	if !ok || match.Exact || match.Confidence < 0.7 || match.StartMS != 9000 {
		t.Fatalf("match = %#v, %v", match, ok)
	}
}

func TestFindRejectsUnrelatedText(t *testing.T) {
	cues := []Cue{{StartMS: 1000, EndMS: 2000, Text: "completely unrelated words"}}
	if match, ok := Find(cues, "a famous movie quote"); ok {
		t.Fatalf("unexpected match = %#v", match)
	}
}

func TestParseRejectsUnknownFormatAndEmptyCaptions(t *testing.T) {
	for _, test := range []struct {
		format string
		data   string
	}{
		{"ass", "dialogue"},
		{"vtt", "not a webvtt file"},
	} {
		_, err := Parse(strings.NewReader(test.data), test.format)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Parse(%q) error = %v", test.format, err)
		}
	}
}
