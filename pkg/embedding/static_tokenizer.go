package embedding

import (
	"fmt"
	"strings"
	"unicode"
)

// StaticTokenizer implements both SentencePiece-style BPE and WordPiece tokenization.
// - BPE/SentencePiece: uses ▁ (U+2581) as space prefix (usesSpacePrefix=true)
// - WordPiece: uses ## as subword prefix (usesSpacePrefix=false)
// Both use greedy longest-prefix matching with byte fallback.
//
// For WordPiece (BERT/BGE family), the tokenizer implements the full BERT
// pre-tokenization pipeline matching HuggingFace tokenizers:
//   1. Lowercase the input (BertNormalizer — this must happen FIRST, before
//      any pre-tokenization, so that camelCase boundaries are destroyed)
//   2. Split on whitespace (BertPreTokenizer)
//   3. Split punctuation off word boundaries (BertPreTokenizer)
//   4. For each word segment, apply longest-prefix matching with ## subword prefix
//
// This matches the HuggingFace BertNormalizer + BertPreTokenizer pipeline
// used by BGE-base.
type StaticTokenizer struct {
	vocabMap        map[string]uint16 // token string -> ID
	vocabSize       int
	unkID           uint16
	usesSpacePrefix bool // true if tokenizer uses ▁ (U+2581) space prefix (BPE/SentencePiece)
}

// Tokenize converts text to a list of token IDs using the full pipeline.
func (t *StaticTokenizer) Tokenize(text string) []uint16 {
	// Pre-tokenize
	var words []string
	if !t.usesSpacePrefix {
		// BERT/BGE pre-tokenization:
		// BertNormalizer (lowercase) → BertPreTokenizer (whitespace + punctuation split)
		words = t.bertPreTokenize(text)
	} else {
		// BPE/SentencePiece/Unigram: split on whitespace, add ▁ prefix to non-first tokens
		parts := strings.FieldsFunc(text, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		for i, part := range parts {
			if i > 0 {
				part = "▁" + part
			}
			words = append(words, part)
		}
	}
	if len(words) == 0 {
		return nil
	}

	var ids []uint16
	for _, word := range words {
		ids = append(ids, t.tokenizeWord(word)...)
	}
	return ids
}

// TokenizeWithTokens returns both token strings and IDs for debugging.
func (t *StaticTokenizer) TokenizeWithTokens(text string) ([]string, []uint16) {
	// Pre-tokenize
	var words []string
	if !t.usesSpacePrefix {
		words = t.bertPreTokenize(text)
	} else {
		parts := strings.FieldsFunc(text, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		for i, part := range parts {
			if i > 0 {
				part = "▁" + part
			}
			words = append(words, part)
		}
	}
	if len(words) == 0 {
		return nil, nil
	}

	var tokens []string
	var ids []uint16
	for _, word := range words {
		wordTokens, wordIDs := t.tokenizeWordWithTokens(word)
		tokens = append(tokens, wordTokens...)
		ids = append(ids, wordIDs...)
	}
	return tokens, ids
}

// bertPreTokenize implements the BERT pre-tokenization step matching HuggingFace:
// 1. Lowercase (BertNormalizer) — MUST happen before splitting
// 2. Split on whitespace (BertPreTokenizer)
// 3. Punctuation splitting (BertPreTokenizer)
//
// Note: camelCase splitting is NOT done because BertNormalizer lowercases
// the text before the pre-tokenizer sees it. So "cosineSim" becomes
// "cosinesim" and is treated as a single word segment.
func (t *StaticTokenizer) bertPreTokenize(text string) []string {
	// Step 1: Lowercase (BertNormalizer)
	text = strings.ToLower(text)

	// Step 2: Split on whitespace (BertPreTokenizer)
	parts := strings.Split(text, " ")
	var result []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		// Step 3: Split punctuation off word boundaries
		result = append(result, t.splitPunctuation(part)...)
	}
	return result
}

// splitPunctuation splits punctuation characters off word boundaries.
// Also splits internal punctuation (like "don't" -> "do", "n", "'", "t").
// This is the core of BertPreTokenizer's non-whitespace splitting.
func (t *StaticTokenizer) splitPunctuation(text string) []string {
	if text == "" {
		return nil
	}

	var tokens []string
	runes := []rune(text)
	i := 0

	for i < len(runes) {
		r := runes[i]

		if isWordChar(r) {
			// Consume a word character run
			j := i
			for j < len(runes) && isWordChar(runes[j]) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		} else {
			// Punctuation or other non-word character — emit as individual token
			tokens = append(tokens, string(r))
			i++
		}
	}

	return tokens
}

// isWordChar returns true if the rune is a letter or digit (word character).
// This matches the BERT pre-tokenizer definition.
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// tokenizeWord applies longest-prefix matching to a single word segment.
func (t *StaticTokenizer) tokenizeWord(word string) []uint16 {
	var ids []uint16
	i := 0

	for i < len(word) {
		bestLen := 0
		bestID := t.unkID

		maxLen := i + 32
		if maxLen > len(word) {
			maxLen = len(word)
		}

		for end := maxLen; end > i; end-- {
			sub := word[i:end]

			// For WordPiece: subsequent subwords need ## prefix
			// Try ## prefix FIRST for subsequent tokens (matching BERT behavior)
			if !t.usesSpacePrefix && len(ids) > 0 {
				key := "##" + sub
				if id, ok := t.vocabMap[key]; ok {
					bestLen = end - i
					bestID = id
					break
				}
			}

			// Direct lookup (always tried — first subword or fallback after ## fails)
			if id, ok := t.vocabMap[sub]; ok {
				bestLen = end - i
				bestID = id
				break
			}
		}

		if bestLen == 0 {
			b := word[i]
			if b < 128 {
				key := fmt.Sprintf("<0x%02X>", b)
				if id, ok := t.vocabMap[key]; ok {
					bestID = id
					bestLen = 1
				}
			}
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

// tokenizeWordWithTokens applies longest-prefix matching and returns both tokens and IDs.
func (t *StaticTokenizer) tokenizeWordWithTokens(word string) ([]string, []uint16) {
	var tokens []string
	var ids []uint16
	i := 0

	for i < len(word) {
		bestLen := 0
		bestID := t.unkID
		bestToken := ""

		maxLen := i + 32
		if maxLen > len(word) {
			maxLen = len(word)
		}

		for end := maxLen; end > i; end-- {
			sub := word[i:end]

			if !t.usesSpacePrefix && len(ids) > 0 {
				key := "##" + sub
				if id, ok := t.vocabMap[key]; ok {
					bestLen = end - i
					bestID = id
					bestToken = key
					break
				}
			}

			if id, ok := t.vocabMap[sub]; ok {
				bestLen = end - i
				bestID = id
				bestToken = sub
				break
			}
		}

		if bestLen == 0 {
			b := word[i]
			if b < 128 {
				key := fmt.Sprintf("<0x%02X>", b)
				if id, ok := t.vocabMap[key]; ok {
					bestID = id
					bestLen = 1
					bestToken = key
				}
			}
			if bestLen == 0 {
				bestID = t.unkID
				bestLen = 1
				bestToken = "<UNK>"
			}
		}

		tokens = append(tokens, bestToken)
		ids = append(ids, bestID)
		i += bestLen
	}

	return tokens, ids
}

// TokenizeBatch tokenizes multiple texts.
func (t *StaticTokenizer) TokenizeBatch(texts []string) [][]uint16 {
	results := make([][]uint16, len(texts))
	for i, text := range texts {
		results[i] = t.Tokenize(text)
	}
	return results
}
