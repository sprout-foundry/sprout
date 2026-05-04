package embedding

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tokenizer implements BERT-style WordPiece tokenization for MiniLM.
type Tokenizer struct {
	vocab      map[string]int
	unkID      int
	clsID      int
	sepID      int
	padID      int
	maxLength  int
	maxWordLen int
}

// tokenizerJSON is the top-level structure of tokenizer.json.
type tokenizerJSON struct {
	Model struct {
		Vocab         map[string]int `json:"vocab"`
		UnkToken      string         `json:"unk_token"`
		MaxInputChars int            `json:"max_input_chars_per_word"`
	} `json:"model"`
}

// NewTokenizerJSON parses tokenizer JSON bytes and returns a Tokenizer.
func NewTokenizerJSON(data []byte, maxLength int) (*Tokenizer, error) {
	var t tokenizerJSON
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parse tokenizer json: %w", err)
	}
	if len(t.Model.Vocab) == 0 {
		return nil, fmt.Errorf("empty vocab in tokenizer.json")
	}
	if t.Model.MaxInputChars == 0 {
		t.Model.MaxInputChars = 100
	}
	return &Tokenizer{
		vocab:      t.Model.Vocab,
		unkID:      t.Model.Vocab[t.Model.UnkToken],
		clsID:      t.Model.Vocab["[CLS]"],
		sepID:      t.Model.Vocab["[SEP]"],
		padID:      t.Model.Vocab["[PAD]"],
		maxLength:  maxLength,
		maxWordLen: t.Model.MaxInputChars,
	}, nil
}

// Tokenize converts text to (inputIDs, attentionMask) of length maxLength.
func (t *Tokenizer) Tokenize(text string) ([]int64, []int64) {
	// 1. Normalize: lowercase
	text = strings.ToLower(text)

	// 2. Pre-tokenize: split on whitespace and punctuation
	words := bertPreTokenizer(text)

	// 3. WordPiece: subword tokenization
	tokens := make([]string, 0, t.maxLength-2)
	for _, w := range words {
		tokens = append(tokens, t.wordPiece(w)...)
	}

	// 4. Truncate to make room for [CLS] and [SEP]
	maxTokens := t.maxLength - 2
	if len(tokens) > maxTokens {
		tokens = tokens[:maxTokens]
	}

	// 5. Build output with [CLS] ... [SEP] then pad
	ids := make([]int64, t.maxLength)
	mask := make([]int64, t.maxLength)

	// [CLS]
	ids[0] = int64(t.clsID)
	mask[0] = 1
	pos := 1

	// tokens
	for _, tok := range tokens {
		if pos >= t.maxLength-1 {
			break
		}
		ids[pos] = int64(t.tokenToID(tok))
		mask[pos] = 1
		pos++
	}

	// [SEP]
	if pos < t.maxLength {
		ids[pos] = int64(t.sepID)
		mask[pos] = 1
		pos++
	}

	// Pad remaining
	for ; pos < t.maxLength; pos++ {
		ids[pos] = int64(t.padID)
		mask[pos] = 0
	}

	return ids, mask
}

// tokenToID returns the vocab ID for a token string, or UNK if not found.
func (t *Tokenizer) tokenToID(tok string) int {
	if id, ok := t.vocab[tok]; ok {
		return id
	}
	return t.unkID
}

// wordPiece applies WordPiece subword tokenization to a single word.
func (t *Tokenizer) wordPiece(word string) []string {
	if len(word) > t.maxWordLen {
		return []string{t.idToString(t.unkID)}
	}

	var subTokens []string
	wordsLeft := word

	for len(wordsLeft) > 0 {
		bestToken := ""
		bestLen := 0

		// Greedy: try longest prefix first
		for i := len(wordsLeft); i >= 2; i-- {
			candidate := wordsLeft[:i]
			if len(subTokens) > 0 {
				candidate = "##" + candidate
			}
			if _, ok := t.vocab[candidate]; ok {
				bestToken = candidate
				bestLen = i
				break // longest prefix found
			}
		}

		if bestToken == "" {
			// No subword found → emit [UNK] and stop
			subTokens = append(subTokens, t.idToString(t.unkID))
			break
		}

		subTokens = append(subTokens, bestToken)
		wordsLeft = wordsLeft[bestLen:]
	}

	return subTokens
}

// idToString returns the token string for a given ID (reverse lookup).
func (t *Tokenizer) idToString(id int) string {
	for tok, idx := range t.vocab {
		if idx == id {
			return tok
		}
	}
	// fallback
	return "[UNK]"
}

// bertPreTokenizer mimics BERT's pre-tokenizer: splits on whitespace and punctuation.
func bertPreTokenizer(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if isWhitespace(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		} else {
			if isPunctuation(r) && current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// isWhitespace returns true for common whitespace characters.
func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// isPunctuation returns true for punctuation characters that trigger a split in BERT.
func isPunctuation(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '(', ')', '[', ']', '{', '}', '"', '\'', '?', '!', '-', '/', '\\', '|',
		'<', '>', '@', '#', '$', '%', '^', '&', '*', '+', '=', '~', '`',
		'\u2018', '\u2019', '\u201c', '\u201d':
		return true
	default:
		return false
	}
}

// TokenizeBatch converts multiple texts to flat inputIDs and attentionMask buffers.
// Returned slices are of length batchSize * maxLength, suitable for tensor input.
func (t *Tokenizer) TokenizeBatch(texts []string) ([]int64, []int64, int) {
	n := len(texts)
	total := n * t.maxLength
	inputIDs := make([]int64, total)
	mask := make([]int64, total)

	for i, text := range texts {
		ids, m := t.Tokenize(text)
		base := i * t.maxLength
		copy(inputIDs[base:], ids)
		copy(mask[base:], m)
	}
	return inputIDs, mask, n
}
