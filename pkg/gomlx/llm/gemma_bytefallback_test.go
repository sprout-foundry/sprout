//go:build darwin && arm64 && cgo

package llm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestGemmaByteFallbackDecode verifies <0xXX> byte-fallback tokens decode
// back to raw bytes (HF ground truth: 'box: ⎿ end' -> [2566 236787 236743 464 380 429 1345]).
func TestGemmaByteFallbackDecode(t *testing.T) {
	dir := os.Getenv("HOME") + "/dev/llm-models/gemma-4-e2b-it-4bit"
	if _, err := os.Stat(filepath.Join(dir, "tokenizer.json")); err != nil {
		t.Skip("model not found")
	}
	tok, err := llm.LoadTokenizer(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		t.Fatalf("LoadTokenizer: %v", err)
	}

	text := "box: ⎿ end"
	ids := tok.Encode(text)
	want := []int{2566, 236787, 236743, 464, 380, 429, 1345}
	t.Logf("got ids: %v", ids)
	if len(ids) != len(want) {
		t.Fatalf("encode mismatch: got %v want %v", ids, want)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Fatalf("encode mismatch at %d: got %v want %v", i, ids, want)
		}
	}
	decoded := tok.Decode(ids)
	if decoded != text {
		t.Fatalf("decode mismatch: got %q want %q", decoded, text)
	}
}
