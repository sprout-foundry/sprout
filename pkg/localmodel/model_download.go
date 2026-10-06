// Model download + on-disk config helpers (split from model.go).
// EnsureModel fetches a model when missing (the HTTPS downloader lives in
// hf_download.go), patchTokenizerConfig normalizes a freshly-downloaded
// tokenizer, and the dir-level guards (hasModelWeights, validModelConfig)
// decide whether a model directory is usable. The catalog / RAM-tiering /
// resolution logic stays in model.go.
package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sinter/llm/catalog"
)

// EnsureModel downloads a model from Hugging Face if it's not already
// installed. progressFn receives aggregate bytes across all selected repo
// files; total is known once the file listing completes.
func EnsureModel(ctx context.Context, status ModelStatus, progressFn ProgressCallback) (string, error) {
	if status.Installed {
		return status.Dir, nil
	}

	dest := status.Dir
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("create models dir: %w", err)
	}

	// When HFInclude is set, files are downloaded with their repo path
	// prefix preserved. Download to the parent of dest so the include
	// subdir lands correctly under dest.
	localDir := dest
	if status.HFInclude != "" {
		localDir = filepath.Dir(dest)
	}
	if err := downloadHFRepo(ctx, status.HFRepo, status.HFInclude, localDir, progressFn); err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	patchTokenizerConfig(dest)

	return dest, nil
}

// patchTokenizerConfig patches tokenizer_config.json for models whose chat
// template tool-call format isn't auto-detected by mlx_lm's inference logic.
// Currently handles LFM2 models (need tool_parser_type=pythonic).
func patchTokenizerConfig(modelDir string) {
	configPath := filepath.Join(modelDir, "config.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	var cfg struct {
		ModelType string `json:"model_type"`
	}
	if json.Unmarshal(configData, &cfg) != nil {
		return
	}

	var toolParser string
	switch cfg.ModelType {
	case "lfm2":
		toolParser = "pythonic"
	default:
		return
	}

	tokPath := filepath.Join(modelDir, "tokenizer_config.json")
	tokData, err := os.ReadFile(tokPath)
	if err != nil {
		return
	}
	var tokConfig map[string]interface{}
	if json.Unmarshal(tokData, &tokConfig) != nil {
		return
	}
	if existing, ok := tokConfig["tool_parser_type"].(string); ok && existing == toolParser {
		return
	}
	tokConfig["tool_parser_type"] = toolParser
	patched, err := json.MarshalIndent(tokConfig, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(tokPath, patched, 0o644)
}

func hasModelWeights(dir string) bool {
	return catalog.HasWeights(dir)
}

// validModelConfig reports whether the model directory carries a parseable
// config.json with the fields the engine requires. A truncated or empty
// config (seen after interrupted downloads) otherwise survives weight-based
// install checks and only surfaces as a divide-by-zero panic deep in
// sinter's LoadConfig at load time (HiddenSize / NumHeads with NumHeads==0).
// A missing config.json is fine — sprout-tuned variants and GGUF models
// don't ship one.
func validModelConfig(dir string) bool {
	cfgPath := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return os.IsNotExist(err) // unreadable file is suspicious; missing is fine
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return false
	}

	var raw struct {
		NumAttentionHeads int `json:"num_attention_heads"`
		HiddenSize        int `json:"hidden_size"`
		TextConfig        *struct {
			NumAttentionHeads int `json:"num_attention_heads"`
			HiddenSize        int `json:"hidden_size"`
		} `json:"text_config"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	numHeads := raw.NumAttentionHeads
	hidden := raw.HiddenSize
	if raw.TextConfig != nil {
		if numHeads == 0 {
			numHeads = raw.TextConfig.NumAttentionHeads
		}
		if hidden == 0 {
			hidden = raw.TextConfig.HiddenSize
		}
	}
	return numHeads > 0 && hidden > 0
}
