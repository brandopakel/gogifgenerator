// Package subtitle parses time-aligned caption files and locates spoken quotes
// without requiring a hosted transcription or search service.
package subtitle

import (
	"bufio"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

const (
	maxCaptionLines = 300000
	maxCaptionCues  = 100000
	maxLineBytes    = 1 << 20
	maxMatchGapMS   = 60000
	minFuzzyScore   = 0.62
)

var ErrInvalid = errors.New("subtitle: invalid caption data")

type Cue struct {
	StartMS int64
	EndMS   int64
	Text    string
}

type Match struct {
	Text       string
	StartMS    int64
	EndMS      int64
	Exact      bool
	Confidence float64
}

// Parse accepts WebVTT and SubRip caption data. Invalid individual blocks are
// skipped so one damaged provider cue does not discard an otherwise useful
// transcript.
func Parse(reader io.Reader, format string) ([]Cue, error) {
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if format != "vtt" && format != "srt" {
		return nil, fmt.Errorf("%w: unsupported format %q", ErrInvalid, format)
	}
	lines, err := readLines(reader)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: caption file is empty", ErrInvalid)
	}
	lines[0] = strings.TrimPrefix(lines[0], "\ufeff")
	if format == "vtt" && !strings.HasPrefix(lines[0], "WEBVTT") {
		return nil, fmt.Errorf("%w: missing WEBVTT signature", ErrInvalid)
	}

	cues := make([]Cue, 0, min(len(lines)/3, 1024))
	startLine := 0
	if format == "vtt" {
		startLine = 1
		for startLine < len(lines) && strings.TrimSpace(lines[startLine]) != "" {
			startLine++
		}
	}
	for index := startLine; index < len(lines); {
		for index < len(lines) && strings.TrimSpace(lines[index]) == "" {
			index++
		}
		if index >= len(lines) {
			break
		}
		first := strings.TrimSpace(lines[index])
		if format == "vtt" && (strings.HasPrefix(first, "NOTE") || first == "STYLE" || first == "REGION") {
			index = nextBlock(lines, index+1)
			continue
		}
		timingIndex := index
		if !strings.Contains(lines[timingIndex], "-->") {
			timingIndex++
		}
		if timingIndex >= len(lines) || !strings.Contains(lines[timingIndex], "-->") {
			index = nextBlock(lines, index+1)
			continue
		}
		startMS, endMS, ok := parseTiming(lines[timingIndex])
		payloadStart := timingIndex + 1
		payloadEnd := payloadStart
		for payloadEnd < len(lines) && strings.TrimSpace(lines[payloadEnd]) != "" {
			payloadEnd++
		}
		if ok {
			text := cleanText(strings.Join(lines[payloadStart:payloadEnd], " "))
			if text != "" {
				cues = append(cues, Cue{StartMS: startMS, EndMS: endMS, Text: text})
				if len(cues) > maxCaptionCues {
					return nil, fmt.Errorf("%w: too many cues", ErrInvalid)
				}
			}
		}
		index = payloadEnd + 1
	}
	if len(cues) == 0 {
		return nil, fmt.Errorf("%w: no valid cues", ErrInvalid)
	}
	return cues, nil
}

// Find returns the best contiguous transcript match. Punctuation and case are
// ignored; a small token edit distance accommodates imperfect ASR captions.
func Find(cues []Cue, quote string) (Match, bool) {
	query := words(quote)
	if len(query) == 0 || len(query) > 64 {
		return Match{}, false
	}
	tokens := make([]captionToken, 0, len(cues)*4)
	for cueIndex, cue := range cues {
		if cue.StartMS < 0 || cue.EndMS <= cue.StartMS || strings.TrimSpace(cue.Text) == "" {
			continue
		}
		for _, word := range words(cue.Text) {
			tokens = append(tokens, captionToken{word: word, cue: cueIndex})
		}
	}
	if len(tokens) == 0 {
		return Match{}, false
	}

	if start, end, ok := exactTokens(cues, tokens, query); ok {
		return buildMatch(cues, tokens[start].cue, tokens[end-1].cue, true, 1), true
	}
	if len(query) < 2 {
		return Match{}, false
	}
	bestStart, bestEnd, bestScore := 0, 0, 0.0
	minimumLength := max(1, len(query)-2)
	maximumLength := len(query) + 2
	previous := make([]int, maximumLength+1)
	current := make([]int, maximumLength+1)
	for start := range tokens {
		for length := minimumLength; length <= maximumLength && start+length <= len(tokens); length++ {
			end := start + length
			startCue, endCue := tokens[start].cue, tokens[end-1].cue
			if cues[endCue].EndMS-cues[startCue].StartMS > maxMatchGapMS {
				continue
			}
			distance := tokenDistance(query, tokens[start:end], previous, current)
			score := 1 - float64(distance)/float64(max(len(query), length))
			if score > bestScore {
				bestStart, bestEnd, bestScore = start, end, score
			}
		}
	}
	if bestScore < minFuzzyScore {
		return Match{}, false
	}
	return buildMatch(cues, tokens[bestStart].cue, tokens[bestEnd-1].cue, false, math.Round(bestScore*1000)/1000), true
}

type captionToken struct {
	word string
	cue  int
}

func readLines(reader io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	lines := make([]string, 0, 4096)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSuffix(scanner.Text(), "\r"))
		if len(lines) > maxCaptionLines {
			return nil, fmt.Errorf("%w: too many lines", ErrInvalid)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: read captions: %v", ErrInvalid, err)
	}
	return lines, nil
}

func nextBlock(lines []string, index int) int {
	for index < len(lines) && strings.TrimSpace(lines[index]) != "" {
		index++
	}
	return index + 1
}

func parseTiming(line string) (int64, int64, bool) {
	parts := strings.SplitN(line, "-->", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok := parseTimestamp(strings.TrimSpace(parts[0]))
	if !ok {
		return 0, 0, false
	}
	endField := strings.Fields(strings.TrimSpace(parts[1]))
	if len(endField) == 0 {
		return 0, 0, false
	}
	end, ok := parseTimestamp(endField[0])
	return start, end, ok && end > start
}

func parseTimestamp(value string) (int64, bool) {
	value = strings.Replace(value, ",", ".", 1)
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, false
	}
	hours := int64(0)
	if len(parts) == 3 {
		var err error
		hours, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || hours < 0 {
			return 0, false
		}
		parts = parts[1:]
	}
	minutes, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, false
	}
	secondParts := strings.Split(parts[1], ".")
	if len(secondParts) != 2 || len(secondParts[1]) < 1 || len(secondParts[1]) > 3 {
		return 0, false
	}
	seconds, err := strconv.ParseInt(secondParts[0], 10, 64)
	if err != nil || seconds < 0 || seconds > 59 {
		return 0, false
	}
	fraction := secondParts[1] + strings.Repeat("0", 3-len(secondParts[1]))
	milliseconds, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, false
	}
	return ((hours*60+minutes)*60+seconds)*1000 + milliseconds, true
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
	return strings.Join(strings.Fields(html.UnescapeString(output.String())), " ")
}

func words(value string) []string {
	value = strings.ToLower(value)
	var normalized strings.Builder
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsNumber(char) {
			normalized.WriteRune(char)
		} else {
			normalized.WriteByte(' ')
		}
	}
	return strings.Fields(normalized.String())
}

func exactTokens(cues []Cue, tokens []captionToken, query []string) (int, int, bool) {
	for start := 0; start+len(query) <= len(tokens); start++ {
		matches := true
		for offset, word := range query {
			if tokens[start+offset].word != word {
				matches = false
				break
			}
		}
		if matches && cues[tokens[start+len(query)-1].cue].EndMS-cues[tokens[start].cue].StartMS <= maxMatchGapMS {
			return start, start + len(query), true
		}
	}
	return 0, 0, false
}

func buildMatch(cues []Cue, startCue, endCue int, exact bool, confidence float64) Match {
	text := make([]string, 0, endCue-startCue+1)
	for index := startCue; index <= endCue; index++ {
		text = append(text, cues[index].Text)
	}
	return Match{
		Text: strings.Join(text, " "), StartMS: cues[startCue].StartMS,
		EndMS: cues[endCue].EndMS, Exact: exact, Confidence: confidence,
	}
}

func tokenDistance(left []string, right []captionToken, previous, current []int) int {
	previous = previous[:len(right)+1]
	current = current[:len(right)+1]
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftWord := range left {
		current[0] = leftIndex + 1
		for rightIndex, rightToken := range right {
			cost := 1
			if leftWord == rightToken.word {
				cost = 0
			}
			current[rightIndex+1] = min(
				current[rightIndex]+1,
				previous[rightIndex+1]+1,
				previous[rightIndex]+cost,
			)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}
