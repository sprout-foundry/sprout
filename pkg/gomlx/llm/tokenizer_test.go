//go:build arm64 && cgo && (darwin || (linux && ggml))

package llm

import (
	"reflect"
	"strings"
	"testing"
)

// TestQwenPreTokenizeNewlineSemantics pins the pre-tokenizer's newline
// handling to HuggingFace's Qwen Split-regex behavior. The previous
// whitespace-attaching splitter merged newline runs into the following word
// ("system\nYou" → ["system", "ĠYou"]) and dropped trailing newlines, so
// every multi-line (chat) prompt encoded to different token IDs than the
// reference tokenizer. Expected segments below are the byte-level-encoded
// forms verified token-for-token against tokenizers' Tokenizer for
// Qwen3.5/Qwen3.6 vocabularies.
func TestQwenPreTokenizeNewlineSemantics(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// \s*[\r\n]+ keeps newline runs standalone: Ċ = \n
		{"system\nYou", []string{"system", "Ċ", "You"}},
		{"a\n\nb", []string{"a", "ĊĊ", "b"}},
		{"end\n", []string{"end", "Ċ"}},
		{"x \n", []string{"x", "ĠĊ"}},
		{"  \n c", []string{"ĠĠĊ", "Ġc"}},
		// punct runs absorb trailing newlines (BPE re-splits: ?! + ... + Ċ —
		// verified token-for-token against HF: [14556 25153 1076 198 3480])
		{"hello?!...\nnext", []string{"hello", "?!...Ċ", "next"}},
		// \t is a valid optional-prefix char for a letter run; BPE splits
		// ĉhere into ĉ + here (HF: [5999 197 6527])
		{"tab\there", []string{"tab", "ĉhere"}},
		// \s+(?!\S) leaves one space for the following word
		{"1 2 3", []string{"1", "Ġ", "2", "Ġ", "3"}},
		// contractions and single digits
		{"it's 42", []string{"it", "'s", "Ġ", "4", "2"}},
	}
	for _, tc := range cases {
		got := qwenPreTokenize(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("qwenPreTokenize(%q)\n  got  %v\n  want %v", tc.in, got, tc.want)
		}
	}
}

// TestQwenSplitTrailingNewlineNotDropped guards the exact regression that
// collapsed 2-bit MoE generation: a trailing "\n" after the last special
// token (chat templates end "<think>\n") used to vanish entirely. <x is one
// pre-token (punct prefix + letters); the punct token absorbs its newline.
func TestQwenSplitTrailingNewlineNotDropped(t *testing.T) {
	got := qwenSplit("assistant\n<x>\n")
	want := []string{"assistant", "\n", "<x", ">\n"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("qwenSplit dropped or merged newline runs\n  got  %q\n  want %q", got, want)
	}
}

// TestDecodeByteLevelRoundTrip checks decode inverts the byte-level mapping.
func TestDecodeByteLevelRoundTrip(t *testing.T) {
	for _, s := range []string{"hello world", "a\nb", "tab\there", "  spaces  ", "punct!?..."} {
		enc := qwenByteEncode(s)
		dec := decodeByteLevel(enc)
		if dec != s {
			t.Errorf("round trip %q → %q → %q", s, enc, dec)
		}
	}
}

// TestFormatChat_HistoryReplayMatchesGeneration guards the KV prefix cache's
// core invariant: the prompt for turn N must be an exact prefix of the
// prompt for turn N+1 once the assistant's turn-N reply is appended as
// history. If FormatChat renders a completed assistant turn differently
// than it rendered the same position as a live "generate now" cue (e.g.
// dropping the <think></think> marker), every multi-turn conversation
// silently loses KV-cache reuse and re-prefills from scratch on every call.
func TestFormatChat_HistoryReplayMatchesGeneration(t *testing.T) {
	tok := &Tokenizer{}

	turn1 := []ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "list files"},
	}
	prompt1 := tok.formatQwenChat(turn1)

	turn2 := []ChatMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: "<tool_call>\n<function=ls>\n</function>\n</tool_call>\n"},
		{Role: "user", Content: "<tool_response>\na.go\n</tool_response>"},
	}
	prompt2 := tok.formatQwenChat(turn2)

	if !strings.HasPrefix(prompt2, prompt1) {
		t.Fatalf("turn-2 prompt is not an exact prefix extension of turn-1 prompt (breaks KV prefix-cache reuse)\nturn1=%q\nturn2=%q", prompt1, prompt2)
	}
}

func TestFormatLFM2Chat_HistoryReplayMatchesGeneration(t *testing.T) {
	tok := &Tokenizer{}

	turn1 := []ChatMessage{
		{Role: "system", Content: "List of tools: []"},
		{Role: "user", Content: "list files"},
	}
	prompt1 := tok.formatLFM2Chat(turn1)

	turn2 := []ChatMessage{
		{Role: "system", Content: "List of tools: []"},
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: "<|tool_call_start|>[ls()]<|tool_call_end|>"},
		{Role: "user", Content: "a.go"},
	}
	prompt2 := tok.formatLFM2Chat(turn2)

	if !strings.HasPrefix(prompt2, prompt1) {
		t.Fatalf("turn-2 prompt is not an exact prefix extension of turn-1 prompt (breaks KV prefix-cache reuse)\nturn1=%q\nturn2=%q", prompt1, prompt2)
	}
}
