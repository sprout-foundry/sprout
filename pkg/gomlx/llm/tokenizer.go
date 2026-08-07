//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

// Tokenizer is a BPE tokenizer for Qwen3 models. It loads the HuggingFace
// tokenizer.json format (BPE merges + vocab).
type Tokenizer struct {
	vocab    map[string]int
	idToTok  map[int]string
	merges   []string
	encoder  *BPEDecoder
	bosID    int
	eosID    int
	padID    int
	spaceTok string
}

// BPEDecoder handles the BPE merge ranking
type BPEDecoder struct {
	ranks map[string]int
}

// LoadTokenizer loads a Qwen3 tokenizer from a HuggingFace tokenizer.json file.
func LoadTokenizer(path string) (*Tokenizer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tokenizer.json: %w", err)
	}

	var raw hfTokenizer
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse tokenizer.json: %w", err)
	}

	tok := &Tokenizer{
		vocab:   make(map[string]int),
		idToTok: make(map[int]string),
	}

	// Build vocab from model.vocab
	for word, id := range raw.Model.Vocab {
		tok.vocab[word] = id
		tok.idToTok[id] = word
	}

	// Add special tokens from added_tokens (not in model.vocab)
	for _, at := range raw.AddedTokens {
		if at.Special {
			tok.vocab[at.Content] = at.ID
			tok.idToTok[at.ID] = at.Content
		}
	}

	// Build merge ranks
	tok.encoder = &BPEDecoder{ranks: make(map[string]int)}
	for i, merge := range raw.Model.Merges {
		if len(merge) == 2 {
			tok.encoder.ranks[merge[0]+merge[1]] = i
		}
	}

	// Find special tokens
	for _, at := range raw.AddedTokens {
		switch at.Content {
		case "<|endoftext|>":
			tok.bosID = at.ID
		case "<|im_end|>":
			tok.eosID = at.ID
		}
	}

	// Fallbacks
	if tok.bosID == 0 {
		tok.bosID = -1 // no BOS
	}
	if tok.eosID == 0 {
		tok.eosID = 151645 // Qwen3 default
	}

	tok.spaceTok = "Ġ" // The BPE space marker used by GPT-2/Qwen tokenizers

	return tok, nil
}

// Encode converts a text string to a sequence of token IDs using BPE.
// Special tokens (e.g. <|im_start|>, <|im_end|>) are recognized as single units.
func (t *Tokenizer) Encode(text string) []int {
	if len(text) == 0 {
		return nil
	}

	// Build a set of special tokens for fast lookup
	specialTokens := map[string]int{}
	for id, tok := range t.idToTok {
		if strings.HasPrefix(tok, "<|") && strings.HasSuffix(tok, "|>") {
			specialTokens[tok] = id
		}
	}

	// Split text on special tokens, keeping them
	var tokenIDs []int
	remaining := text
	for len(remaining) > 0 {
		// Find the earliest special token
		bestIdx := -1
		bestTok := ""
		bestID := -1
		for tok, id := range specialTokens {
			idx := strings.Index(remaining, tok)
			if idx >= 0 && (bestIdx == -1 || idx < bestIdx) {
				bestIdx = idx
				bestTok = tok
				bestID = id
			}
		}

		if bestIdx == -1 {
			// No more special tokens — BPE encode the rest
			tokenIDs = append(tokenIDs, t.encodeBPE(remaining)...)
			break
		}

		// BPE encode text before the special token
		if bestIdx > 0 {
			tokenIDs = append(tokenIDs, t.encodeBPE(remaining[:bestIdx])...)
		}
		// Add the special token
		tokenIDs = append(tokenIDs, bestID)
		// Continue after the special token
		remaining = remaining[bestIdx+len(bestTok):]
	}

	return tokenIDs
}

// encodeBPE applies BPE to regular (non-special) text.
func (t *Tokenizer) encodeBPE(text string) []int {
	// Pre-tokenize: split into words on whitespace/punctuation, keeping delimiters
	words := preTokenize(text)

	var tokenIDs []int
	for _, word := range words {
		// Convert to BPE space-encoding: leading space becomes Ġ
		bpeWord := toBPESpace(word)
		tokens := t.bpe(bpeWord)
		for _, tokStr := range tokens {
			if id, ok := t.vocab[tokStr]; ok {
				tokenIDs = append(tokenIDs, id)
			}
		}
	}

	return tokenIDs
}

// Decode converts token IDs back to text.
func (t *Tokenizer) Decode(ids []int) string {
	var sb strings.Builder
	for _, id := range ids {
		tok, ok := t.idToTok[id]
		if !ok {
			continue
		}
		// Convert BPE space-encoding back to regular spaces
		decoded := fromBPESpace(tok)
		sb.WriteString(decoded)
	}
	return sb.String()
}

// bpe applies the BPE algorithm to a word, returning the subword tokens.
func (t *Tokenizer) bpe(word string) []string {
	if len(word) == 0 {
		return nil
	}

	// Quick check: if the whole word is in vocab, return it
	if _, ok := t.vocab[word]; ok {
		return []string{word}
	}

	// Split into characters
	symbols := []rune(word)
	if len(symbols) <= 1 {
		return []string{word}
	}

	// BPE merge loop
	pairs := getAllPairs(symbols)
	for len(symbols) > 1 {
		// Find the best pair (lowest rank)
		bestPair := ""
		bestRank := -1
		for _, pair := range pairs {
			rank, ok := t.encoder.ranks[string(pair)]
			if ok && (bestRank == -1 || rank < bestRank) {
				bestRank = rank
				bestPair = string(pair)
			}
		}

		if bestRank == -1 {
			break
		}

		// Merge all occurrences of bestPair
		newSymbols := []rune{}
		i := 0
		bestPairRunes := []rune(bestPair)
		for i < len(symbols) {
			if i < len(symbols)-1 && symbols[i] == bestPairRunes[0] && symbols[i+1] == bestPairRunes[1] {
				// Merge: keep first rune, the merged token is handled by
				// treating consecutive runes as a string
				newSymbols = append(newSymbols, 0) // placeholder for merged
				// Replace with a unique rune for the merged token
				// We need a different approach: work with strings
				break
			}
			newSymbols = append(newSymbols, symbols[i])
			i++
		}
		if i < len(symbols) {
			// Did the break above — fall back to string-based merge
			return t.bpeStringMerge(word)
		}
		symbols = newSymbols
		pairs = getAllPairs(symbols)
	}

	// Convert symbols back to strings
	result := make([]string, len(symbols))
	for i, s := range symbols {
		result[i] = string(s)
	}
	return result
}

// bpeStringMerge does the BPE merge using string symbols instead of runes.
// This handles multi-character merge tokens correctly.
func (t *Tokenizer) bpeStringMerge(word string) []string {
	symbols := []string{}
	for _, r := range word {
		symbols = append(symbols, string(r))
	}

	for len(symbols) > 1 {
		bestPair := ""
		bestRank := -1
		for i := 0; i < len(symbols)-1; i++ {
			pair := symbols[i] + symbols[i+1]
			rank, ok := t.encoder.ranks[pair]
			if ok && (bestRank == -1 || rank < bestRank) {
				bestRank = rank
				bestPair = pair
			}
		}

		if bestRank == -1 {
			break
		}

		newSymbols := []string{}
		i := 0
		for i < len(symbols) {
			if i < len(symbols)-1 && symbols[i]+symbols[i+1] == bestPair {
				newSymbols = append(newSymbols, bestPair)
				i += 2
			} else {
				newSymbols = append(newSymbols, symbols[i])
				i++
			}
		}
		symbols = newSymbols
	}

	return symbols
}

// getAllPairs returns all adjacent symbol pairs.
func getAllPairs(symbols []rune) []string {
	pairs := make([]string, 0, len(symbols)-1)
	for i := 0; i < len(symbols)-1; i++ {
		pairs = append(pairs, string(symbols[i])+string(symbols[i+1]))
	}
	return pairs
}

// preTokenize splits text into words, preserving whitespace.
func preTokenize(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			// Attach space to next word
			words = append(words, " ")
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// toBPESpace converts a pre-tokenized word to BPE space encoding.
// In GPT-2/Qwen BPE, a leading space is encoded as Ġ (U+0120).
func toBPESpace(word string) string {
	if word == " " {
		return "Ġ"
	}
	// Replace leading space with Ġ
	if strings.HasPrefix(word, " ") {
		return "Ġ" + word[1:]
	}
	return word
}

// fromBPESpace converts BPE space-encoding back to regular text.
func fromBPESpace(token string) string {
	s := strings.ReplaceAll(token, "Ġ", " ")
	s = strings.ReplaceAll(s, "Ċ", "\n")
	s = strings.ReplaceAll(s, "đ", "\t")
	return s
}

// HuggingFace tokenizer.json structures
type hfTokenizer struct {
	Model      hfModel      `json:"model"`
	AddedTokens []hfAddedToken `json:"added_tokens"`
}

type hfModel struct {
	Type   string            `json:"type"`
	Vocab  map[string]int    `json:"vocab"`
	Merges [][]string        `json:"merges"`
}

type hfAddedToken struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
	Special bool   `json:"special"`
}

// FormatChat applies the Qwen3 chat template to messages.
func (t *Tokenizer) FormatChat(messages []ChatMessage) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString("<|im_start|>")
		sb.WriteString(msg.Role)
		sb.WriteString("\n")
		sb.WriteString(msg.Content)
		sb.WriteString("<|im_end|>\n")
	}
	sb.WriteString("<|im_start|>assistant\n")
	return sb.String()
}

// ChatMessage is a single message in a chat conversation.
type ChatMessage struct {
	Role    string // "system", "user", or "assistant"
	Content string
}

// VocabSize returns the vocabulary size.
func (t *Tokenizer) VocabSize() int {
	return len(t.vocab)
}

// SortedTokens returns tokens sorted by ID (for debugging).
func (t *Tokenizer) SortedTokens() []string {
	ids := make([]int, 0, len(t.idToTok))
	for id := range t.idToTok {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	tokens := make([]string, len(ids))
	for i, id := range ids {
		tokens[i] = t.idToTok[id]
	}
	return tokens
}
