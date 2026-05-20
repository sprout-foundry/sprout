package embedding

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// ModelConfig describes an ONNX model to download.
type ModelConfig struct {
	Name          string // e.g. "embeddinggemma-300m-q8"
	ModelURL      string // HuggingFace download URL for model.onnx
	TokenizerURL  string // HuggingFace download URL for tokenizer.json
	ModelHash     string // SHA256 hex of model file
	TokenizerHash string // SHA256 hex of tokenizer file
}

// ModelDownloader downloads ONNX models and tokenizers from HuggingFace.
type ModelDownloader struct {
	modelDir string // ~/.config/sprout/models/
	client   *http.Client
}

// NewModelDownloader creates a downloader that stores models in the default directory.
func NewModelDownloader() *ModelDownloader {
	return &ModelDownloader{
		modelDir: DefaultModelDir(),
		client:   &http.Client{},
	}
}

// NewModelDownloaderWithDir creates a downloader with a specific model directory.
func NewModelDownloaderWithDir(modelDir string) *ModelDownloader {
	return &ModelDownloader{
		modelDir: modelDir,
		client:   &http.Client{},
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
	tokenizerPath := filepath.Join(dir, "tokenizer.json")

	// Determine total download phases for progress calculation.
	var phases int
	if cfg.ModelURL != "" {
		phases++
	}
	if cfg.TokenizerURL != "" {
		phases++
	}

	// Download model.
	if cfg.ModelURL != "" {
		phaseStart := 0.0
		if err := d.downloadFile(ctx, modelPath, cfg.ModelURL, cfg.ModelHash,
			func(frac float64) { progress(phaseStart + frac/float64(phases)); }); err != nil {
			return fmt.Errorf("model: download %s: %w", cfg.Name, err)
		}
	}

	// Download tokenizer.
	if cfg.TokenizerURL != "" {
		phaseStart := float64(0)
		if phases > 1 {
			phaseStart = 1.0 / float64(phases)
		}
		if err := d.downloadFile(ctx, tokenizerPath, cfg.TokenizerURL, cfg.TokenizerHash,
			func(frac float64) { progress(phaseStart + frac/float64(phases)); }); err != nil {
			return fmt.Errorf("model: download tokenizer: %w", err)
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

// EmbeddingGemma2925MConfig returns the predefined config for the
// EmbeddingGemma-2-925M model.
func EmbeddingGemma2925MConfig() ModelConfig {
	return ModelConfig{
		Name:         "embeddinggemma-2-925m",
		ModelURL:     "https://huggingface.co/google/EmbeddingGemma-2-925M/resolve/main/embeddinggemma-2-925m/model_q4.onnx",
		TokenizerURL: "https://huggingface.co/google/EmbeddingGemma-2-925M/resolve/main/embeddinggemma-2-925m/tokenizer.json",
		ModelHash:    "d150749d0e15322e14eaff4994085e16e42fcc09f56f7a023bd3baf560441408",
	}
}

// DownloadModel ensures the model and tokenizer files exist in modelDir for the
// given ModelConfig. If files already exist with matching checksums, download is skipped.
// Progress is reported via the callback (0.0 to 1.0).
func DownloadModel(ctx context.Context, modelDir string, cfg ModelConfig) error {
	d := NewModelDownloaderWithDir(modelDir)
	return d.Download(ctx, cfg, nil)
}
