package embedding

import (
	"math"
	"strings"
	"unicode"
)

// StaticTokenizer implements tokenization for three tokenizer types:
// - BPE/SentencePiece (tokenizerType==0): ▁ (U+2581) space prefix, greedy matching
// - WordPiece (tokenizerType==1): ## subword prefix, BERT pre-tokenization pipeline
// - Unigram (tokenizerType==2): Viterbi decoding with log probabilities
//
// For Unigram models with v3 binary format, the tokenizer uses proper Viterbi
// decoding instead of greedy longest-prefix matching. This matches the behavior
// of HuggingFace tokenizers and SentencePiece.
//
// For Unigram models with v2 format (no full vocab/weights/mapping), falls back
// to greedy longest-prefix matching (same as BPE/SentencePiece).
type StaticTokenizer struct {
	vocabMap        map[string]uint16    // embedding vocab: token string -> embedding ID
	vocabSize       int                  // embedding vocab size
	unkID           uint16               // unknown token ID in embedding vocab
	usesSpacePrefix bool                 // true if tokenizer uses ▁ (U+2581) space prefix
	tokenizerType   uint8                // 0=BPE, 1=WordPiece, 2=Unigram

	// v3 Unigram fields (populated when HasViterbiData())
	vocabFull    []string       // full unigram vocab (276k tokens), ordered by tokenizer ID
	vocabFullMap map[string]uint32 // full vocab: token string -> tokenizer ID
	weights      []float32      // log probabilities for each tokenizer ID
	mapping      []int32        // tokenizer ID -> embedding ID
}

// --- Pre-tokenization ---

// preTokenize splits text into word segments for the tokenizer.
// - Unigram/BPE/SentencePiece: whitespace split + ▁ prefix (Metaspace)
// - WordPiece: BERT pre-tokenization (lowercase + whitespace + punctuation split)
func (t *StaticTokenizer) preTokenize(text string) []string {
	if t.tokenizerType == 1 {
		// WordPiece: BERT pre-tokenization
		return t.bertPreTokenize(text)
	}

	// Unigram/BPE/SentencePiece: Metaspace pre-tokenization
	// Matches HuggingFace {"type": "Sequence", "pretokenizers": [
	//   {"type": "WhitespaceSplit"},
	//   {"type": "Metaspace", "replacement": "\u2581", "prepend_scheme": "always", "split": true}
	// ]}
	return t.metaspacePreTokenize(text)
}

// metaspacePreTokenize implements the Metaspace pre-tokenizer used by Unigram/BPE models.
// Steps:
//   1. Split on whitespace (WhitespaceSplit)
//   2. Prepend ▁ to each whitespace-delimited token (Metaspace with prepend_scheme="always")
//
// This is simpler than BERT pre-tokenization and matches the HuggingFace tokenizer.json
// pre_tokenizer configuration for nomic-embed-text-v2-moe and similar models.
func (t *StaticTokenizer) metaspacePreTokenize(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})

	var result []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		// Metaspace with prepend_scheme="always": prepend ▁ to EVERY token
		// (including the first one), not just tokens after whitespace
		part = "▁" + part
		result = append(result, part)
	}
	return result
}

// bertPreTokenize implements the BERT pre-tokenization step matching HuggingFace:
// 1. Lowercase (BertNormalizer) — MUST happen before splitting
// 2. Split on whitespace (BertPreTokenizer)
// 3. Punctuation splitting (BertPreTokenizer)
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

// --- Tokenization ---

// Tokenize converts text to a list of embedding token IDs using the full pipeline.
func (t *StaticTokenizer) Tokenize(text string) []uint16 {
	words := t.preTokenize(text)
	if len(words) == 0 {
		return nil
	}

	// Choose tokenization strategy
	if t.tokenizerType == 2 && t.vocabFullMap != nil {
		// Unigram Viterbi (v3 format)
		return t.tokenizeWithViterbi(words)
	}

	// Greedy longest-prefix (BPE/SentencePiece/WordPiece, or v2 Unigram fallback)
	var ids []uint16
	for _, word := range words {
		ids = append(ids, t.tokenizeWordGreedy(word)...)
	}
	return ids
}

// tokenizeWithViterbi applies Viterbi decoding across all pre-tokenized segments.
// Returns embedding IDs (mapped through the mapping array).
func (t *StaticTokenizer) tokenizeWithViterbi(words []string) []uint16 {
	var result []uint16
	for _, word := range words {
		ids := t.viterbiTokenizeWord(word)
		result = append(result, ids...)
	}
	return result
}

// viterbiTokenizeWord applies Unigram Viterbi decoding to a single pre-tokenized word.
// Returns embedding IDs (mapped through the mapping array).
//
// The algorithm:
// 1. Convert word to runes (for proper Unicode handling)
// 2. Build ngram scores: for each position [i,j), look up the token in vocabFullMap
//    and get its log probability from weights.
// 3. Viterbi forward pass to find the best segmentation path
// 4. Backtrack to get token boundaries
// 5. Map tokenizer IDs to embedding IDs through the mapping array
// 6. For unmatched characters, fall back to UNK (one rune at a time)
func (t *StaticTokenizer) viterbiTokenizeWord(word string) []uint16 {
	runes := []rune(word)
	n := len(runes)

	if n == 0 {
		return nil
	}

	// bestScore[i] = best log probability to tokenize runes[0:i]
	// bestPrev[i] = the j where the best path to i came from (token was runes[j:i])
	bestScore := make([]float32, n+1)
	bestPrev := make([]int32, n+1)

	// Initialize
	bestScore[0] = 0
	for i := 1; i <= n; i++ {
		bestScore[i] = -math.MaxFloat32
		bestPrev[i] = -1
	}

	// Forward pass: for each position i, try all possible token start positions j
	for i := 1; i <= n; i++ {
		// Try tokens of various lengths ending at position i
		// Limit token length to 50 runes (max reasonable token size)
		maxLen := 50
		if i > maxLen {
			maxLen = i
		}
		for j := i - maxLen; j >= 0; j-- {
			tokenStr := string(runes[j:i])
			tokenID, ok := t.vocabFullMap[tokenStr]
			if !ok {
				continue
			}

			score := bestScore[j] + t.weights[tokenID]
			if score > bestScore[i] {
				bestScore[i] = score
				bestPrev[i] = int32(j)
			}
		}
	}

	// If we couldn't reach the end with valid tokens, we need to handle unmatched chars.
	// Strategy: find the longest prefix that IS tokenizable, emit UNK for unmatched chars,
	// then try again from the first unmatched position.
	if bestPrev[n] == -1 || bestScore[n] == -math.MaxFloat32 {
		return t.viterbiTokenizeWithUnkFallback(word, runes)
	}

	// Backtrack to get token boundaries
	var tokenizerIDs []uint32
	pos := n
	for pos > 0 {
		prev := bestPrev[pos]
		if prev < 0 {
			// Shouldn't happen if we reached the end, but safety fallback
			tokenizerIDs = append(tokenizerIDs, uint32(t.unkID))
			pos--
			continue
		}
		tokenStr := string(runes[prev:pos])
		tokenID, ok := t.vocabFullMap[tokenStr]
		if !ok {
			tokenizerIDs = append(tokenizerIDs, uint32(t.unkID))
		} else {
			tokenizerIDs = append(tokenizerIDs, tokenID)
		}
		pos = int(prev)
	}

	// Reverse (we built backward)
	for i, j := 0, len(tokenizerIDs)-1; i < j; i, j = i+1, j-1 {
		tokenizerIDs[i], tokenizerIDs[j] = tokenizerIDs[j], tokenizerIDs[i]
	}

	// Map tokenizer IDs to embedding IDs
	return t.mapToEmbeddingIDs(tokenizerIDs)
}

// viterbiTokenizeWithUnkFallback handles words with characters that don't form any valid token.
// It greedily finds the longest tokenizable prefix/suffix and emits UNK for gaps.
func (t *StaticTokenizer) viterbiTokenizeWithUnkFallback(word string, runes []rune) []uint16 {
	n := len(runes)
	var tokenizerIDs []uint32

	pos := 0
	for pos < n {
		// Try to find the longest token starting at pos
		bestLen := 0
		bestTokenID := uint32(0)

		maxLen := 50
		if pos+maxLen > n {
			maxLen = n - pos
		}
		for end := pos + maxLen; end > pos; end-- {
			tokenStr := string(runes[pos:end])
			tokenID, ok := t.vocabFullMap[tokenStr]
			if ok {
				bestLen = end - pos
				bestTokenID = tokenID
				break
			}
		}

		if bestLen > 0 {
			tokenizerIDs = append(tokenizerIDs, bestTokenID)
			pos += bestLen
		} else {
			// No valid token starting here — emit UNK for this rune
			tokenizerIDs = append(tokenizerIDs, uint32(t.unkID))
			pos++
		}
	}

	return t.mapToEmbeddingIDs(tokenizerIDs)
}

// mapToEmbeddingIDs converts tokenizer IDs to embedding IDs using the mapping array.
// If a tokenizer ID maps to -1 (unmapped), it is skipped (not included in output).
func (t *StaticTokenizer) mapToEmbeddingIDs(tokenizerIDs []uint32) []uint16 {
	if t.mapping == nil {
		// No mapping — tokenizer IDs are embedding IDs
		result := make([]uint16, len(tokenizerIDs))
		for i, id := range tokenizerIDs {
			result[i] = uint16(id)
		}
		return result
	}

	var result []uint16
	for _, tid := range tokenizerIDs {
		if int(tid) >= len(t.mapping) {
			continue
		}
		eid := t.mapping[tid]
		if eid < 0 {
			continue // unmapped token, skip
		}
		result = append(result, uint16(eid))
	}
	return result
}

// tokenizeWordGreedy applies greedy longest-prefix matching to a single word segment.
// Used for BPE/SentencePiece/WordPiece and as fallback for v2 Unigram.
func (t *StaticTokenizer) tokenizeWordGreedy(word string) []uint16 {
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
			if !t.usesSpacePrefix && len(ids) > 0 {
				key := "##" + sub
				if id, ok := t.vocabMap[key]; ok {
					bestLen = end - i
					bestID = id
					break
				}
			}

			// Direct lookup
			if id, ok := t.vocabMap[sub]; ok {
				bestLen = end - i
				bestID = id
				break
			}
		}

		if bestLen == 0 {
			bestID = t.unkID
			bestLen = 1
		}

		ids = append(ids, bestID)
		i += bestLen
	}

	return ids
}

// --- Debug/inspection methods ---

// TokenizeWithTokens returns both token strings and embedding IDs for debugging.
func (t *StaticTokenizer) TokenizeWithTokens(text string) ([]string, []uint16) {
	words := t.preTokenize(text)
	if len(words) == 0 {
		return nil, nil
	}

	if t.tokenizerType == 2 && t.vocabFullMap != nil {
		return t.tokenizeWithViterbiAndTokens(words)
	}

	// Greedy path
	var tokens []string
	var ids []uint16
	for _, word := range words {
		wordTokens, wordIDs := t.tokenizeWordGreedyWithTokens(word)
		tokens = append(tokens, wordTokens...)
		ids = append(ids, wordIDs...)
	}
	return tokens, ids
}

// tokenizeWithViterbiAndTokens applies Viterbi and returns both token strings and embedding IDs.
func (t *StaticTokenizer) tokenizeWithViterbiAndTokens(words []string) ([]string, []uint16) {
	var allTokens []string
	var allIDs []uint16

	for _, word := range words {
		tokens, ids := t.viterbiTokenizeWordWithTokens(word)
		allTokens = append(allTokens, tokens...)
		allIDs = append(allIDs, ids...)
	}
	return allTokens, allIDs
}

// viterbiTokenizeWordWithTokens applies Viterbi and returns token strings + embedding IDs.
func (t *StaticTokenizer) viterbiTokenizeWordWithTokens(word string) ([]string, []uint16) {
	runes := []rune(word)
	n := len(runes)

	if n == 0 {
		return nil, nil
	}

	// Viterbi forward pass
	bestScore := make([]float32, n+1)
	bestPrev := make([]int32, n+1)
	bestToken := make([]string, n+1) // token string at each position

	bestScore[0] = 0
	for i := 1; i <= n; i++ {
		bestScore[i] = -math.MaxFloat32
		bestPrev[i] = -1
		bestToken[i] = ""
	}

	for i := 1; i <= n; i++ {
		maxLen := 50
		if i > maxLen {
			maxLen = i
		}
		for j := i - maxLen; j >= 0; j-- {
			tokenStr := string(runes[j:i])
			tokenID, ok := t.vocabFullMap[tokenStr]
			if !ok {
				continue
			}

			score := bestScore[j] + t.weights[tokenID]
			if score > bestScore[i] {
				bestScore[i] = score
				bestPrev[i] = int32(j)
				bestToken[i] = tokenStr
			}
		}
	}

	// Check if we reached the end
	if bestPrev[n] == -1 || bestScore[n] == -math.MaxFloat32 {
		return t.viterbiTokenizeWithUnkFallbackAndTokens(word, runes)
	}

	// Backtrack
	var tokens []string
	var tokenizerIDs []uint32
	pos := n
	for pos > 0 {
		prev := bestPrev[pos]
		if prev < 0 {
			tokens = append(tokens, "<UNK>")
			tokenizerIDs = append(tokenizerIDs, uint32(t.unkID))
			pos--
			continue
		}
		tokens = append(tokens, bestToken[pos])
		tokenizerIDs = append(tokenizerIDs, t.vocabFullMap[bestToken[pos]])
		pos = int(prev)
	}

	// Reverse
	for i, j := 0, len(tokens)-1; i < j; i, j = i+1, j-1 {
		tokens[i], tokens[j] = tokens[j], tokens[i]
		tokenizerIDs[i], tokenizerIDs[j] = tokenizerIDs[j], tokenizerIDs[i]
	}

	// Map tokenizer IDs to embedding IDs (with token string preservation)
	return t.mapToEmbeddingIDsWithTokens(tokens, tokenizerIDs)
}

// viterbiTokenizeWithUnkFallbackAndTokens handles words with untokenizable characters.
func (t *StaticTokenizer) viterbiTokenizeWithUnkFallbackAndTokens(word string, runes []rune) ([]string, []uint16) {
	n := len(runes)
	var tokens []string
	var tokenizerIDs []uint32

	pos := 0
	for pos < n {
		bestLen := 0
		bestTokenID := uint32(0)
		bestTokenStr := ""

		maxLen := 50
		if pos+maxLen > n {
			maxLen = n - pos
		}
		for end := pos + maxLen; end > pos; end-- {
			tokenStr := string(runes[pos:end])
			tokenID, ok := t.vocabFullMap[tokenStr]
			if ok {
				bestLen = end - pos
				bestTokenID = tokenID
				bestTokenStr = tokenStr
				break
			}
		}

		if bestLen > 0 {
			tokens = append(tokens, bestTokenStr)
			tokenizerIDs = append(tokenizerIDs, bestTokenID)
			pos += bestLen
		} else {
			tokens = append(tokens, "<UNK>")
			tokenizerIDs = append(tokenizerIDs, uint32(t.unkID))
			pos++
		}
	}

	return t.mapToEmbeddingIDsWithTokens(tokens, tokenizerIDs)
}

// mapToEmbeddingIDsWithTokens maps tokenizer IDs to embedding IDs, updating token strings
// to reflect the actual token from the full vocab.
func (t *StaticTokenizer) mapToEmbeddingIDsWithTokens(tokens []string, tokenizerIDs []uint32) ([]string, []uint16) {
	if t.mapping == nil {
		result := make([]uint16, len(tokens))
		for i, id := range tokenizerIDs {
			result[i] = uint16(id)
		}
		return tokens, result
	}

	var result []string
	var ids []uint16
	for i, tid := range tokenizerIDs {
		if int(tid) >= len(t.mapping) {
			continue
		}
		eid := t.mapping[tid]
		if eid < 0 {
			continue
		}
		result = append(result, tokens[i])
		ids = append(ids, uint16(eid))
	}
	return result, ids
}

// tokenizeWordGreedyWithTokens applies greedy longest-prefix matching and returns tokens + IDs.
func (t *StaticTokenizer) tokenizeWordGreedyWithTokens(word string) ([]string, []uint16) {
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
			bestID = t.unkID
			bestLen = 1
			bestToken = "<UNK>"
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
