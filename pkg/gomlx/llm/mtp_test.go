//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// modelDirRaw returns the raw-HF Qwen3.5-4B directory (carries mtp.* tensors)
// or "" when it isn't present locally.
func modelDirRaw(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := home + "/.cache/sprout/models/qwen3.5-4b-raw"
	if _, err := os.Stat(dir + "/model.safetensors.index.json"); err != nil {
		return ""
	}
	return dir
}

// TestMTPDraftAvailable loads the raw Qwen3.5-4B (the only local model that
// ships mtp.* tensors — mlx-community conversions strip them) and asserts the
// MTP head is detected and exposes a working draft path.
func TestMTPDraftAvailable(t *testing.T) {
	dir := modelDirRaw(t)
	if dir == "" {
		t.Skip("raw qwen3.5-4b with mtp tensors not present")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Skipf("NewModel: %v (raw BF16 model needs SPROUT_ALLOW_OVERWEIGHT=1 on this machine)", err)
	}
	defer model.Close()

	if !model.MTPAvailable() {
		t.Fatal("expected MTP head to be available on raw qwen3.5-4b")
	}

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 8
	cfg.Temperature = 0
	cfg.MaxMTPDrafts = 4

	out, err := model.GenerateText(context.Background(), "Hello", cfg)
	if err != nil {
		t.Fatalf("GenerateText with MTP: %v", err)
	}
	if out == "" {
		t.Fatal("empty generation with MTP")
	}
	t.Logf("mtp generation: %q", out)
}

// TestMTPParity is the lossless check: greedy generation with the MTP
// spec-decode path must produce byte-identical output to the plain greedy
// path. MTP acceptance is exact (the main model's argmax must equal the
// draft), so any divergence is a bug in draft/verify bookkeeping.
func TestMTPParity(t *testing.T) {
	dir := modelDirRaw(t)
	if dir == "" {
		t.Skip("raw qwen3.5-4b with mtp tensors not present")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Skipf("NewModel: %v (raw BF16 model needs SPROUT_ALLOW_OVERWEIGHT=1 on this machine)", err)
	}
	defer model.Close()

	prompts := []string{
		"Hello",
		"The capital of France is",
		"Write a short poem about the ocean.",
	}
	for _, prompt := range prompts {
		cfg := llm.DefaultGenerateConfig()
		cfg.MaxTokens = 24
		cfg.Temperature = 0
		cfg.MaxMTPDrafts = 4

		withMTP, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(%q) with MTP: %v", prompt, err)
		}

		cfg.MaxMTPDrafts = 0
		plain, err := model.GenerateText(context.Background(), prompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(%q) plain: %v", prompt, err)
		}

		if withMTP != plain {
			t.Errorf("MTP divergence for %q:\n  mtp:  %q\n  plain: %q", prompt, withMTP, plain)
		} else {
			t.Logf("parity ok for %q: %q", prompt, plain)
		}
	}
}
