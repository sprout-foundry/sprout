package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// ModelStatus describes a model from the user's perspective — either a
// downloadable catalog entry or an installed model discovered on disk.
type ModelStatus struct {
	Name          string `json:"name"`
	Dir           string `json:"dir"`
	HFRepo        string `json:"hf_repo"`
	MinRAM        uint64 `json:"min_ram_gb"`
	Installed     bool   `json:"installed"`
	Size          int64  `json:"size_bytes"`
	IsTuned       bool   `json:"is_tuned"`
	ParamSize     string `json:"param_size"`
	QuantBits     string `json:"quant_bits"`
	ServerBackend string `json:"server_backend,omitempty"`
}

// ProgressCallback is called during model download with bytes downloaded
// and total bytes (0 if unknown).
type ProgressCallback func(downloaded, total int64)

// ListModels returns all models: installed variants discovered on disk
// (including sprout-tuned, different quant levels) plus downloadable
// catalog entries that aren't installed. Installed models are listed first,
// sorted by param size (largest first).
func ListModels() []ModelStatus {
	installed := llm.ListInstalledModels(DefaultModelsDir)
	var statuses []ModelStatus

	seen := make(map[string]bool)

	for _, im := range installed {
		statuses = append(statuses, ModelStatus{
			Name:      im.Name,
			Dir:       im.Dir,
			Installed: true,
			Size:      im.SizeBytes,
			IsTuned:   im.IsTuned,
			ParamSize: im.ParamSize,
			QuantBits: im.QuantBits,
		})
		seen[im.Name] = true
	}

	// Add downloadable catalog entries that aren't installed.
	for _, m := range llm.ModelCatalog {
		dir := filepath.Join(DefaultModelsDir, m.Dir)
		if seen[m.Dir] || seen[m.Name] {
			continue
		}
		installedOnDisk := false
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			installedOnDisk = hasModelWeights(dir)
		}
		statuses = append(statuses, ModelStatus{
			Name:          m.Name,
			Dir:           dir,
			HFRepo:        m.HFRepo,
			MinRAM:        m.MinRAM / (1024 * 1024 * 1024),
			Installed:     installedOnDisk,
			ParamSize:     llm.ExtractParamSize(m.Name),
			ServerBackend: m.ServerBackend,
		})
		seen[m.Name] = true
	}

	return statuses
}

// RecommendedModel returns the best model for the machine's RAM that is
// already installed, preferring sprout-tuned variants.
func RecommendedModel(ramBytes uint64) *ModelStatus {
	// First: tuned installed model that fits
	if rec := llm.RecommendModelForRAM(ramBytes); rec != nil {
		for _, s := range ListModels() {
			if s.IsTuned && s.Installed && s.ParamSize == llm.ExtractParamSize(rec.Name) {
				return &s
			}
		}
	}

	// Second: any installed model that the RAM selector picks
	if rec := llm.RecommendModelForRAM(ramBytes); rec != nil {
		for _, s := range ListModels() {
			if s.Name == rec.Name && s.Installed {
				return &s
			}
		}
	}

	// Fall back: recommend download
	if rec := llm.RecommendModelForRAM(ramBytes); rec != nil {
		for _, s := range ListModels() {
			if s.Name == rec.Name {
				return &s
			}
		}
	}
	return nil
}

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

	cmd := exec.CommandContext(ctx, bin, "download", status.HFRepo, "--local-dir", dest)
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

	go parseDownloadProgress(stderr, progressFn)
	go parseDownloadProgress(stdout, progressFn)

	if err := cmd.Wait(); err != nil {
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

func parseDownloadProgress(r interface{ Read([]byte) (int, error) }, fn ProgressCallback) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && fn != nil {
			line := string(buf[:n])
			if pct := extractPercentage(line); pct >= 0 {
				fn(pct, 100)
			}
		}
		if err != nil {
			return
		}
	}
}

func extractPercentage(s string) int64 {
	idx := strings.Index(s, "%")
	if idx < 0 {
		return -1
	}
	start := idx
	for start > 0 && (s[start-1] >= '0' && s[start-1] <= '9') {
		start--
	}
	if start == idx {
		return -1
	}
	var pct int64
	for _, c := range s[start:idx] {
		pct = pct*10 + int64(c-'0')
	}
	return pct
}

func hasModelWeights(dir string) bool {
	return llm.HasWeights(dir)
}
