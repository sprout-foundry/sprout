//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"unicode"
)

// This file implements the Qwen/GPT-2 pre-tokenization pipeline that
// HuggingFace tokenizers apply before BPE:
//
//  1. A regex Split (Qwen variant — see qwenSplit) that isolates
//     contractions, words (with at most one leading space), single digits,
//     punctuation runs (with trailing newlines), newline runs (with leading
//     spaces), and whitespace.
//  2. A byte-level mapping (GPT-2 bytes_to_unicode) so every UTF-8 byte of
//     each pre-token maps to a printable rune: ' ' -> 'Ġ', '\n' -> 'Ċ',
//     '\t' -> 'ĉ'.
//
// The previous whitespace-attaching splitter merged newline runs into the
// following word ("system\nYou" -> "system" + "ĠYou") and dropped trailing
// newlines entirely. HF emits "system" + "Ċ" + "You": newline runs are
// standalone pre-tokens. Dense Qwen models tolerate the corruption; a 2-bit
// MoE does not — token IDs diverge and output collapses to noise.

// qwenByteEncoder maps raw byte values (0–255) to the GPT-2 Unicode code
// points used in byte-level BPE vocabularies.
var qwenByteEncoder [256]rune

// qwenByteDecoder reverses qwenByteEncoder.
var qwenByteDecoder map[rune]byte

func init() {
	printable := make(map[int]bool, 188)
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
			qwenByteEncoder[b] = rune(b)
		} else {
			qwenByteEncoder[b] = rune(256 + n)
			n++
		}
	}
	qwenByteDecoder = make(map[rune]byte, 256)
	for b, r := range qwenByteEncoder {
		qwenByteDecoder[r] = byte(b)
	}
}

// qwenPreTokenize splits text into byte-level-encoded pre-tokens following
// the Qwen tokenizer.json Split regex:
//
//	(?i:'s|'t|'re|'ve|'m|'ll|'d)|[^\r\n\p{L}\p{N}]?\p{L}+|\p{N}|
//	 ?[^\s\p{L}\p{N}]+[\r\n]*|\s*[\r\n]+|\s+(?!\S)|\s+
//
// Alternatives are tried in order at each position (leftmost-first). Each
// returned segment is already mapped through qwenByteEncoder.
func qwenPreTokenize(text string) []string {
	segs := qwenSplit(text)
	out := make([]string, len(segs))
	for i, seg := range segs {
		out[i] = qwenByteEncode(seg)
	}
	return out
}

// qwenByteEncode maps every UTF-8 byte of s through qwenByteEncoder.
func qwenByteEncode(s string) string {
	runes := make([]rune, 0, len(s))
	for i := 0; i < len(s); i++ {
		runes = append(runes, qwenByteEncoder[s[i]])
	}
	return string(runes)
}

// isQwenNL, isQwenWS mirror the regex classes [\r\n], \s.
func isQwenNL(r rune) bool { return r == '\r' || r == '\n' }

func isQwenWS(r rune) bool { return unicode.IsSpace(r) }

// qwenSplit implements the Qwen pre-tokenizer regex over runes. Each
// alternative is a try* function returning the matched segment and the next
// position; the first that matches wins (regex alternation order).
func qwenSplit(s string) []string {
	r := []rune(s)
	var out []string
	i := 0
	for i < len(r) {
		if seg, ni, ok := tryContraction(r, i); ok {
			out = append(out, seg)
			i = ni
			continue
		}
		if seg, ni, ok := tryLetters(r, i); ok {
			out = append(out, seg)
			i = ni
			continue
		}
		if seg, ni, ok := tryDigit(r, i); ok {
			out = append(out, seg)
			i = ni
			continue
		}
		if seg, ni, ok := tryPunct(r, i); ok {
			out = append(out, seg)
			i = ni
			continue
		}
		if seg, ni, ok := tryNewlineRun(r, i); ok {
			out = append(out, seg)
			i = ni
			continue
		}
		if seg, ni, ok := tryWhitespace(r, i); ok {
			out = append(out, seg)
			i = ni
			continue
		}
		// No alternative matched: emit one rune (regex \s+ / fallback —
		// shouldn't happen since every rune class is covered, but guarantees
		// forward progress).
		out = append(out, string(r[i]))
		i++
	}
	return out
}

// tryContraction matches (?i:'s|'t|'re|'ve|'m|'ll|'d).
func tryContraction(r []rune, i int) (string, int, bool) {
	if r[i] != '\'' || i+1 >= len(r) {
		return "", i, false
	}
	for _, suf := range []string{"s", "t", "re", "ve", "m", "ll", "d"} {
		sr := []rune(suf)
		if i+1+len(sr) > len(r) {
			continue
		}
		ok := true
		for j, c := range sr {
			if unicode.ToLower(r[i+1+j]) != c {
				ok = false
				break
			}
		}
		if ok {
			return string(r[i : i+1+len(sr)]), i + 1 + len(sr), true
		}
	}
	return "", i, false
}

// tryLetters matches [^\r\n\p{L}\p{N}]?\p{L}+ — an optional single
// non-letter/non-digit/non-newline char (usually ' ') followed by letters.
func tryLetters(r []rune, i int) (string, int, bool) {
	j := i
	if j < len(r) && !isQwenNL(r[j]) && !unicode.IsLetter(r[j]) && !unicode.IsNumber(r[j]) {
		j++
	}
	if j >= len(r) || !unicode.IsLetter(r[j]) {
		return "", i, false
	}
	k := j
	for k < len(r) && unicode.IsLetter(r[k]) {
		k++
	}
	return string(r[i:k]), k, true
}

// tryDigit matches \p{N} — exactly one digit.
func tryDigit(r []rune, i int) (string, int, bool) {
	if unicode.IsNumber(r[i]) {
		return string(r[i]), i + 1, true
	}
	return "", i, false
}

// tryPunct matches ` ?[^\s\p{L}\p{N}]+[\r\n]*` — an optional literal space,
// a run of punctuation/symbols, and any newlines immediately after.
func tryPunct(r []rune, i int) (string, int, bool) {
	j := i
	if r[j] == ' ' {
		j++
	}
	if j >= len(r) || isQwenWS(r[j]) || unicode.IsLetter(r[j]) || unicode.IsNumber(r[j]) {
		return "", i, false
	}
	k := j
	for k < len(r) && !isQwenWS(r[k]) && !unicode.IsLetter(r[k]) && !unicode.IsNumber(r[k]) {
		k++
	}
	for k < len(r) && isQwenNL(r[k]) {
		k++
	}
	return string(r[i:k]), k, true
}

// tryNewlineRun matches \s*[\r\n]+ — a whitespace prefix ending in the last
// newline of the run (regex backtracking semantics: \s* is maximal but must
// leave a newline for [\r\n]+, so trailing non-newline whitespace after the
// last newline is NOT consumed).
func tryNewlineRun(r []rune, i int) (string, int, bool) {
	k := i
	for k < len(r) && isQwenWS(r[k]) {
		k++
	}
	last := -1
	for m := k - 1; m >= i; m-- {
		if isQwenNL(r[m]) {
			last = m
			break
		}
	}
	if last < 0 {
		return "", i, false
	}
	return string(r[i : last+1]), last + 1, true
}

// tryWhitespace matches \s+(?!\S) then \s+ — a whitespace run, shortened by
// one when followed by non-whitespace so the lookahead alternative can apply
// (e.g. "  b" splits " " + " b": the first space matches here, the second
// joins the word via tryLetters).
func tryWhitespace(r []rune, i int) (string, int, bool) {
	k := i
	for k < len(r) && isQwenWS(r[k]) {
		k++
	}
	if k == i {
		return "", i, false
	}
	if k == len(r) {
		return string(r[i:]), k, true // \s+(?!\S) at end of input
	}
	if k-i > 1 {
		return string(r[i : k-1]), k - 1, true
	}
	return string(r[i:k]), k, true // \s+ fallback (run of one)
}
