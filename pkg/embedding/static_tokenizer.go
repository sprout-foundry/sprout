package embedding

import (
	"fmt"
	"strings"
)

// StaticTokenizer implements a SentencePiece-style BPE tokenizer.
// It uses greedy longest-prefix matching with byte fallback.
type StaticTokenizer struct {
	vocabMap        map[string]uint16 // token string -> ID
	vocabSize       int
	unkID           uint16
	usesSpacePrefix bool // true if tokenizer uses ▁ (U+2581) space prefix
}

// Tokenize converts text to a list of token IDs using the full pipeline:
// 1. Pre-tokenize: split on whitespace
// 2. Prepend ▁ to non-first words (if usesSpacePrefix)
// 3. For each word segment, apply longest-prefix matching with byte fallback
func (t *StaticTokenizer) Tokenize(text string) []uint16 {
	// Pre-tokenize: split on whitespace
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var ids []uint16
	for i, word := range words {
		// Prepend ▁ (U+2581) to non-first words if tokenizer uses space prefix
		if t.usesSpacePrefix && i > 0 {
			word = "\u2581" + word
		}
		ids = append(ids, t.tokenizeWord(word)...)
	}
	return ids
}

// tokenizeWord applies longest-prefix matching to a single word segment.
func (t *StaticTokenizer) tokenizeWord(word string) []uint16 {
	var ids []uint16
	i := 0

	for i < len(word) {
		bestLen := 0
		bestID := t.unkID

		// Try longest match first (limit to reasonable length)
		maxLen := i + 32
		if maxLen > len(word) {
			maxLen = len(word)
		}

		for end := maxLen; end > i; end-- {
			sub := word[i:end]
			if id, ok := t.vocabMap[sub]; ok {
				bestLen = end - i
				bestID = id
				break // longest match found
			}
		}

		if bestLen == 0 {
			// Byte fallback: encode current byte as <0xNN>
			b := word[i]
			if b < 128 {
				key := fmt.Sprintf("<0x%02X>", b)
				if id, ok := t.vocabMap[key]; ok {
					bestID = id
					bestLen = 1
				}
			}
			// Still no match: use UNK and advance one byte
			if bestLen == 0 {
				bestID = t.unkID
				bestLen = 1
			}
		}

		ids = append(ids, bestID)
		i += bestLen
	}

	return ids
}

// TokenizeBatch tokenizes multiple texts.
func (t *StaticTokenizer) TokenizeBatch(texts []string) [][]uint16 {
	results := make([][]uint16, len(texts))
	for i, text := range texts {
		results[i] = t.Tokenize(text)
	}
	return results
}
