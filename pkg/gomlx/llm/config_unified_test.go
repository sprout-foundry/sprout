//go:build arm64 && cgo && (darwin || (linux && ggml))

package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigGemma4Unified pins the gemma4_unified_text parsing that the
// 12B sprout-tuned build depends on: aliasing to the gemma4 engine, the
// 6-layer full-attention pattern derived from layer_types (not the gemma4_text
// default 5), and the k_eq_v global-KV-head fields.
func TestLoadConfigGemma4Unified(t *testing.T) {
	dir := t.TempDir()
	layerTypes := make([]string, 48)
	for i := range layerTypes {
		if (i+1)%6 == 0 {
			layerTypes[i] = "full_attention"
		} else {
			layerTypes[i] = "sliding_attention"
		}
	}
	raw := map[string]interface{}{
		"model_type":          "gemma4_unified",
		"tie_word_embeddings": true,
		"text_config": map[string]interface{}{
			"model_type":                 "gemma4_unified_text",
			"hidden_size":                3840,
			"num_hidden_layers":          48,
			"num_attention_heads":        16,
			"num_key_value_heads":        8,
			"num_global_key_value_heads": 1,
			"head_dim":                   256,
			"global_head_dim":            512,
			"attention_k_eq_v":           true,
			"layer_types":                layerTypes,
			"vocab_size":                 262144,
		},
	}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Arch != "gemma4_text" {
		t.Errorf("Arch = %q, want gemma4_text (engine alias)", cfg.Arch)
	}
	if cfg.SlidingWindowPattern != 6 {
		t.Errorf("SlidingWindowPattern = %d, want 6 (first full_attention at index 5)", cfg.SlidingWindowPattern)
	}
	if cfg.NumGlobalKVHeads != 1 {
		t.Errorf("NumGlobalKVHeads = %d, want 1", cfg.NumGlobalKVHeads)
	}
	if !cfg.AttentionKEqV {
		t.Error("AttentionKEqV = false, want true")
	}
	if cfg.GlobalHeadDim != 512 {
		t.Errorf("GlobalHeadDim = %d, want 512", cfg.GlobalHeadDim)
	}
	// Positional pattern must agree with the per-layer types: layer 5 full,
	// layer 4 sliding.
	if (5+1)%cfg.SlidingWindowPattern != 0 {
		t.Error("layer 5 should be full attention under pattern 6")
	}
	if (4+1)%cfg.SlidingWindowPattern == 0 {
		t.Error("layer 4 should be sliding attention under pattern 6")
	}
}
