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

// BundledProvider is an EmbeddingProvider backed by a local ONNX model.
// It loads the MiniLM model from a directory containing tokenizer.json and
// the .onnx model file, and uses onnxruntime_go for inference.
type BundledProvider struct {
	mu        sync.Mutex
	session   *ort.AdvancedSession
	tokenizer *Tokenizer
	dims      int
	maxLen    int

	// Single-example tensors (batch_size=1), reused across Embed() calls.
	inputIDs   *ort.Tensor[int64]
	inputMask  *ort.Tensor[int64]
	outputVec  *ort.Tensor[float32]

	closed bool
}

// NewBundledProvider creates a provider that loads models from modelDir.
// ortLibPath is the path to the onnxruntime shared library (e.g., libonnxruntime.so).
// The model file must be at modelDir/all-MiniLM-L6-v2-int8.onnx and
// the tokenizer at modelDir/tokenizer.json.
func NewBundledProvider(modelDir string, ortLibPath string) (*BundledProvider, error) {
	const (
		modelFile = "all-MiniLM-L6-v2-int8.onnx"
		tokenizerFile = "tokenizer.json"
		dims       = 384
		maxLen     = 128
	)

	ort.SetSharedLibraryPath(ortLibPath)
	if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", err)
	}

	modelPath := filepath.Join(modelDir, modelFile)
	tokenizerPath := filepath.Join(modelDir, tokenizerFile)

	modelData, err := os.ReadFile(modelPath)
	if err != nil {
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("read model: %w", err)
	}

	tokenizerData, err := os.ReadFile(tokenizerPath)
	if err != nil {
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("read tokenizer: %w", err)
	}

	tok, err := NewTokenizerJSON(tokenizerData, maxLen)
	if err != nil {
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}

	// Create input/output tensors for batch_size=1
	shape := ort.Shape{1, maxLen}
	inputIDs, err := ort.NewEmptyTensor[int64](shape)
	if err != nil {
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	inputMask, err := ort.NewEmptyTensor[int64](shape)
	if err != nil {
		inputIDs.Destroy()
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	outputShape := ort.Shape{1, dims}
	outputVec, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		inputIDs.Destroy()
		inputMask.Destroy()
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("create output tensor: %w", err)
	}

	session, err := ort.NewAdvancedSessionWithONNXData(
		modelData,
		[]string{"input_ids", "attention_mask"},
		[]string{"937"},
		[]ort.Value{inputIDs, inputMask},
		[]ort.Value{outputVec},
		nil,
	)
	if err != nil {
		inputIDs.Destroy()
		inputMask.Destroy()
		outputVec.Destroy()
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("create session: %w", err)
	}

	return &BundledProvider{
		session:   session,
		tokenizer: tok,
		dims:      dims,
		maxLen:    maxLen,
		inputIDs:  inputIDs,
		inputMask: inputMask,
		outputVec: outputVec,
	}, nil
}

// Embed returns a L2-normalized 384-dim embedding vector for the given text.
func (p *BundledProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("provider is closed")
	}

	// Tokenize and fill input tensors
	ids, mask := p.tokenizer.Tokenize(text)
	copy(p.inputIDs.GetData(), ids)
	copy(p.inputMask.GetData(), mask)

	// Run inference
	if err := p.session.Run(); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}

	// Extract and normalize output
	out := p.outputVec.GetData()
	result := l2Normalize(out[:p.dims])
	return result, nil
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
// Results are returned in the same order as input.
func (p *BundledProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec, err := p.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		results = append(results, vec)
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
func (p *BundledProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	var firstErr error
	if p.session != nil {
		if err := p.session.Destroy(); err != nil {
			firstErr = fmt.Errorf("destroy session: %w", err)
		}
		p.session = nil
	}
	if p.inputIDs != nil {
		p.inputIDs.Destroy()
		p.inputIDs = nil
	}
	if p.inputMask != nil {
		p.inputMask.Destroy()
		p.inputMask = nil
	}
	if p.outputVec != nil {
		p.outputVec.Destroy()
		p.outputVec = nil
	}
	if err := ort.DestroyEnvironment(); err != nil {
		if firstErr == nil {
			firstErr = fmt.Errorf("destroy environment: %w", err)
		}
	}
	return firstErr
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
