package embedding

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// GemmaTokenizer implements the HuggingFace BPE tokenization pipeline used by
// Google's EmbeddingGemma-300M model. It is intentionally narrow: it covers
// the exact normalizer / pre-tokenizer / model / added-tokens combination
// that EmbeddingGemma ships with, rather than the full HF tokenizers schema.
//
// Pipeline (matches HuggingFace tokenizers semantics):
//
//  1. Split input around added-token strings (e.g. "\n", "\t", "<bos>"). The
//     matched runs are emitted directly as their IDs; everything else falls
//     through to the BPE path.
//  2. Normalize each non-added segment by replacing " " (U+0020) with the
//     SentencePiece space marker "▁" (U+2581), per the Replace normalizer.
//  3. Apply rank-ordered BPE merges to the normalized text, treated as a
//     sequence of single-rune symbols.
//  4. Map each resulting symbol to its vocab id, falling back to <unk> on miss.
//
// Decoding is not implemented — the embedding path only needs Encode().
type GemmaTokenizer struct {
	vocab    map[string]int32
	bpeRanks map[bpePair]int

	// Added-token state. addedContents is sorted by descending content length
	// so the encode loop can perform longest-match lookups efficiently.
	addedByContent map[string]int32
	addedLengths   []int // unique content lengths, descending

	bosID int32
	eosID int32
	unkID int32
	padID int32

	vocabSize int

	// normalizerPattern is the literal string the normalizer replaces.
	// normalizerContent is what it gets replaced with. For EmbeddingGemma:
	// pattern=" " and content="▁". An empty pattern means normalization is
	// disabled.
	normalizerPattern string
	normalizerContent string
}

// bpePair is a (first, second) pair used as a key into the merge-rank table.
// EmbeddingGemma stores merges as 2-element arrays, not joined strings, so we
// key by the pair directly rather than encoding/decoding a separator.
type bpePair struct {
	first, second string
}

// tokenizerJSON is a tightly-scoped subset of the HuggingFace tokenizer.json
// schema sufficient for EmbeddingGemma. We deliberately ignore decoder,
// post_processor, and most processor fields because the embedding path does
// not need them.
type tokenizerJSON struct {
	Model struct {
		Type   string           `json:"type"`
		Vocab  map[string]int32 `json:"vocab"`
		Merges json.RawMessage  `json:"merges"`
	} `json:"model"`
	AddedTokens []addedTokenJSON `json:"added_tokens,omitempty"`
	Normalizer  json.RawMessage  `json:"normalizer,omitempty"`
}

type addedTokenJSON struct {
	ID      int32  `json:"id"`
	Content string `json:"content"`
	Special bool   `json:"special"`
}

// patternJSON models the discriminated-union pattern field used by HF tokenizers
// (e.g. {"String": " "} or {"Regex": "..."}). EmbeddingGemma uses only the
// String form for its normalizer; we accept Regex too as a forward-compat
// gesture but do not currently apply it.
type patternJSON struct {
	String string `json:"String,omitempty"`
	Regex  string `json:"Regex,omitempty"`
}

type normalizerJSON struct {
	Type    string      `json:"type"`
	Pattern patternJSON `json:"pattern"`
	Content string      `json:"content"`
}

// NewGemmaTokenizer parses a HuggingFace tokenizer.json file produced for an
// EmbeddingGemma-class model and returns an encoder.
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

	merges, err := parseMerges(tj.Model.Merges)
	if err != nil {
		return nil, fmt.Errorf("tokenizer: parse merges: %w", err)
	}

	t := &GemmaTokenizer{
		vocab:          make(map[string]int32, len(tj.Model.Vocab)),
		bpeRanks:       make(map[bpePair]int, len(merges)),
		addedByContent: make(map[string]int32),
		vocabSize:      len(tj.Model.Vocab),
		bosID:          -1, eosID: -1, unkID: -1, padID: -1,
	}
	for token, id := range tj.Model.Vocab {
		t.vocab[token] = id
	}
	for i, m := range merges {
		t.bpeRanks[m] = i
	}

	// Build added-token lookup. We record every added token, special or not —
	// HF treats both classes as atomic during pre-processing, so a literal
	// "\n\n\n" in the input must become token id 109 rather than going through
	// BPE. We also pick out the canonical special-token IDs by content.
	seenLen := make(map[int]struct{})
	for _, at := range tj.AddedTokens {
		if at.Content == "" {
			continue
		}
		t.addedByContent[at.Content] = at.ID
		seenLen[len(at.Content)] = struct{}{}
		switch at.Content {
		case "<bos>":
			t.bosID = at.ID
		case "<eos>":
			t.eosID = at.ID
		case "<unk>":
			t.unkID = at.ID
		case "<pad>":
			t.padID = at.ID
		}
	}
	t.addedLengths = make([]int, 0, len(seenLen))
	for l := range seenLen {
		t.addedLengths = append(t.addedLengths, l)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(t.addedLengths)))

	// Parse the normalizer (only the simple Replace form is recognized).
	if len(tj.Normalizer) > 0 && string(tj.Normalizer) != "null" {
		var n normalizerJSON
		if err := json.Unmarshal(tj.Normalizer, &n); err == nil && n.Type == "Replace" && n.Pattern.String != "" {
			t.normalizerPattern = n.Pattern.String
			t.normalizerContent = n.Content
		}
	}

	return t, nil
}

// parseMerges accepts either of the two HF merge formats:
//   - newer: [["first","second"], ...]
//   - older: ["first second", ...]
//
// EmbeddingGemma uses the newer form; the older form is kept so unit tests
// that hand-craft tiny tokenizers can stay readable.
func parseMerges(raw json.RawMessage) ([]bpePair, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var pairs [][]string
	if err := json.Unmarshal(raw, &pairs); err == nil {
		out := make([]bpePair, 0, len(pairs))
		for i, p := range pairs {
			if len(p) != 2 {
				return nil, fmt.Errorf("merges[%d]: expected 2 elements, got %d", i, len(p))
			}
			out = append(out, bpePair{first: p[0], second: p[1]})
		}
		return out, nil
	}

	var joined []string
	if err := json.Unmarshal(raw, &joined); err != nil {
		return nil, fmt.Errorf("unrecognized merges format: %w", err)
	}
	out := make([]bpePair, 0, len(joined))
	for i, s := range joined {
		idx := strings.Index(s, " ")
		if idx < 0 {
			return nil, fmt.Errorf("merges[%d] %q: no space separator", i, s)
		}
		out = append(out, bpePair{first: s[:idx], second: s[idx+1:]})
	}
	return out, nil
}

// Encode tokenizes text into a sequence of token ids matching what the
// HuggingFace `tokenizers` reference produces for the same input (modulo the
// BOS/EOS markers, which Encode does NOT add — use EncodeWithBOSAndEOS for
// that).
func (t *GemmaTokenizer) Encode(text string) []int32 {
	if text == "" {
		return nil
	}
	var ids []int32
	t.encodeSegment(text, &ids)
	return ids
}

// encodeSegment walks `text` left-to-right, peeling off the leftmost
// longest-matching added-token span at each step and routing the gaps
// through the BPE path.
func (t *GemmaTokenizer) encodeSegment(text string, out *[]int32) {
	for len(text) > 0 {
		start, length, id, found := t.findLeftmostAddedToken(text)
		if !found {
			t.bpeEncode(text, out)
			return
		}
		if start > 0 {
			t.bpeEncode(text[:start], out)
		}
		*out = append(*out, id)
		text = text[start+length:]
	}
}

// findLeftmostAddedToken finds the earliest position in text where any added
// token matches, returning the start offset, the matched length, and the id.
// At each position the longest matching token wins, mirroring HuggingFace's
// tokenizers behavior.
func (t *GemmaTokenizer) findLeftmostAddedToken(text string) (start, length int, id int32, found bool) {
	if len(t.addedByContent) == 0 {
		return 0, 0, 0, false
	}
	for s := 0; s < len(text); s++ {
		for _, l := range t.addedLengths {
			if s+l > len(text) {
				continue
			}
			if mid, ok := t.addedByContent[text[s:s+l]]; ok {
				return s, l, mid, true
			}
		}
	}
	return 0, 0, 0, false
}

// bpeEncode normalizes a segment and runs BPE, appending the resulting token
// ids to out.
func (t *GemmaTokenizer) bpeEncode(segment string, out *[]int32) {
	if segment == "" {
		return
	}
	normalized := t.normalize(segment)
	symbols := splitIntoRuneStrings(normalized)
	merged := t.applyBPE(symbols)
	for _, sym := range merged {
		if id, ok := t.vocab[sym]; ok {
			*out = append(*out, id)
		} else if t.unkID >= 0 {
			*out = append(*out, t.unkID)
		}
	}
}

// normalize applies the (single) Replace normalizer recorded at load time.
// EmbeddingGemma uses pattern=" " → content="▁"; callers without a normalizer
// configured get the input back unchanged.
func (t *GemmaTokenizer) normalize(s string) string {
	if t.normalizerPattern == "" {
		return s
	}
	return strings.ReplaceAll(s, t.normalizerPattern, t.normalizerContent)
}

// applyBPE runs the classical BPE algorithm: repeatedly merge the pair with
// the lowest merge rank until no merge applies. Operates on a slice of
// single-character symbol strings (one rune each, initially) which grow as
// merges are applied.
func (t *GemmaTokenizer) applyBPE(symbols []string) []string {
	if len(symbols) < 2 {
		return symbols
	}
	for {
		bestRank := math.MaxInt
		bestIdx := -1
		for i := 0; i < len(symbols)-1; i++ {
			if rank, ok := t.bpeRanks[bpePair{symbols[i], symbols[i+1]}]; ok && rank < bestRank {
				bestRank = rank
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			return symbols
		}

		// Merge every adjacent occurrence of the chosen pair in a single pass.
		// HF tokenizers' BPE merges all occurrences of the best pair before
		// rescanning — we mirror that to match its output exactly.
		merged := make([]string, 0, len(symbols))
		i := 0
		for i < len(symbols) {
			if i+1 < len(symbols) && symbols[i] == symbols[bestIdx] && symbols[i+1] == symbols[bestIdx+1] {
				merged = append(merged, symbols[i]+symbols[i+1])
				i += 2
			} else {
				merged = append(merged, symbols[i])
				i++
			}
		}
		symbols = merged
	}
}

// splitIntoRuneStrings turns a string into a slice where each element is the
// string-form of a single Unicode code point. Multi-byte runes stay intact.
func splitIntoRuneStrings(s string) []string {
	out := make([]string, 0, len(s))
	for _, r := range s {
		out = append(out, string(r))
	}
	return out
}

// EncodeWithBOSAndEOS returns Encode(text) with the BOS id prepended and the
// EOS id appended (when they were resolved from added_tokens). This matches
// what HuggingFace's encode() returns for EmbeddingGemma with default
// post-processing — including the [BOS, EOS] pair for empty input.
func (t *GemmaTokenizer) EncodeWithBOSAndEOS(text string) []int32 {
	tokens := t.Encode(text)
	out := make([]int32, 0, len(tokens)+2)
	if t.bosID >= 0 {
		out = append(out, t.bosID)
	}
	out = append(out, tokens...)
	if t.eosID >= 0 {
		out = append(out, t.eosID)
	}
	return out
}

// EncodeWithBOS prepends BOS but does not append EOS. Kept for callers that
// only want the prefix marker.
func (t *GemmaTokenizer) EncodeWithBOS(text string) []int32 {
	tokens := t.Encode(text)
	if t.bosID < 0 {
		return tokens
	}
	out := make([]int32, 0, len(tokens)+1)
	out = append(out, t.bosID)
	out = append(out, tokens...)
	return out
}

// EncodeBatch tokenizes multiple texts and right-pads each to the longest
// length with padID, returning a rectangular [][]int32. Used by the ONNX
// provider to feed batched input tensors.
func (t *GemmaTokenizer) EncodeBatch(texts []string, padID int32) [][]int32 {
	results := make([][]int32, len(texts))
	maxLen := 0
	for i, text := range texts {
		results[i] = t.Encode(text)
		if len(results[i]) > maxLen {
			maxLen = len(results[i])
		}
	}
	for i := range results {
		if len(results[i]) < maxLen {
			pad := make([]int32, maxLen-len(results[i]))
			for j := range pad {
				pad[j] = padID
			}
			results[i] = append(results[i], pad...)
		}
	}
	return results
}

// MaskBatch produces attention masks (1 = real token, 0 = padding) for a
// batch of encoded sequences. Length matches each sequence row in encoded.
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

// VocabSize returns the model vocabulary size (not counting added tokens that
// share ids with vocab entries, which is rare).
func (t *GemmaTokenizer) VocabSize() int { return t.vocabSize }

// TokenIDs is an alias for Encode, kept for callers that prefer the name.
func (t *GemmaTokenizer) TokenIDs(text string) []int32 { return t.Encode(text) }
