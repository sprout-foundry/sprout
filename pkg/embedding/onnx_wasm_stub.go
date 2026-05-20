//go:build wasm

package embedding

import (
	"context"
	"errors"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// This file provides stub types/functions for the ONNX embedding provider
// when building for WASM (js/wasm) where CGO is unavailable.
//
// The real implementations are in onnx_runtime.go, onnx_embedding_provider.go,
// and model_downloader.go (all behind !wasm).
// ---------------------------------------------------------------------------

var errWASMNotSupported = errors.New("onnx: ONNX provider not available on WASM")

// ---------------------------------------------------------------------------
// ONNXRuntime (stub)
// ---------------------------------------------------------------------------

// ONNXRuntime is a no-op on WASM.
type ONNXRuntime struct{}

func DefaultModelDir() string {
	return filepath.Join(".", "models")
}

func NewONNXRuntime() (*ONNXRuntime, error) {
	return nil, errWASMNotSupported
}

func NewONNXRuntimeWithDir(_ string) (*ONNXRuntime, error) {
	return nil, errWASMNotSupported
}

func (r *ONNXRuntime) Ready() bool {
	return false
}

func (r *ONNXRuntime) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// SessionOption (stub)
// ---------------------------------------------------------------------------

type SessionOption int

const (
	WithInterOpNumThreads SessionOption = iota
	WithIntraOpNumThreads
)

func (o SessionOption) Apply(_ interface{}) error {
	return nil
}

// ---------------------------------------------------------------------------
// ONNXEmbeddingProvider (stub)
// ---------------------------------------------------------------------------

type EmbedOptions struct {
	Task string
}

type ONNXEmbeddingProvider struct{}

func NewONNXEmbeddingProvider(
	_ context.Context,
	_ *ONNXRuntime,
	_, _ string,
	_ int,
) (*ONNXEmbeddingProvider, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) EmbedWithPrefix(_ context.Context, _, _ string) ([]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) EmbedBatchWithPrefix(_ context.Context, _ []string, _ string) ([][]float32, error) {
	return nil, errWASMNotSupported
}

func (p *ONNXEmbeddingProvider) Dimensions() int {
	return 0
}

func (p *ONNXEmbeddingProvider) Name() string {
	return "onnx-wasm-stub"
}

func (p *ONNXEmbeddingProvider) ModelHash() string {
	return ""
}

func (p *ONNXEmbeddingProvider) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// GemmaTokenizer (stub)
// ---------------------------------------------------------------------------

type GemmaTokenizer struct{}

func NewGemmaTokenizer(_ string) (*GemmaTokenizer, error) {
	return nil, errWASMNotSupported
}

func (t *GemmaTokenizer) EncodeWithBOSAndEOS(_ string) []uint32 {
	return nil
}

func (t *GemmaTokenizer) VocabularySize() int {
	return 0
}

func (t *GemmaTokenizer) Tokenize(_ string) []string {
	return nil
}

func (t *GemmaTokenizer) TokenizeTrailingContext(_ string) []string {
	return nil
}

// ---------------------------------------------------------------------------
// ModelConfig (stub)
// ---------------------------------------------------------------------------

// ModelConfig describes an ONNX model to download.
type ModelConfig struct {
	Name          string
	ModelURL      string
	TokenizerURL  string
	ModelHash     string
	TokenizerHash string
}

// EmbeddingGemma2925MConfig returns the predefined config for the
// EmbeddingGemma-2-925M model. On WASM this returns a zero config.
func EmbeddingGemma2925MConfig() ModelConfig {
	return ModelConfig{
		Name: "embeddinggemma-2-925m",
	}
}

// ---------------------------------------------------------------------------
// ModelDownloader (stub)
// ---------------------------------------------------------------------------

type ModelDownloader struct {
	modelDir string
}

func NewModelDownloader() *ModelDownloader {
	return &ModelDownloader{modelDir: DefaultModelDir()}
}

func NewModelDownloaderWithDir(modelDir string) *ModelDownloader {
	return &ModelDownloader{modelDir: modelDir}
}

func (d *ModelDownloader) Download(_ context.Context, _ ModelConfig, _ func(float64)) error {
	return errWASMNotSupported
}

func (d *ModelDownloader) GetModelPath(name string) string {
	return filepath.Join(d.modelDir, name, "model.onnx")
}

func (d *ModelDownloader) GetTokenizerPath(name string) string {
	return filepath.Join(d.modelDir, name, "tokenizer.json")
}

func (d *ModelDownloader) IsDownloaded(_ string) bool {
	return false
}

// DownloadModel ensures the model and tokenizer are available at the given
// modelDir. On WASM this always returns an error.
func DownloadModel(_ context.Context, _ string, _ ModelConfig) error {
	return errWASMNotSupported
}
