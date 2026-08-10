package localmodel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// ModelStatus describes a model in the catalog from the user's perspective.
type ModelStatus struct {
	Name      string `json:"name"`
	Dir       string `json:"dir"`
	HFRepo    string `json:"hf_repo"`
	MinRAM    uint64 `json:"min_ram_gb"`
	Installed bool   `json:"installed"`
	Size      int64  `json:"size_bytes"` // on-disk size if installed
}

// ProgressCallback is called during model download with bytes downloaded
// and total bytes (0 if unknown).
type ProgressCallback func(downloaded, total int64)

// ListModels returns all catalog models with their installation status.
// The catalog is ordered from largest to smallest; the caller should
// highlight the recommended model for the machine's RAM.
func ListModels() []ModelStatus {
	catalog := llm.ModelCatalog
	statuses := make([]ModelStatus, len(catalog))
	for i, m := range catalog {
		dir := filepath.Join(DefaultModelsDir, m.Dir)
		installed := false
		var size int64
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			installed = hasModelWeights(dir)
			if installed {
				size = dirSize(dir)
			}
		}
		statuses[i] = ModelStatus{
			Name:      m.Name,
			Dir:       dir,
			HFRepo:    m.HFRepo,
			MinRAM:    m.MinRAM / (1024 * 1024 * 1024),
			Installed: installed,
			Size:      size,
		}
	}
	return statuses
}

// RecommendedModel returns the best model for the machine's RAM that is
// already installed, or the recommended one to download if none installed.
func RecommendedModel(ramBytes uint64) *ModelStatus {
	// First try: installed model that fits
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

// EnsureModel downloads a model if it's not already installed. The progressFn
// callback is called periodically with download progress. Returns the absolute
// path to the model directory when ready.
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

	// Parse progress from stderr (hf CLI prints progress bars on stderr).
	// The format is: "Fetching X files: 45%|████▌     | 1.2G/2.5G"
	go parseDownloadProgress(stderr, progressFn)
	go parseDownloadProgress(stdout, progressFn)

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	return dest, nil
}

// parseDownloadProgress reads hf CLI output lines and calls fn with progress.
func parseDownloadProgress(r interface{ Read([]byte) (int, error) }, fn ProgressCallback) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && fn != nil {
			line := string(buf[:n])
			// Extract percentage if present
			if pct := extractPercentage(line); pct >= 0 {
				// We don't know total bytes, so report percentage as
				// downloaded/100. The UI can show percentage directly.
				fn(pct, 100)
			}
		}
		if err != nil {
			return
		}
	}
}

// extractPercentage parses "45%" from hf CLI output. Returns -1 if not found.
func extractPercentage(s string) int64 {
	idx := strings.Index(s, "%")
	if idx < 0 {
		return -1
	}
	// Walk backward from % to find the number
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

// hasModelWeights checks if a directory contains actual model weights
// (not just metadata files from a partial download).
func hasModelWeights(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".safetensors") ||
			strings.HasSuffix(name, ".gguf") ||
			strings.HasSuffix(name, "model.safetensors.index.json") {
			return true
		}
	}
	return false
}

// dirSize returns the total size of all files in a directory tree.
func dirSize(dir string) int64 {
	var size int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
