// Model download + on-disk config helpers (split from model.go).
// EnsureModel fetches a model when missing (with progress + download
// polling), patchTokenizerConfig normalizes a freshly-downloaded
// tokenizer, and the dir-level guards (dirSizeBytes, hasModelWeights,
// validModelConfig) decide whether a model directory is usable. The
// catalog / RAM-tiering / resolution logic stays in model.go.
package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sinter/llm/catalog"
)

// EnsureModel downloads a model if it's not already installed.
func EnsureModel(ctx context.Context, status ModelStatus, progressFn ProgressCallback) (string, error) {
	if status.Installed {
		return status.Dir, nil
	}

	bin := "hf"
	if _, err := exec.LookPath("hf"); err != nil {
		bin = "huggingface-cli"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("huggingface CLI not found — install with: pip install -U huggingface_hub")
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
	args := []string{"download", status.HFRepo}
	if status.HFInclude != "" {
		args = append(args, "--include", status.HFInclude)
	}
	args = append(args, "--local-dir", localDir)
	cmd := exec.CommandContext(ctx, bin, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("pipe stderr: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("pipe stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start download: %w", err)
	}

	// Drain stdout/stderr so the subprocess never blocks on a full pipe
	// buffer — hf download writes nothing to either when piped (see
	// pollDownloadProgress's doc comment), but the pipes still need a
	// reader.
	go io.Copy(io.Discard, stderr)
	go io.Copy(io.Discard, stdout)

	stopPoll := make(chan struct{})
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		pollDownloadProgress(localDir, stopPoll, progressFn)
	}()

	waitErr := cmd.Wait()
	close(stopPoll)
	<-pollDone
	if waitErr != nil {
		return "", fmt.Errorf("download failed: %w", waitErr)
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

// pollDownloadProgress reports download progress by periodically measuring
// how many bytes have landed in dest, until stop is closed.
//
// The obvious approach — scan the hf/huggingface-cli subprocess's stdout
// and stderr for a percentage — doesn't work: confirmed empirically (piped
// hf download, --format human/json/default all tried) that it writes
// ZERO bytes to either stream when not connected to a real terminal,
// regardless of --format. Its progress bars are tqdm-based and gated on
// isatty(), which a Go exec.Cmd pipe never satisfies. So the total is
// unknown up front (fn is called with total=0 — callers show bytes
// downloaded so far rather than a percentage) and progress is inferred
// from disk instead, which works regardless of the download tool's own
// TTY detection.
func pollDownloadProgress(dest string, stop <-chan struct{}, fn ProgressCallback) {
	if fn == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			fn(dirSizeBytes(dest), 0)
			return
		case <-ticker.C:
			fn(dirSizeBytes(dest), 0)
		}
	}
}

func dirSizeBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
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
