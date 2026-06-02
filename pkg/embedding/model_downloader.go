package embedding

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ModelConfig describes an ONNX model to download.
type ModelConfig struct {
	Name          string // e.g. "embeddinggemma-300m"
	ModelURL      string // HuggingFace download URL for model.onnx
	TokenizerURL  string // HuggingFace download URL for tokenizer.json
	ModelHash     string // SHA256 hex of model file (empty = skip verification)
	TokenizerHash string // SHA256 hex of tokenizer file

	// ModelDataURL is the URL for the external weights blob (e.g. model_q4.onnx_data)
	// that ONNX Runtime loads as a sibling of the .onnx graph file. Required for
	// models that use external data; leave empty for self-contained .onnx files.
	ModelDataURL  string
	ModelDataHash string

	// FullDims is the model's native output dimensionality (e.g., 768 for EmbeddingGemma).
	// Used to allocate the output tensor in runInference.
	FullDims int

	// Dims is the desired output dimensionality after optional MRL truncation.
	// Must be <= FullDims. When equal to FullDims, no truncation is applied.
	Dims int
}

// defaultDownloadTimeout is the HTTP client timeout for model downloads.
const defaultDownloadTimeout = 5 * time.Minute

// ModelDownloader downloads ONNX models and tokenizers from HuggingFace.
type ModelDownloader struct {
	modelDir string // ~/.config/sprout/models/
	client   *http.Client
}

// NewModelDownloader creates a downloader that stores models in the default directory.
func NewModelDownloader() *ModelDownloader {
	return &ModelDownloader{
		modelDir: DefaultModelDir(),
		client:   &http.Client{Timeout: defaultDownloadTimeout},
	}
}

// NewModelDownloaderWithDir creates a downloader with a specific model directory.
func NewModelDownloaderWithDir(modelDir string) *ModelDownloader {
	return &ModelDownloader{
		modelDir: modelDir,
		client:   &http.Client{Timeout: defaultDownloadTimeout},
	}
}

// Download downloads the model and tokenizer files, validating checksums.
// If files already exist and checksums match, they are skipped.
// Progress is reported via the progress callback (0.0 to 1.0).
func (d *ModelDownloader) Download(ctx context.Context, cfg ModelConfig, progress func(float64)) error {
	dir := filepath.Join(d.modelDir, cfg.Name)

	// Create directory.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("model: create dir %s: %w", dir, err)
	}

	modelPath := filepath.Join(dir, "model_q4.onnx")
	modelDataPath := filepath.Join(dir, "model_q4.onnx_data")
	tokenizerPath := filepath.Join(dir, "tokenizer.json")

	// Build the phase list dynamically so progress maps to whatever combination
	// of {model graph, external weights, tokenizer} the caller asked for.
	type phase struct {
		path, url, hash, label string
	}
	var phases []phase
	if cfg.ModelURL != "" {
		phases = append(phases, phase{modelPath, cfg.ModelURL, cfg.ModelHash, cfg.Name})
	}
	if cfg.ModelDataURL != "" {
		phases = append(phases, phase{modelDataPath, cfg.ModelDataURL, cfg.ModelDataHash, cfg.Name + " weights"})
	}
	if cfg.TokenizerURL != "" {
		phases = append(phases, phase{tokenizerPath, cfg.TokenizerURL, cfg.TokenizerHash, "tokenizer"})
	}

	total := float64(len(phases))
	for i, ph := range phases {
		phaseStart := float64(i) / total
		if err := d.downloadFile(ctx, ph.path, ph.url, ph.hash,
			func(frac float64) {
				if progress != nil {
					progress(phaseStart + frac/total)
				}
			}); err != nil {
			return fmt.Errorf("model: download %s: %w", ph.label, err)
		}
	}

	// Write NOTICE file with license info (idempotent).
	noticePath := filepath.Join(dir, "NOTICE")
	if _, err := os.Stat(noticePath); os.IsNotExist(err) {
		notice := fmt.Sprintf("Model: %s\nLicense: See https://ai.google.dev/gemma/terms\n", cfg.Name)
		if err := os.WriteFile(noticePath, []byte(notice), 0o644); err != nil {
			return fmt.Errorf("model: write notice: %w", err)
		}
	}

	if progress != nil {
		progress(1.0)
	}

	return nil
}

// downloadFile downloads a single file from URL to destPath, validating SHA256 checksum.
// If the file already exists and the hash matches, the download is skipped.
// Progress is reported from 0.0 (start) to 1.0 (complete) for this file only.
func (d *ModelDownloader) downloadFile(ctx context.Context, destPath, url, expectedHash string, progress func(float64)) error {
	// Check if file exists and hash matches — skip download.
	if expectedHash != "" {
		if hash, err := d.fileHash(destPath); err == nil && hash == expectedHash {
			if progress != nil {
				progress(1.0)
			}
			return nil
		}
	} else {
		// No hash to verify; skip if file already exists.
		if _, err := os.Stat(destPath); err == nil {
			if progress != nil {
				progress(1.0)
			}
			return nil
		}
	}

	// Create HTTP request with context for cancellation.
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http error: %d %s", resp.StatusCode, resp.Status)
	}

	// Create temp file in the same directory for atomic rename.
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()

	// Download into temp file while computing hash.
	hasher := sha256.New()
	total := resp.ContentLength // may be 0 if server doesn't send Content-Length

	var written int64
	tee := io.TeeReader(resp.Body, hasher)

	// Wrap tmp with a counter so we can report progress.
	counter := &countWriter{w: tmp}
	if _, err = io.Copy(counter, tee); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copy: %w", err)
	}
	written = counter.n

	if progress != nil && total > 0 {
		progress(float64(written) / float64(total))
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp: %w", err)
	}

	// Validate hash.
	actualHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if expectedHash != "" && actualHash != expectedHash {
		os.Remove(tmpPath)
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	// Atomic rename to final destination.
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	if progress != nil {
		progress(1.0)
	}

	return nil
}

// countWriter wraps an io.Writer to track the number of bytes written.
type countWriter struct {
	w   io.Writer
	n   int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// fileHash computes SHA256 hash of a file. Returns empty string if file doesn't exist.
func (d *ModelDownloader) fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// GetModelPath returns the path to the model file for the given model name.
func (d *ModelDownloader) GetModelPath(name string) string {
	return filepath.Join(d.modelDir, name, "model_q4.onnx")
}

// GetTokenizerPath returns the path to the tokenizer file for the given model name.
func (d *ModelDownloader) GetTokenizerPath(name string) string {
	return filepath.Join(d.modelDir, name, "tokenizer.json")
}

// IsDownloaded returns true if both the model and tokenizer files exist for the given name.
func (d *ModelDownloader) IsDownloaded(name string) bool {
	_, err1 := os.Stat(d.GetModelPath(name))
	_, err2 := os.Stat(d.GetTokenizerPath(name))
	return err1 == nil && err2 == nil
}

// ---------------------------------------------------------------------------
// Predefined model configurations
// ---------------------------------------------------------------------------

// EmbeddingGemma300MConfig returns the predefined config for Google's
// EmbeddingGemma-300M (308M parameter) model — the actual published model name.
// The .onnx graph is small (~520 KB) but references an external weights blob
// (~197 MB) that must be downloaded into the same directory; both URLs are set.
//
// Source: the community ONNX export at onnx-community/embeddinggemma-300m-ONNX,
// since the official Google repo ships SafeTensors only.
//
// Hashes pin the upstream files we validated end-to-end. The downloader rejects
// mismatched files, so a poisoned mirror or MITM swap on the HuggingFace path
// fails closed instead of silently feeding a tampered model into the embedding
// pipeline. To update for a new upstream revision, regenerate by downloading
// the three files and running `sha256sum` against them.
func EmbeddingGemma300MConfig() ModelConfig {
	const base = "https://huggingface.co/onnx-community/embeddinggemma-300m-ONNX/resolve/main"
	return ModelConfig{
		Name:          "embeddinggemma-300m",
		ModelURL:      base + "/onnx/model_q4.onnx",
		ModelHash:     "ad1dfee81a70f7944b9b9d1cc6e48075b832881cf33fab2f2b248be78f3f0043",
		ModelDataURL:  base + "/onnx/model_q4.onnx_data",
		ModelDataHash: "599962c3143b040de2dd05e5975be3e9091dd067cacc6a8f7186e3203bab9e02",
		TokenizerURL:  base + "/tokenizer.json",
		TokenizerHash: "4dda02faaf32bc91031dc8c88457ac272b00c1016cc679757d1c441b248b9c47",
		FullDims:      768, // EmbeddingGemma-300M native output dimension
		Dims:          768, // Full dimension (no MRL truncation)
	}
}

// DownloadModel ensures the model and tokenizer files exist in modelDir for the
// given ModelConfig. If files already exist with matching checksums, download is skipped.
// Progress is reported via the callback (0.0 to 1.0).
func DownloadModel(ctx context.Context, modelDir string, cfg ModelConfig) error {
	d := NewModelDownloaderWithDir(modelDir)
	return d.Download(ctx, cfg, nil)
}
