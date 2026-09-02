package intent

import "strings"

// Interpret reads a natural-language idea offline and deterministically. It
// replaces guessing derived from a hash of the prompt with a shallow but real
// reading: who or what the frame is about, what they are doing, where, and how
// it should look and move.
//
// It never calls a network service, so it is also the fallback whenever a
// remote interpreter is unavailable.
func Interpret(prompt string) Brief {
	words, boundaries := tokenize(prompt)
	if len(words) == 0 {
		brief, _ := Brief{Source: "local"}.Normalize()
		return brief
	}

	consumed := make([]bool, len(words))
	brief := Brief{Source: "local"}
	brief.Camera = detectCameraPhrase(prompt)

	for index, word := range words {
		if brief.Style == "" {
			if style, ok := styleMarkers[word]; ok {
				brief.Style = style
				consumed[index] = true
				continue
			}
		}
		if brief.Mood == "" {
			if mood, ok := moodMarkers[word]; ok {
				brief.Mood = mood
				// A mood word can also be the subject's action ("celebrating"),
				// so it stays available to the action scan.
				if !isActionWord(word) {
					consumed[index] = true
				}
				continue
			}
		}
		if brief.Camera == "" {
			if camera, ok := cameraMarkers[word]; ok {
				brief.Camera = camera
				// Camera words like "spinning" often describe the subject too,
				// so only pure framing words are removed from the scene text.
				if !isActionWord(word) {
					consumed[index] = true
				}
			}
		}
	}

	actionIndex := -1
	for index, word := range words {
		if isActionWord(word) {
			actionIndex = index
			break
		}
	}
	settingIndex := -1
	for index, word := range words {
		if index > actionIndex && prepositions[word] {
			settingIndex = index
			break
		}
	}

	actionEnd := len(words)
	if settingIndex > actionIndex {
		actionEnd = settingIndex
	}
	if actionIndex >= 0 {
		brief.Action = join(words, consumed, actionIndex, actionEnd, boundaries)
		brief.Subject = trimFiller(join(words, consumed, 0, actionIndex, boundaries))
		if brief.Subject == "" {
			// "spinning vinyl record": the sentence opens with the action, so
			// the subject is whatever the action is applied to.
			brief.Subject = trimFiller(join(words, consumed, actionIndex+1, actionEnd, boundaries))
			brief.Action = words[actionIndex]
		}
	} else if settingIndex > 0 {
		brief.Subject = trimFiller(join(words, consumed, 0, settingIndex, boundaries))
	} else {
		brief.Subject = trimFiller(join(words, consumed, 0, len(words), boundaries))
	}
	if settingIndex >= 0 {
		brief.Setting = join(words, consumed, settingIndex, len(words), boundaries)
	}

	for index, word := range words {
		if consumed[index] || stopwords[word] || articles[word] || prepositions[word] {
			continue
		}
		brief.Keywords = append(brief.Keywords, word)
	}

	normalized, err := brief.Normalize()
	if err != nil {
		// Every value above comes from the closed vocabularies, so this can
		// only mean a lexicon typo. Fall back to a valid default brief.
		fallback, _ := Brief{Source: "local"}.Normalize()
		return fallback
	}
	return normalized
}

// isActionWord treats an "-ing" form as the action unless it is a known noun.
func isActionWord(word string) bool {
	if actionVerbs[word] {
		return true
	}
	return len(word) > 4 && strings.HasSuffix(word, "ing") && !gerundExceptions[word]
}

func detectCameraPhrase(prompt string) string {
	lowered := strings.ToLower(prompt)
	for phrase, camera := range cameraPhrases {
		if strings.Contains(lowered, phrase) {
			return camera
		}
	}
	return ""
}

// join rebuilds a phrase from the token range, dropping tokens that a marker
// already claimed and leading articles. boundaries stops a phrase at the
// punctuation the user wrote so two clauses never merge into one.
func join(words []string, consumed []bool, start, end int, boundaries map[int]bool) string {
	if start < 0 {
		start = 0
	}
	parts := make([]string, 0, end-start)
	for index := start; index < end && index < len(words); index++ {
		if index > start && boundaries[index] {
			break
		}
		if consumed[index] {
			continue
		}
		if len(parts) == 0 && articles[words[index]] {
			continue
		}
		parts = append(parts, words[index])
	}
	for len(parts) > 0 && (articles[parts[len(parts)-1]] || parts[len(parts)-1] == "and") {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, " ")
}

// trimFiller removes the request wrapper people put around an idea — "please
// make me a gif of ..." — from the edges of the subject. Interior words are
// kept because they can carry the relationship between two nouns.
func trimFiller(phrase string) string {
	parts := strings.Fields(phrase)
	isFiller := func(word string) bool {
		return stopwords[word] || articles[word] || prepositions[word]
	}
	for len(parts) > 0 && isFiller(parts[0]) {
		parts = parts[1:]
	}
	for len(parts) > 0 && isFiller(parts[len(parts)-1]) {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, " ")
}

// tokenize lowercases the prompt into words and records which word indexes
// start a new clause because punctuation preceded them.
func tokenize(prompt string) ([]string, map[int]bool) {
	words := make([]string, 0, 24)
	boundaries := make(map[int]bool)
	current := strings.Builder{}
	pendingBoundary := false
	flush := func() {
		if current.Len() == 0 {
			return
		}
		word := strings.Trim(current.String(), "-'")
		current.Reset()
		if word == "" {
			return
		}
		if pendingBoundary {
			boundaries[len(words)] = true
			pendingBoundary = false
		}
		words = append(words, word)
	}
	for _, symbol := range strings.ToLower(prompt) {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= '0' && symbol <= '9', symbol == '-', symbol == '\'':
			current.WriteRune(symbol)
		case symbol == ',' || symbol == '.' || symbol == ';' || symbol == ':' || symbol == '!' || symbol == '?':
			flush()
			pendingBoundary = true
		default:
			flush()
		}
	}
	flush()
	return words, boundaries
}
