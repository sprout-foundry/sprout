//go:build !wasm && !cgo

// Package embedding provides stub types for ONNX embedding infrastructure
// when CGO is disabled. The actual implementation is in onnx_runtime.go and
// onnx_embedding_provider.go, which require CGO to link onnxruntime_go.
//
// When CGO is unavailable (e.g. Electron desktop builds on CI, or environments
// without the ONNX Runtime C library), these stubs ensure the package still
// compiles. The embedding manager detects onnxAvailable==false and falls back
// to static embeddings instead of ONNX.
package embedding

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// onnxAvailable is false — no ONNX backend is available in this build.
// CGO is disabled and there is no WASM JS bridge. The embedding manager
// detects onnxAvailable==false and falls back to static embeddings.
const onnxAvailable = false

// DefaultONNXRuntimeLibraryName is the default library name when CGO is disabled.
const DefaultONNXRuntimeLibraryName = ""

// ResolveONNXRuntimeLibrary resolves the ONNX runtime library path when CGO is disabled.
func ResolveONNXRuntimeLibrary(customName string) string {
	return ""
}

// TryDownloadONNXRuntime attempts to download the ONNX runtime when CGO is disabled.
func TryDownloadONNXRuntime(ctx context.Context, modelDir string) (string, error) {
	return "", fmt.Errorf("CGO is disabled; ONNX runtime cannot be used")
}

// onnxRequiresModelFiles reports whether the active ONNX backend expects .onnx
// and tokenizer.json on disk. In WASM it does not (JS bridge provides them).
// In non-CGO builds it is never called because onnxAvailable is false, but
// the symbol must exist for the package to compile.
// It is never called because onnxAvailable is false.
func onnxRequiresModelFiles() bool {
	return true
}

// DefaultModelDir returns the default model directory path.
// Used by manager.go even in non-CGO builds for path resolution.
func DefaultModelDir() string {
	dir := os.Getenv("SPROUT_MODEL_DIR")
	if dir != "" {
		return dir
	}
	u, err := user.Current()
	if err == nil && u.HomeDir != "" {
		return filepath.Join(u.HomeDir, ".cache", "sprout")
	}
	return filepath.Join(os.TempDir(), "sprout-models")
}

// ONNXRuntime is a stub type when CGO is disabled.
type ONNXRuntime struct{}

// NewONNXRuntime creates a new ONNX runtime when CGO is disabled.
func NewONNXRuntime(runtimePath string, numThreads int) (*ONNXRuntime, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX runtime cannot be used")
}

// NewONNXRuntimeWithDir creates a new ONNX runtime with directory when CGO is disabled.
func NewONNXRuntimeWithDir(modelDir string) (*ONNXRuntime, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX runtime cannot be used")
}

// OnnxRuntime returns the underlying ONNX runtime when CGO is disabled.
func (r *ONNXRuntime) OnnxRuntime() interface{} {
	return nil
}

// IsAvailable returns whether the ONNX runtime is available when CGO is disabled.
func (r *ONNXRuntime) IsAvailable() bool {
	return false
}

// RuntimePath returns the runtime path when CGO is disabled.
func (r *ONNXRuntime) RuntimePath() string {
	return ""
}

// Close releases resources when CGO is disabled.
func (r *ONNXRuntime) Close() error {
	return nil
}

// SessionOptions is a stub type when CGO is disabled.
type SessionOptions struct {
	numThreads int
}

// NewSessionOptions creates new session options when CGO is disabled.
func NewSessionOptions() *SessionOptions {
	return &SessionOptions{}
}

// SetIntraOpNumThreads sets the number of intra-op threads when CGO is disabled.
func (o *SessionOptions) SetIntraOpNumThreads(n int) {
	o.numThreads = n
}

// SetGraphOptimizationLevel sets the graph optimization level when CGO is disabled.
func (o *SessionOptions) SetGraphOptimizationLevel(level string) {}

// SetEnableCPUThreadPool sets the CPU thread pool when CGO is disabled.
func (o *SessionOptions) SetEnableCPUThreadPool(enable bool) {}

// CreateSession creates a new ONNX session when CGO is disabled.
func (r *ONNXRuntime) CreateSession(modelPath string, opts *SessionOptions) (ONNXInferenceSession, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX sessions cannot be created")
}

// ONNXInferenceSession is a stub type when CGO is disabled.
type ONNXInferenceSession interface {
	Run(ctx context.Context, inputMap map[string]interface{}, outputNames []string) (map[string]interface{}, error)
}

// ONNXEmbeddingProvider is a stub type when CGO is disabled.
type ONNXEmbeddingProvider struct {
	modelPath     string
	tokenizerPath string
	modelDims     int
}

// NewONNXEmbeddingProvider creates a new ONNX embedding provider when CGO is disabled.
func NewONNXEmbeddingProvider(ctx context.Context, runtime *ONNXRuntime, modelPath, tokenizerPath string, dims int) (*ONNXEmbeddingProvider, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding provider cannot be used")
}

// Embed implements EmbeddingProvider.
func (p *ONNXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding not available")
}

// EmbedBatch implements EmbeddingProvider.
func (p *ONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding not available")
}

// EmbedWithPrefix implements EmbeddingProvider.
func (p *ONNXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text string, prefix string) ([]float32, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding not available")
}

// EmbedBatchWithPrefix implements EmbeddingProvider.
func (p *ONNXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding not available")
}

// ModelDims returns the model dimensions when CGO is disabled.
func (p *ONNXEmbeddingProvider) ModelDims() int {
	return 0
}

// ModelPath returns the model path when CGO is disabled.
func (p *ONNXEmbeddingProvider) ModelPath() string {
	return ""
}

// TokenizerPath returns the tokenizer path when CGO is disabled.
func (p *ONNXEmbeddingProvider) TokenizerPath() string {
	return ""
}

// Close releases resources when CGO is disabled.
func (p *ONNXEmbeddingProvider) Close() error {
	return nil
}

// ModelHash returns a hash when CGO is disabled.
func (p *ONNXEmbeddingProvider) ModelHash() string {
	return ""
}

// Dimensions implements EmbeddingProvider. Returns 0 in no-CGO builds.
func (p *ONNXEmbeddingProvider) Dimensions() int {
	return 0
}

// Name implements EmbeddingProvider.
func (p *ONNXEmbeddingProvider) Name() string {
	return "onnx-stub"
}

// EmbedText implements EmbeddingProvider for text input.
func (p *ONNXEmbeddingProvider) EmbedText(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding not available")
}

// EmbedBatchText implements EmbeddingProvider for batch text input.
func (p *ONNXEmbeddingProvider) EmbedBatchText(ctx context.Context, texts []string) ([][]float32, error) {
	return nil, fmt.Errorf("CGO is disabled; ONNX embedding not available")
}
