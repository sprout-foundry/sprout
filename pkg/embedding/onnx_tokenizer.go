package embedding

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// GemmaTokenizer implements Byte-level BPE tokenization for Gemma/EmbeddingGemma models.
//
// It parses the HuggingFace tokenizer.json format and encodes/decodes text
// using the BytePiece algorithm with Gemma-specific byte encoding.
//
// Gemma uses a specific byte fallback encoding where bytes 1-255 are represented
// as ⟨0x01⟩ through ⟨0xFF⟩ (using Unicode private use area U+E000-U+EFFD).
type GemmaTokenizer struct {
	vocab     map[string]int32
	vocabSize int
	merges    []string
	bpeRanks  map[string]int // merged token → rank (position in merge list)

	// Reverse mapping for decoding.
	idToToken map[int32]string

	// Special tokens.
	bosToken string
	eosToken string
	unkToken string

	// Pre-tokenizer regex from tokenizer.json (Gemma uses subword split).
	preTokenizer *regexp.Regexp
}

// tokenizerJSON is a partial representation of the HuggingFace tokenizer.json.
type tokenizerJSON struct {
	Model struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []interface{}  `json:"merges"` // can be ["a","b"] arrays or "a b" strings
	} `json:"model"`
	AddedTokens []struct {
		ID          int    `json:"id"`
		Value       string `json:"value"`
		Special     bool   `json:"special"`
	} `json:"added_tokens,omitempty"`
	PreTokenizer struct {
		Type    string      `json:"type"`
		Pattern interface{} `json:"pattern"` // can be string or object like {"String": " "}
	} `json:"pre_tokenizer,omitempty"`
	Decoder struct {
		Type  string `json:"type"`
		Piece string `json:"piece"`
	} `json:"decoder,omitempty"`
}

// NewGemmaTokenizer creates a tokenizer from a tokenizer.json file.
func NewGemmaTokenizer(path string) (*GemmaTokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tokenizer: read file: %w", err)
	}

	var tj tokenizerJSON
	if err := json.Unmarshal(data, &tj); err != nil {
		return nil, fmt.Errorf("tokenizer: parse json: %w", err)
	}

	if tj.Model.Type != "BPE" {
		return nil, fmt.Errorf("tokenizer: expected BPE model, got %q", tj.Model.Type)
	}

	t := &GemmaTokenizer{
		vocab:     make(map[string]int32, len(tj.Model.Vocab)),
		vocabSize: len(tj.Model.Vocab),
		bpeRanks:  make(map[string]int, len(tj.Model.Merges)),
		idToToken: make(map[int32]string, len(tj.Model.Vocab)),
	}

	// Build vocab and reverse mappings.
	for token, id := range tj.Model.Vocab {
		t.vocab[token] = int32(id)
		t.idToToken[int32(id)] = token
	}

	// Build merge ranking (order matters for BPE).
	for i, raw := range tj.Model.Merges {
		var merge string
		switch m := raw.(type) {
		case string:
			merge = m // "a b" format
		case []interface{}:
			// ["a", "b"] array format
			if len(m) >= 2 {
				if a, ok := m[0].(string); ok {
					if b, ok := m[1].(string); ok {
						merge = a + " " + b
					}
				}
			}
		}
		if merge == "" {
			continue
		}
		t.merges = append(t.merges, merge)
		t.bpeRanks[merge] = i
	}

	// Find special tokens.
	t.bosToken = "<bos>"
	t.eosToken = "<eos>"
	t.unkToken = "<unk>"

	// Parse pre-tokenizer regex if present.
	switch p := tj.PreTokenizer.Pattern.(type) {
	case string:
		if p != "" {
			t.preTokenizer = regexp.MustCompile(p)
		}
	case map[string]interface{}:
		// HuggingFace format: {"String": " "} or {"Regex": "..."}
		if regex, ok := p["Regex"]; ok {
			if s, ok := regex.(string); ok && s != "" {
				t.preTokenizer = regexp.MustCompile(s)
			}
		}
		// "String" type means split on literal string — we use it as a simple split
	}

	return t, nil
}

// Encode converts text to a sequence of token IDs using Byte-level BPE.
//
// The algorithm:
// 1. Byte-encode the input (Gemma byte fallback for non-ASCII)
// 2. Split into subwords using the pre-tokenizer (or simple whitespace split)
// 3. Apply BPE merges iteratively on each subword
// 4. Concatenate token IDs
func (t *GemmaTokenizer) Encode(text string) []int32 {
	if text == "" {
		return nil
	}

	// Step 1: Byte-encode input into strings suitable for BPE.
	// Gemma uses byte fallback: each byte becomes a token string.
	// ASCII bytes are themselves, non-ASCII bytes are ⟨0xNN⟩ format.
	bytesStr := t.byteEncode(text)

	// Step 2: Split into subwords.
	subwords := t.splitPreTokens(bytesStr)

	// Step 3: BPE merge each subword and collect token IDs.
	var tokens []int32
	for _, sub := range subwords {
		subTokens := t.bpe(sub)
		tokens = append(tokens, subTokens...)
	}

	return tokens
}

// Decode converts a sequence of token IDs back to text.
func (t *GemmaTokenizer) Decode(tokens []int32) string {
	var sb strings.Builder
	for _, id := range tokens {
		token, ok := t.idToToken[id]
		if !ok {
			sb.WriteString("<unk>")
			continue
		}
		decoded := t.decodeToken(token)
		sb.WriteString(decoded)
	}
	return sb.String()
}

// byteEncode converts UTF-8 text to the Gemma byte-encoding representation.
//
// In Gemma's BPE scheme:
// - ASCII characters (0x01-0x7F) are used directly
// - Non-ASCII bytes are encoded as ⟨0xNN⟩ using the Unicode block U+E000-U+EFFD
//
// This matches the encoding used in the tokenizer.json vocab.
func (t *GemmaTokenizer) byteEncode(text string) string {
	var sb strings.Builder
	for _, b := range []byte(text) {
		if b >= 0x01 && b <= 0x7F {
			sb.WriteByte(b)
		} else {
			// Gemma byte fallback: ⟨0xNN⟩ → Unicode U+E000+0xNN-1
			// The tokenizer.json uses format like ⟨0x80⟩ which maps to U+E000
			sb.WriteRune(0xE000 + rune(b) - 1)
		}
	}
	return sb.String()
}

// decodeToken converts a single token string back to its decoded text form.
// This reverses the byte encoding and handles the leading space convention
// used in BPE tokenization.
func (t *GemmaTokenizer) decodeToken(token string) string {
	// BPE tokens typically have a leading space to mark word boundaries,
	// except for the first token in a sequence. We strip the leading space
	// during decoding, but this is handled at a higher level. For now,
	// just reverse the byte encoding.

	var sb strings.Builder
	for _, r := range token {
		if r >= 0xE000 && r <= 0xEF00 {
			// Reverse byte fallback.
			sb.WriteByte(byte(r - 0xE000 + 1))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// splitPreTokens splits the byte-encoded string into subwords for BPE processing.
// Gemma uses a regex-based pre-tokenizer that splits on punctuation and whitespace.
func (t *GemmaTokenizer) splitPreTokens(s string) []string {
	if t.preTokenizer != nil {
		return t.preTokenizer.FindAllString(s, -1)
	}

	// Fallback: simple whitespace + punctuation split.
	// This matches the common pre-tokenizer pattern used by Gemma models.
	var tokens []string
	var current strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// bpe applies byte-pair encoding merges to a single subword string,
// returning the resulting token IDs.
func (t *GemmaTokenizer) bpe(piece string) []int32 {
	if len(piece) == 0 {
		return nil
	}

	// Split into individual character tokens.
	symbols := splitStringIntoRunes(piece)

	if len(symbols) == 1 {
		id, ok := t.vocab[symbols[0]]
		if ok {
			return []int32{id}
		}
		// Unknown token — fall back to byte-level encoding.
		return t.fallbackToBytes(piece)
	}

	// Iteratively apply merges.
	// Each iteration finds the pair with the lowest rank and merges it.
	for {
		// Find the pair with the lowest merge rank.
		pair, rank := t.bestMerge(symbols)
		if _, ok := t.bpeRanks[pair]; !ok {
			break // No more merges available
		}

		// Apply the merge.
		symbols = t.applyMerge(symbols, pair, rank)
	}

	// Convert merged tokens to IDs.
	var ids []int32
	for _, token := range symbols {
		id, ok := t.vocab[token]
		if ok {
			ids = append(ids, id)
		} else {
			// Token not in vocab — shouldn't happen if merges are correct,
			// but handle gracefully.
			ids = append(ids, t.fallbackToBytes(token)...)
		}
	}

	return ids
}

// bestMerge finds the merge pair with the lowest rank (applied first).
func (t *GemmaTokenizer) bestMerge(symbols []string) (string, int) {
	bestRank := int(1<<31 - 1)
	bestPair := ""

	for i := 0; i < len(symbols)-1; i++ {
		pair := symbols[i] + symbols[i+1]
		if rank, ok := t.bpeRanks[pair]; ok && rank < bestRank {
			bestRank = rank
			bestPair = pair
		}
	}

	return bestPair, bestRank
}

// applyMerge applies all instances of the best merge pair in the symbol list,
// using the standard BPE algorithm that merges all occurrences of the best pair.
func (t *GemmaTokenizer) applyMerge(symbols []string, pair string, rank int) []string {
	first, second := splitMergePair(pair)

	// Use the HuggingFace BPE approach: scan through the list, merging
	// consecutive pairs that match the current best merge.
	var newSymbols []string
	i := 0
	for i < len(symbols) {
		// Try to match this pair.
		if i+1 < len(symbols) && symbols[i] == first && symbols[i+1] == second {
			// Found a match — but we need to check if this pair is still
			// the best. If a better pair appeared at or before this position,
			// we need to re-scan from the beginning.
			pairRank, ok := t.bpeRanks[symbols[i]+symbols[i+1]]
			if ok && pairRank == rank {
				newSymbols = append(newSymbols, pair)
				i += 2
				continue
			}
		}
		newSymbols = append(newSymbols, symbols[i])
		i++
	}

	return newSymbols
}

// splitMergePair splits a merged token back into its two constituent parts.
// The merge format is "tok1 tok2" (space-separated).
func splitMergePair(pair string) (string, string) {
	idx := strings.Index(pair, " ")
	if idx < 0 {
		return pair, ""
	}
	return pair[:idx], pair[idx+1:]
}

// splitStringIntoRunes splits a string into a slice of single-rune strings.
func splitStringIntoRunes(s string) []string {
	var result []string
	for _, r := range s {
		result = append(result, string(r))
	}
	return result
}

// fallbackToBytes handles tokens not found in the vocabulary by
// falling back to individual byte encoding.
func (t *GemmaTokenizer) fallbackToBytes(token string) []int32 {
	var ids []int32
	for _, r := range token {
		if id, ok := t.vocab[string(r)]; ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		// Absolute fallback: use unknown token if available.
		if unk, ok := t.vocab[t.unkToken]; ok {
			return []int32{unk}
		}
	}
	return ids
}

// EncodeWithBOS returns the encoded tokens with the BOS token prepended.
func (t *GemmaTokenizer) EncodeWithBOS(text string) []int32 {
	tokens := t.Encode(text)
	if bosID, ok := t.vocab[t.bosToken]; ok {
		tokens = append([]int32{bosID}, tokens...)
	}
	return tokens
}

// EncodeWithBOSAndEOS returns the encoded tokens with BOS prepended and EOS appended.
func (t *GemmaTokenizer) EncodeWithBOSAndEOS(text string) []int32 {
	tokens := t.Encode(text)
	if bosID, ok := t.vocab[t.bosToken]; ok {
		tokens = append([]int32{bosID}, tokens...)
	}
	if eosID, ok := t.vocab[t.eosToken]; ok {
		tokens = append(tokens, eosID)
	}
	return tokens
}

// VocabSize returns the size of the vocabulary.
func (t *GemmaTokenizer) VocabSize() int {
	return t.vocabSize
}

// TokenIDs returns the token IDs for the given text (alias for Encode).
func (t *GemmaTokenizer) TokenIDs(text string) []int32 {
	return t.Encode(text)
}

// TokenizeWithTokens returns both the token strings and their IDs for debugging.
func (t *GemmaTokenizer) TokenizeWithTokens(text string) ([]string, []int32) {
	ids := t.Encode(text)
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i] = t.idToToken[id]
	}
	return tokens, ids
}

// EncodeBatch tokenizes multiple texts and returns a padded batch of token IDs.
// The padding value is padID (typically 0). All sequences are padded to the
// max length found in the batch.
func (t *GemmaTokenizer) EncodeBatch(texts []string, padID int32) [][]int32 {
	results := make([][]int32, len(texts))
	maxLen := 0
	for i, text := range texts {
		results[i] = t.Encode(text)
		if len(results[i]) > maxLen {
			maxLen = len(results[i])
		}
	}

	// Pad to max length.
	for i := range results {
		if len(results[i]) < maxLen {
			results[i] = slices.Grow(results[i], maxLen-len(results[i]))
			for j := len(results[i]); j < maxLen; j++ {
				results[i] = append(results[i], padID)
			}
		}
	}

	return results
}

// MaskBatch creates attention masks for a batch of encoded sequences.
// Returns a batch of int64 masks (1 = real token, 0 = padding).
func (t *GemmaTokenizer) MaskBatch(encoded [][]int32, padID int32) [][]int64 {
	masks := make([][]int64, len(encoded))
	for i, seq := range encoded {
		masks[i] = make([]int64, len(seq))
		for j, id := range seq {
			if id != padID {
				masks[i][j] = 1
			}
		}
	}
	return masks
}
