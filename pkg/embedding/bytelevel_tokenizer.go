package embedding

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode"
	"unicode/utf8"
)

// ByteLevelTokenizer implements HuggingFace ByteLevel pre-tokenizer + BPE
// encoding for GPT-2/Jina/RoBERTa-style tokenizers. Separate from
// GemmaTokenizer because pre-tokenization and vocab format differ.

// byteEncoder maps raw byte values (0–255) to the GPT-2 Unicode code points
// used in ByteLevel tokenizers. Initialized from buildByteEncoder().
var byteEncoder [256]rune

// byteDecoder is the reverse mapping for debugging/validation.
var byteDecoder map[rune]byte

func init() {
	buildByteEncoder()
}

// buildByteEncoder fills byteEncoder/byteDecoder with the GPT-2 byte-level mapping.
func buildByteEncoder() {
	// Printable byte ranges that map to themselves.
	printable := make(map[int]bool)
	for b := 33; b <= 126; b++ {
		printable[b] = true
	}
	for b := 161; b <= 172; b++ {
		printable[b] = true
	}
	for b := 174; b <= 255; b++ {
		printable[b] = true
	}

	n := 0
	for b := 0; b < 256; b++ {
		if printable[b] {
			byteEncoder[b] = rune(b)
		} else {
			byteEncoder[b] = rune(256 + n)
			n++
		}
	}

	byteDecoder = make(map[rune]byte, 256)
	for b, r := range byteEncoder {
		byteDecoder[r] = byte(b)
	}
}

// byteLevelPreTokenizer splits text into word segments using the GPT-2 regex,
// then maps each segment's bytes through byteEncoder. The 'Ġ' character
// (U+0120) represents a leading space in the mapped space.
type byteLevelPreTokenizer struct{}

func (byteLevelPreTokenizer) preTokenize(text string) []string {
	// GPT-2 pre-tokenization regex: contractions, letters, digits, whitespace, single chars.
	segments := gpt2RegexSplit(text)
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		out = append(out, byteEncodeString(seg))
	}
	return out
}

// byteEncodeString maps each UTF-8 byte of s through byteEncoder, producing
// the GPT-2 byte-level representation (spaces become 'Ġ', etc.).
func byteEncodeString(s string) string {
	var buf []rune
	for i := 0; i < len(s); i++ {
		buf = append(buf, byteEncoder[s[i]])
	}
	return string(buf)
}

// gpt2RegexSplit implements the GPT-2 pre-tokenization regex:
//
//	's|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+
//
// The `\s+(?!\S)` alternative matches a whitespace run NOT followed by a
// non-whitespace char (i.e., trailing whitespace before end-of-input). This
// matters for multi-char whitespace sequences in code (\n\t\t\n etc.).
func gpt2RegexSplit(s string) []string {
	var result []string
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		// Try contraction: 's 't 're 've 'm 'll 'd
		if runes[i] == '\'' && i+1 < len(runes) {
			matched := false
			for _, suf := range []string{"s", "t", "re", "ve", "m", "ll", "d"} {
				sufRunes := []rune(suf)
				if i+1+len(sufRunes) > len(runes) {
					continue
				}
				match := true
				for j, sr := range sufRunes {
					if runes[i+1+j] != sr {
						match = false
						break
					}
				}
				if match {
					result = append(result, string(runes[i:i+1+len(sufRunes)]))
					i += 1 + len(sufRunes)
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}

		r := runes[i]

		// Non-space whitespace: split runs per HF ByteLevel regex (\s+(?!\S) peels the last char).
		if unicode.IsSpace(r) && r != ' ' {
			start := i
			i++
			for i < len(runes) && unicode.IsSpace(runes[i]) && runes[i] != ' ' {
				i++
			}
			// If followed by non-whitespace (or end), peel off the last char.
			if i > start+1 && (i >= len(runes) || !unicode.IsSpace(runes[i])) {
				result = append(result, string(runes[start:i-1]))
				result = append(result, string(runes[i-1:i]))
			} else {
				result = append(result, string(runes[start:i]))
			}
			continue
		}

		// Standalone space run: keep as one segment unless followed by non-space whitespace.
		if r == ' ' && (i+1 >= len(runes) || runes[i+1] == ' ') {
			start := i
			i++
			for i < len(runes) && runes[i] == ' ' {
				i++
			}
			// Peel off last char only when followed by non-space whitespace
			// (e.g., "  \n"). At end-of-input, keep as one segment.
			if i > start+1 && i < len(runes) && unicode.IsSpace(runes[i]) && runes[i] != ' ' {
				result = append(result, string(runes[start:i-1]))
				result = append(result, string(runes[i-1:i]))
			} else {
				result = append(result, string(runes[start:i]))
			}
			continue
		}

		// Optional leading space before non-whitespace token.
		spaceStart := i
		if r == ' ' && i+1 < len(runes) {
			next := runes[i+1]
			if !unicode.IsSpace(next) {
				i++
				r = runes[i]
			}
		}

		isLetter := unicode.IsLetter(r)
		isDigit := unicode.IsNumber(r) || unicode.IsDigit(r)

		switch {
		case isLetter:
			i++
			for i < len(runes) && unicode.IsLetter(runes[i]) {
				i++
			}
			result = append(result, string(runes[spaceStart:i]))
		case isDigit:
			i++
			for i < len(runes) && (unicode.IsNumber(runes[i]) || unicode.IsDigit(runes[i])) {
				i++
			}
			result = append(result, string(runes[spaceStart:i]))
		default:
			i++
			for i < len(runes) && !unicode.IsLetter(runes[i]) && !unicode.IsNumber(runes[i]) && !unicode.IsDigit(runes[i]) && !unicode.IsSpace(runes[i]) {
				i++
			}
			result = append(result, string(runes[spaceStart:i]))
		}
	}
	return result
}

// ByteLevelTokenizer is a GPT-2/Jina/RoBERTa BPE tokenizer with byte-level pre-tokenization.
type ByteLevelTokenizer struct {
	vocab    map[string]int32
	bpeRanks map[bpePair]int

	addedByContent map[string]int32
	addedLengths   []int

	bosID int32
	eosID int32
	unkID int32
	padID int32

	vocabSize int

	preTok byteLevelPreTokenizer
}

// NewByteLevelTokenizer parses a HuggingFace tokenizer.json file.
func NewByteLevelTokenizer(path string) (*ByteLevelTokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("bytelevel tokenizer: read file: %w", err)
	}

	var tj tokenizerJSON
	if err := json.Unmarshal(data, &tj); err != nil {
		return nil, fmt.Errorf("bytelevel tokenizer: parse json: %w", err)
	}
	if tj.Model.Type != "BPE" {
		return nil, fmt.Errorf("bytelevel tokenizer: expected BPE model, got %q", tj.Model.Type)
	}

	merges, err := parseMerges(tj.Model.Merges)
	if err != nil {
		return nil, fmt.Errorf("bytelevel tokenizer: parse merges: %w", err)
	}

	t := &ByteLevelTokenizer{
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

	// Build added-token lookup and resolve special tokens.
	seenLen := make(map[int]struct{})
	for _, at := range tj.AddedTokens {
		if at.Content == "" {
			continue
		}
		t.addedByContent[at.Content] = at.ID
		seenLen[len(at.Content)] = struct{}{}
		switch at.Content {
		case "<s>", "<bos>":
			t.bosID = at.ID
		case "</s>", "<eos>":
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
	sortIntsDesc(t.addedLengths)

	return t, nil
}

// Encode converts text into a sequence of BPE token IDs using byte-level
// pre-tokenization. No special tokens are added.
func (t *ByteLevelTokenizer) Encode(text string) []int32 {
	if text == "" {
		return nil
	}
	var ids []int32
	for _, word := range t.preTok.preTokenize(text) {
		// Exact match on added tokens.
		if id, ok := t.vocab[word]; ok {
			ids = append(ids, id)
			continue
		}
		t.bpeEncodeWord(word, &ids)
	}
	return ids
}

// bpeEncodeWord runs the BPE merge loop on a single pre-tokenized word
// (already byte-level encoded) and appends the resulting token IDs.
func (t *ByteLevelTokenizer) bpeEncodeWord(word string, out *[]int32) {
	if word == "" {
		return
	}
	// Split the byte-encoded word into individual rune-strings for BPE.
	symbols := splitIntoRuneStrings(word)
	merged := applyBPEMerge(symbols, t.bpeRanks)
	for _, sym := range merged {
		if id, ok := t.vocab[sym]; ok {
			*out = append(*out, id)
		} else if t.unkID >= 0 {
			*out = append(*out, t.unkID)
		}
	}
}

// applyBPEMerge runs the BPE merge loop on a symbol slice using the given rank
// table. Shared between ByteLevelTokenizer and GemmaTokenizer to guarantee
// identical merge semantics.
func applyBPEMerge(symbols []string, bpeRanks map[bpePair]int) []string {
	if len(symbols) < 2 {
		return symbols
	}
	for {
		bestRank := maxInt
		bestIdx := -1
		for i := 0; i < len(symbols)-1; i++ {
			if rank, ok := bpeRanks[bpePair{symbols[i], symbols[i+1]}]; ok && rank < bestRank {
				bestRank = rank
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			return symbols
		}
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

// sortIntsDesc sorts a slice of ints in descending order in-place.
func sortIntsDesc(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] < s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// EncodeWithBOSAndEOS wraps Encode with the BOS/EOS special tokens.
func (t *ByteLevelTokenizer) EncodeWithBOSAndEOS(text string) []int32 {
	ids := t.Encode(text)
	if t.bosID >= 0 {
		ids = append([]int32{t.bosID}, ids...)
	}
	if t.eosID >= 0 {
		ids = append(ids, t.eosID)
	}
	return ids
}

const maxInt = int(^uint(0) >> 1)

// VocabSize returns the number of entries in the vocabulary.
func (t *ByteLevelTokenizer) VocabSize() int { return t.vocabSize }

// BOSID returns the beginning-of-sequence token ID, or -1 if unset.
func (t *ByteLevelTokenizer) BOSID() int32 { return t.bosID }

// EOSID returns the end-of-sequence token ID, or -1 if unset.
func (t *ByteLevelTokenizer) EOSID() int32 { return t.eosID }

// byteEncoderValid is a compile-time sanity check that ensures the byte
// encoder table has no zero entries (which would indicate an init bug).
//
//nolint:unused
func byteEncoderValid() bool {
	for _, r := range byteEncoder {
		if r == 0 {
			return false
		}
	}
	return true
}

// splitIntoRuneStrings turns a string into a slice where each element is the
// string-form of a single Unicode code point. Shared with GemmaTokenizer.
//
//nolint:unused // used by bpeEncodeWord
var _ = splitIntoRuneStrings

// utf8Valid is a compile-time guard to keep the utf8 import alive even if all
// explicit references are in test files.
//
//nolint:unused
var _ = utf8.Valid
