package semantic

import (
	"context"
	"hash/fnv"
	"strings"
)

// LexicalDimensions is the width of the offline vector. It is small enough to
// score a full result page in microseconds and wide enough that unrelated
// terms rarely collide.
const LexicalDimensions = 256

// Lexical is a deterministic offline embedder. It hashes stemmed word tokens
// and character 4-grams into a fixed-width vector, which recovers most
// morphological and typo-level matches ("running" against "runs", "sprinkler"
// against "sprinklers") without a model, a key, or a network call.
//
// It is deliberately weaker than a trained sentence encoder: it cannot relate
// "puppy" to "dog". It exists so ranking degrades instead of disappearing when
// no model is configured, and so the ranking path stays testable offline.
type Lexical struct{}

func (Lexical) Descriptor() Descriptor {
	return Descriptor{ID: "lexical", Label: "Offline lexical vectors", Local: true, Dimensions: LexicalDimensions}
}

func (l Lexical) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	if err := ValidateInputs(inputs); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(inputs))
	for index, input := range inputs {
		vectors[index] = l.vector(input)
	}
	return vectors, nil
}

func (Lexical) vector(input string) []float32 {
	vector := make([]float32, LexicalDimensions)
	for _, token := range lexicalTokens(input) {
		add(vector, "w:"+token, 1)
		// Character n-grams carry the shared shape of related word forms.
		padded := "^" + token + "$"
		for start := 0; start+4 <= len(padded); start++ {
			add(vector, "g:"+padded[start:start+4], 0.4)
		}
	}
	return Unit(vector)
}

// add applies the hashing trick with a sign bit so unrelated features that
// land in the same bucket cancel out instead of reinforcing each other.
func add(vector []float32, feature string, weight float32) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(feature))
	sum := h.Sum32()
	bucket := int(sum % uint32(len(vector)))
	if sum&0x80000000 != 0 {
		weight = -weight
	}
	vector[bucket] += weight
}

func lexicalTokens(input string) []string {
	fields := strings.FieldsFunc(strings.ToLower(input), func(symbol rune) bool {
		return !(symbol >= 'a' && symbol <= 'z' || symbol >= '0' && symbol <= '9')
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) < 2 || lexicalStopwords[field] {
			continue
		}
		tokens = append(tokens, stem(field))
	}
	return tokens
}

// stem removes the inflectional endings that matter most for catalog search.
// It is intentionally shallow: over-stemming loses more precision than the
// recall it buys at this vector width.
func stem(token string) string {
	switch {
	case len(token) > 5 && strings.HasSuffix(token, "ing"):
		return token[:len(token)-3]
	case len(token) > 4 && strings.HasSuffix(token, "ed"):
		return token[:len(token)-2]
	case len(token) > 4 && strings.HasSuffix(token, "es"):
		return token[:len(token)-2]
	case len(token) > 3 && strings.HasSuffix(token, "s") && !strings.HasSuffix(token, "ss"):
		return token[:len(token)-1]
	default:
		return token
	}
}

var lexicalStopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true,
	"from": true, "was": true, "were": true, "are": true, "its": true, "it": true,
	"of": true, "in": true, "on": true, "at": true, "to": true, "by": true,
	"an": true, "as": true, "is": true, "be": true, "or": true, "a": true,
}
