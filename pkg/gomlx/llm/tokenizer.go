//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

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
	// specialTokens maps every added token (atomic, not BPE-decomposed) to its
	// ID. HuggingFace registers all added_tokens as single units regardless of
	// the Special flag — <think>/</think> are added but not Special, and must
	// still encode atomically or the model never sees the thinking boundary.
	specialTokens map[string]int
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

	// Register added tokens as atomic units. HF treats every added_tokens
	// entry as a single token during encoding regardless of Special — Qwen3.5
	// ships <think>/</think> with Special=false and they must still encode
	// atomically or the thinking boundary is invisible to the model.
	tok.specialTokens = make(map[string]int, len(raw.AddedTokens))
	for _, at := range raw.AddedTokens {
		tok.vocab[at.Content] = at.ID
		tok.idToTok[at.ID] = at.Content
		tok.specialTokens[at.Content] = at.ID
	}

	// Build merge ranks. Merges are stored as flat "left right" strings
	// (modern HF) or joined pairs (legacy, converted in UnmarshalJSON).
	tok.encoder = &BPEDecoder{ranks: make(map[string]int)}
	for i, merge := range raw.Model.Merges {
		// A rank key is the concatenation of the two merge pieces with no
		// separator. The flat form joins them with a single space; the
		// legacy UnmarshalJSON already joined pairs with no space.
		joined := merge
		if parts := strings.SplitN(merge, " ", 2); len(parts) == 2 {
			joined = parts[0] + parts[1]
		}
		tok.encoder.ranks[joined] = i
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

	// Split text on special tokens, keeping them
	var tokenIDs []int
	remaining := text
	for len(remaining) > 0 {
		// Find the earliest special token
		bestIdx := -1
		bestTok := ""
		bestID := -1
		for tok, id := range t.specialTokens {
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
	// Gemma/SentencePiece: \n is a standalone vocab token. Check and emit directly.
	if _, hasNewline := t.vocab["\n"]; hasNewline {
		return t.encodeGemma(text)
	}
	// Standard BPE (Qwen/GPT-2 style)
	words := preTokenize(text)
	var tokenIDs []int
	for _, word := range words {
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

// encodeGemma handles Gemma/SentencePiece-style tokenization where spaces
// are replaced with ▁ (U+2581) and \n is a standalone token.
func (t *Tokenizer) encodeGemma(text string) []int {
	var tokenIDs []int
	for _, r := range text {
		var bpeWord string
		if r == '\n' {
			bpeWord = "\n"
		} else if r == ' ' {
			bpeWord = "▁"
		} else {
			bpeWord = string(r)
		}
		// Try direct vocab lookup first
		if id, ok := t.vocab[bpeWord]; ok {
			tokenIDs = append(tokenIDs, id)
			continue
		}
		// BPE merge
		tokens := t.bpe(bpeWord)
		for _, tokStr := range tokens {
			if id, ok := t.vocab[tokStr]; ok {
				tokenIDs = append(tokenIDs, id)
			}
		}
	}
	// Post-process: merge consecutive BPE tokens that form multi-char vocab entries
	// This handles cases like ▁i▁s needing to be one token
	return t.mergeGemmaTokens(tokenIDs)
}

// mergeGemmaTokens applies a second pass of BPE merging on the tokenized
// output for Gemma's SentencePiece-style merges.
func (t *Tokenizer) mergeGemmaTokens(ids []int) []int {
	if len(ids) < 2 {
		return ids
	}
	// Convert IDs back to strings, then run BPE
	var tokens []string
	for _, id := range ids {
		if s, ok := t.idToTok[id]; ok {
			tokens = append(tokens, s)
		}
	}
	// Re-merge the full sequence
	merged := t.bpe(strings.Join(tokens, ""))
	var result []int
	for _, tokStr := range merged {
		if id, ok := t.vocab[tokStr]; ok {
			result = append(result, id)
		}
	}
	if len(result) == 0 {
		return ids // fallback
	}
	return result
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

// preTokenize splits text into words, attaching each whitespace run to the
// following word (GPT-2/Qwen BPE convention: "The capital" → ["The", " capital"]).
// A bare " " must never be emitted as its own word — toBPESpace would encode
// it as the standalone Ġ token (id 220) instead of the merged "Ġcapital".
func preTokenize(text string) []string {
	var words []string
	var current strings.Builder
	spacePending := false

	for _, r := range text {
		if unicode.IsSpace(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			spacePending = true // attach to next word
		} else {
			if spacePending {
				current.WriteString(" ")
				spacePending = false
			}
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
// Handles both SentencePiece-style (▁) and GPT-2 byte-level encoding.
func fromBPESpace(token string) string {
	s := strings.ReplaceAll(token, "▁", " ") // Gemma SentencePiece space
	// GPT-2/Qwen byte-level: bytes < 33 or > 126 are mapped to chars >= U+0100.
	return decodeByteLevel(s)
}

// decodeByteLevel reverses the GPT-2 byte-level pre-tokenizer mapping.
// Bytes 0–255 map to unicode chars: printable ASCII (33–126) map to
// themselves; everything else maps to chr(byte + 256).
func decodeByteLevel(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		if r < 128 {
			sb.WriteRune(r)
			continue
		}
		// Reverse the GPT-2 bytes_to_unicode mapping
		b := byteLevelRuneToByte(r)
		if b >= 0 {
			sb.WriteByte(byte(b))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// byteLevelRuneToByte reverses the GPT-2 bytes_to_unicode mapping for a
// single rune. Returns -1 if the rune is not part of the mapping (e.g.
// genuine multilingual text).
func byteLevelRuneToByte(r rune) int {
	switch {
	case r >= 33 && r <= 126:
		return int(r)
	case r >= 256 && r <= 256+255:
		v := int(r - 256)
		if v < 33 || v > 126 {
			return v
		}
	}
	return -1
}

// HuggingFace tokenizer.json structures
type hfTokenizer struct {
	Model       hfModel        `json:"model"`
	AddedTokens []hfAddedToken `json:"added_tokens"`
}

type hfModel struct {
	Type   string         `json:"type"`
	Vocab  map[string]int `json:"vocab"`
	Merges []string       `json:"merges"`
}

// UnmarshalJSON accepts both the legacy tokenizer format (merges as
// [][]string, each pair split later) and the modern flat format (merges as
// []string like "Ġ Ġ"). Raw-HF Qwen3.5 exports ship the flat form.
func (m *hfModel) UnmarshalJSON(data []byte) error {
	var flat struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges []string       `json:"merges"`
	}
	if err := json.Unmarshal(data, &flat); err == nil && len(flat.Merges) > 0 {
		m.Type = flat.Type
		m.Vocab = flat.Vocab
		m.Merges = flat.Merges
		return nil
	}
	var legacy struct {
		Type   string         `json:"type"`
		Vocab  map[string]int `json:"vocab"`
		Merges [][]string     `json:"merges"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	m.Type = legacy.Type
	m.Vocab = legacy.Vocab
	for _, pair := range legacy.Merges {
		if len(pair) == 2 {
			m.Merges = append(m.Merges, pair[0]+pair[1])
		}
	}
	return nil
}

type hfAddedToken struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
	Special bool   `json:"special"`
}

// FormatChat applies the Qwen3 chat template to messages.
//
// Qwen3.5's reference template closes the think block when thinking is
// disabled: <think>\n\n</think>\n\n. That tells the model to answer directly
// instead of burning context on a reasoning essay — the right default for
// small local models (keeps them cogent and within the context budget). If
// a model emits a thinking block anyway, the generation loop still strips it
// via shouldFilterToken. Models without thinking tokens (plain qwen3) ignore
// the marker harmlessly.
func (t *Tokenizer) FormatChat(messages []ChatMessage) string {
	// LFM2 uses its own chat format with BOS and <think> markers
	if _, isLFM2 := t.vocab["<|im_start|>"]; isLFM2 && t.bosID > 0 && t.specialTokens["<|startoftext|>"] > 0 {
		return t.formatLFM2Chat(messages)
	}
	// Gemma uses a different chat format than Qwen
	if _, isGemma := t.vocab["<|turn>"]; isGemma {
		return t.formatGemmaChat(messages)
	}
	return t.formatQwenChat(messages)
}

// FormatChatPrefix renders messages the same way FormatChat does, but
// without the trailing "generate now" cue. Because every architecture's
// chat template appends one message at a time, the result is guaranteed to
// be an exact prefix of FormatChat(longerMessages) for any longerMessages
// sharing this leading sequence. Used to pre-warm a KV cache slot for
// content (system prompt + tool definitions) shared by many otherwise-
// unrelated conversations — see Model.WarmSystemPrefix.
func (t *Tokenizer) FormatChatPrefix(messages []ChatMessage) string {
	if _, isLFM2 := t.vocab["<|im_start|>"]; isLFM2 && t.bosID > 0 && t.specialTokens["<|startoftext|>"] > 0 {
		return "<|startoftext|>" + t.formatLFM2Body(messages)
	}
	if _, isGemma := t.vocab["<|turn>"]; isGemma {
		return t.formatGemmaBody(messages)
	}
	return t.formatQwenBody(messages)
}

func (t *Tokenizer) formatLFM2Chat(messages []ChatMessage) string {
	return "<|startoftext|>" + t.formatLFM2Body(messages) + "<|im_start|>assistant\n<think>\n\n</think>\n\n"
}

func (t *Tokenizer) formatLFM2Body(messages []ChatMessage) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString("<|im_start|>")
		sb.WriteString(msg.Role)
		sb.WriteString("\n")
		if msg.Role == "assistant" {
			sb.WriteString("<think>\n\n</think>\n\n")
		}
		sb.WriteString(msg.Content)
		sb.WriteString("<|im_end|>\n")
	}
	return sb.String()
}

func (t *Tokenizer) formatGemmaChat(messages []ChatMessage) string {
	return t.formatGemmaBody(messages) + "<|turn>model\n"
}

func (t *Tokenizer) formatGemmaBody(messages []ChatMessage) string {
	var sb strings.Builder
	for _, msg := range messages {
		role := msg.Role
		if role == "user" {
			sb.WriteString("<|turn>user\n")
		} else {
			sb.WriteString("<|turn>model\n")
		}
		sb.WriteString(msg.Content)
		sb.WriteString("<turn|>\n")
	}
	return sb.String()
}

func (t *Tokenizer) formatQwenChat(messages []ChatMessage) string {
	return t.formatQwenBody(messages) + "<|im_start|>assistant\n<think>\n\n</think>\n\n"
}

func (t *Tokenizer) formatQwenBody(messages []ChatMessage) string {
	var sb strings.Builder
	for _, msg := range messages {
		sb.WriteString("<|im_start|>")
		sb.WriteString(msg.Role)
		sb.WriteString("\n")
		if msg.Role == "assistant" {
			sb.WriteString("<think>\n\n</think>\n\n")
		}
		sb.WriteString(msg.Content)
		sb.WriteString("<|im_end|>\n")
	}
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

// IDOf returns the token ID for an atomic (added) token, or 0 if the
// tokenizer has no such token. Used to resolve <think>/</think>.
func (t *Tokenizer) IDOf(content string) int {
	if id, ok := t.specialTokens[content]; ok {
		return id
	}
	return 0
}

// EOSID returns the end-of-sequence token ID the tokenizer detected
// (<|im_end|> for Qwen chat models, falling back to the Qwen3 default).
// This is distinct from config.json's eos_token_id (which on multimodal
// Qwen3.5 wrappers points at <|endoftext|>, not the chat terminator the
// model actually emits).
func (t *Tokenizer) EOSID() int { return t.eosID }

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
