//go:build arm64 && cgo && (darwin || (linux && ggml))

package llm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMultimodalTokenFence pins the modality-placeholder fence: tokens whose
// string names an image/audio/video/vision placeholder are hidden from
// callbacks on every decode path (shouldFilterToken), because a text-only
// engine can never legitimately emit them — the 12B q5 sprout-tuned build
// surfaced "using a<audio|> called a hash function" mid-answer.
func TestMultimodalTokenFence(t *testing.T) {
	tok := &Tokenizer{
		specialTokens: map[string]int{
			"<|im_start|>":    1,
			"<|im_end|>":      2,
			"<think>":         3,
			"</think>":        4,
			"<image_pad>":     248056,
			"<video_pad>":     248057,
			"<audio|>":        258883,
			"<|image|>":       255999,
			"<vision_start|>": 248053,
			// Text-side structural markers that must NOT be fenced:
			"<|channel>":      100,
			"<channel|>":      101,
			"<|startoftext|>": 5,
		},
	}
	m := &Model{tokenizer: tok}
	m.initThinking()

	for id := range tok.multimodalTokenIDs() {
		if !m.shouldFilterToken(id, GenerateConfig{}) {
			t.Errorf("token %d (%q) should be fenced", id, tok.idToTok[id])
		}
	}
	for _, safe := range []int{1, 2, 5, 100, 101} {
		if m.shouldFilterToken(safe, GenerateConfig{}) {
			t.Errorf("token %d should NOT be fenced", safe)
		}
	}
	// <think>/</think> markers are always dropped from output (pre-existing
	// thinking-filter behavior) — outside this test's scope.
	_ = tok.IDOf("<think>")
	// Sanity on the exact production case: <audio|> fenced, channel markers not.
	if !m.shouldFilterToken(258883, GenerateConfig{}) {
		t.Error("<audio|> must be fenced")
	}
	if m.shouldFilterToken(100, GenerateConfig{}) || m.shouldFilterToken(101, GenerateConfig{}) {
		t.Error("Gemma4 channel markers must not be fenced — they are text-side structure")
	}
}

// TestMultimodalTokenFenceFromTokenizer loads a real tokenizer.json shape
// (Qwen3.5) and verifies the resolved ban set matches the known placeholder
// IDs and only those.
func TestMultimodalTokenFenceFromTokenizer(t *testing.T) {
	dir := t.TempDir()
	tj := `{
	  "model": {"type": "BPE", "vocab": {"a": 10, "b": 11}, "merges": []},
	  "added_tokens": [
	    {"id": 1, "content": "<|im_start|>"},
	    {"id": 2, "content": "<|im_end|>"},
	    {"id": 3, "content": "<think>"},
	    {"id": 4, "content": "</think>"},
	    {"id": 248053, "content": "<|vision_start|>"},
	    {"id": 248056, "content": "<|image_pad|>"},
	    {"id": 248057, "content": "<|video_pad|>"},
	    {"id": 100, "content": "<|channel>"},
	    {"id": 101, "content": "<channel|>"}
	  ]
	}`
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(tj), 0o644); err != nil {
		t.Fatal(err)
	}
	tok, err := LoadTokenizer(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := tok.multimodalTokenIDs()
	want := map[int]bool{248053: true, 248056: true, 248057: true}
	if len(got) != len(want) {
		t.Errorf("ban set = %v, want exactly %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("id %d missing from ban set", id)
		}
	}
	if got[100] || got[101] {
		t.Error("channel markers must not be banned")
	}
}
