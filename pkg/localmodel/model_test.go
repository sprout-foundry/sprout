package localmodel

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecommendedModel_PrefersQuantOverRawWeights guards against a bug where
// RecommendedModel picked the first alphabetically-sorted sprout-tuned
// installed model instead of the RAM-gated, quant-preferring pick. Since
// "qwen35-4b-sprout-tuned" (raw bf16) sorts before
// "qwen35-4b-sprout-tuned-mlx-q5" alphabetically, the old implementation
// always returned the oversized raw model, which then failed the RAM gate
// at load time even though a quantized variant that fits was installed.
func TestRecommendedModel_PrefersQuantOverRawWeights(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{
		"qwen35-4b-sprout-tuned",
		"qwen35-4b-sprout-tuned-mlx-q5",
	} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	old := DefaultModelsDir
	DefaultModelsDir = root
	defer func() { DefaultModelsDir = old }()

	rec := RecommendedModel(8 * 1024 * 1024 * 1024)
	if rec == nil {
		t.Fatal("expected a recommendation, got nil")
	}
	if got := filepath.Base(rec.Dir); got != "qwen35-4b-sprout-tuned-mlx-q5" {
		t.Fatalf("got %s, want qwen35-4b-sprout-tuned-mlx-q5 (quantized variant should be preferred over raw weights)", got)
	}
}
