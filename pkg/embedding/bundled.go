//go:build cgo

package embedding

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Package-level ORT lifecycle management.
// The ONNX Runtime environment is global and must be initialized once per process.
var (
	ortOnce    sync.Once
	ortInitErr error
)

// BundledProvider is an EmbeddingProvider backed by a local ONNX model.
// It loads the MiniLM model from a directory containing tokenizer.json and
// the .onnx model file, and uses onnxruntime_go for inference.
type BundledProvider struct {
	mu        sync.Mutex
	session   *ort.DynamicAdvancedSession
	tokenizer *Tokenizer
	dims      int
	maxLen    int

	closed bool
}

// NewBundledProvider creates a provider that loads models from modelDir.
// ortLibPath is the path to the onnxruntime shared library (e.g., libonnxruntime.so).
//
// If modelDir is non-empty, the model and tokenizer are read from disk at
// modelDir/all-MiniLM-L6-v2-int8.onnx and modelDir/tokenizer.json.
// If modelDir is empty, the embedded model and tokenizer are used, so no
// external files are needed at runtime.
func NewBundledProvider(modelDir string, ortLibPath string) (*BundledProvider, error) {
	const (
		modelFile   = "all-MiniLM-L6-v2-int8.onnx"
		tokenizerFile = "tokenizer.json"
		dims        = 384
		maxLen      = 128
	)

	// Resolve ORT library path with fallback chain.
	resolvedPath, err := resolveORTLibrary(ortLibPath)
	if err != nil {
		return nil, err
	}

	// Initialize ORT environment once globally.
	ortOnce.Do(func() {
		ort.SetSharedLibraryPath(resolvedPath)
		ortInitErr = ort.InitializeEnvironment(ort.WithLogLevelWarning())
	})
	if ortInitErr != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", ortInitErr)
	}

	var modelData, tokenizerData []byte

	if modelDir != "" {
		// Read from disk (backward compatible).
		modelPath := filepath.Join(modelDir, modelFile)
		tokenizerPath := filepath.Join(modelDir, tokenizerFile)

		modelData, err = os.ReadFile(modelPath)
		if err != nil {
			return nil, fmt.Errorf("read model: %w", err)
		}
		tokenizerData, err = os.ReadFile(tokenizerPath)
		if err != nil {
			return nil, fmt.Errorf("read tokenizer: %w", err)
		}
	} else {
		// Use embedded model data.
		modelData = modelONNX
		tokenizerData = embeddedTokenizerJSON
	}

	tok, err := NewTokenizerJSON(tokenizerData, maxLen)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	session, err := ort.NewDynamicAdvancedSessionWithONNXData(
		modelData,
		[]string{"input_ids", "attention_mask"},
		[]string{"937"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &BundledProvider{
		session:   session,
		tokenizer: tok,
		dims:      dims,
		maxLen:    maxLen,
	}, nil
}

// Embed returns a L2-normalized 384-dim embedding vector for the given text.
func (p *BundledProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
// Results are returned in the same order as input.
func (p *BundledProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(texts) == 0 {
		return nil, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("provider is closed")
	}

	batchSize := len(texts)

	// Tokenize all texts into padded input_ids and attention_mask
	ids, mask, _ := p.tokenizer.TokenizeBatch(texts)
	// ids and mask are []int64 of size batchSize * maxLen

	// Create tensors with shape {batchSize, maxLen}
	inputIDs, err := ort.NewTensor(ort.Shape{int64(batchSize), int64(p.maxLen)}, ids)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDs.Destroy()

	inputMask, err := ort.NewTensor(ort.Shape{int64(batchSize), int64(p.maxLen)}, mask)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer inputMask.Destroy()

	// Output tensor with shape {batchSize, dims}
	outputVec, err := ort.NewEmptyTensor[float32](ort.Shape{int64(batchSize), int64(p.dims)})
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputVec.Destroy()

	// Run inference
	if err := p.session.Run([]ort.Value{inputIDs, inputMask}, []ort.Value{outputVec}); err != nil {
		return nil, fmt.Errorf("run batch: %w", err)
	}

	// Extract and normalize results
	out := outputVec.GetData()
	results := make([][]float32, batchSize)
	for i := 0; i < batchSize; i++ {
		start := i * p.dims
		results[i] = l2Normalize(out[start : start+p.dims])
	}
	return results, nil
}

// Dimensions returns the dimensionality of vectors produced by this provider (384).
func (p *BundledProvider) Dimensions() int {
	return p.dims
}

// Name returns a human-readable identifier for the provider.
func (p *BundledProvider) Name() string {
	return "bundled-minilm"
}

// Close releases all resources held by the provider.
// This only destroys the session; the global ORT environment is managed
// separately via CleanupORT().
func (p *BundledProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	if p.session != nil {
		if err := p.session.Destroy(); err != nil {
			return fmt.Errorf("destroy session: %w", err)
		}
		p.session = nil
	}
	return nil
}

// CleanupORT destroys the global ONNX Runtime environment.
// Call this once on process exit (e.g., via defer in main).
// Do NOT call this while any BundledProvider is still active.
func CleanupORT() error {
	return ort.DestroyEnvironment()
}

// l2Normalize returns a unit vector in the same direction as v.
func l2Normalize(v []float32) []float32 {
	norm := float64(0)
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	norm = math.Sqrt(norm)
	if norm < 1e-9 {
		return append([]float32{}, v...)
	}
	result := make([]float32, len(v))
	for i, x := range v {
		result[i] = float32(float64(x) / norm)
	}
	return result
}
