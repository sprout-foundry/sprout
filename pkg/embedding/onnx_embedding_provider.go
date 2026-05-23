//go:build !wasm && cgo

package embedding

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	"sync"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// EmbeddingGemma prompt prefixes for task-specific embedding.
// These are prepended to text before tokenization to guide the model
// toward the right embedding space for the task.
const (
	// QueryPrefix is used for search/query embeddings.
	QueryPrefix = "task: search result | query: "
	// DocumentPrefix is used for document/content embeddings.
	DocumentPrefix = "title: none | text: "
	// CodeQueryPrefix is used for code-specific search queries.
	CodeQueryPrefix = "task: code retrieval | query: "
)

// EmbedOptions holds optional parameters for embedding operations.
type EmbedOptions struct {
	// Prefix is prepended to each text before tokenization.
	// This is used for task-specific prompting (e.g., query vs document prefixes).
	Prefix string
}

// ONNXEmbeddingProvider implements EmbeddingProvider using EmbeddingGemma via ONNX Runtime.
//
// It loads a Gemma-based embedding model, tokenizes input text with a BPE
// tokenizer, runs ONNX inference, and extracts mean-pooled, L2-normalized
// embeddings with optional MRL dimension truncation.
type ONNXEmbeddingProvider struct {
	mu        sync.RWMutex
	runtime   *ONNXRuntime
	session   *onnxruntime.DynamicAdvancedSession
	tokenizer *GemmaTokenizer
	dims      int           // output dimensions (after MRL truncation)
	modelHash string
	closed    bool

	// Max sequence length for the model (EmbeddingGemma supports 2048 tokens).
	maxSeqLen int
}

// NewONNXEmbeddingProvider creates an embedding provider from ONNX model files.
//
// The runtime must already be initialized. modelPath points to the .onnx model
// file, tokenizerPath points to tokenizer.json. dims specifies the output
// dimensionality (e.g., 256 for MRL truncation from 768, or 768 for full).
func NewONNXEmbeddingProvider(ctx context.Context, runtime *ONNXRuntime, modelPath, tokenizerPath string, dims int) (*ONNXEmbeddingProvider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Load tokenizer.
	tokenizer, err := NewGemmaTokenizer(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("onnx embedding: load tokenizer: %w", err)
	}

	// Create inference session. EmbeddingGemma's ONNX export exposes two
	// inputs (input_ids, attention_mask) and a pre-pooled sentence_embedding
	// output. The yalue session requires explicit names; nil would make Run()
	// fail with "session specified 0 input names".
	session, err := runtime.NewDynamicSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"sentence_embedding"},
		SessionOption{
			IntraOpNumThreads: 1, // Single thread is fast enough for embeddings.
		})
	if err != nil {
		return nil, fmt.Errorf("onnx embedding: create session: %w", err)
	}

	// Compute model hash from file for store validation.
	hash, err := fileSHA256(modelPath)
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("onnx embedding: hash model: %w", err)
	}

	if dims <= 0 || dims > 768 {
		session.Destroy()
		return nil, fmt.Errorf("onnx embedding: dims must be between 1 and 768, got %d", dims)
	}

	return &ONNXEmbeddingProvider{
		runtime:   runtime,
		session:   session,
		tokenizer: tokenizer,
		dims:      dims,
		modelHash: hash,
		maxSeqLen: 2048,
	}, nil
}

// Embed returns a L2-normalized embedding vector for the given text.
func (p *ONNXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("onnx embedding: provider is closed")
	}

	// Tokenize with BOS and EOS markers (EmbeddingGemma expects this).
	tokenIDs := p.tokenizer.EncodeWithBOSAndEOS(text)

	// Truncate to max sequence length if needed.
	if len(tokenIDs) > p.maxSeqLen {
		tokenIDs = tokenIDs[:p.maxSeqLen]
	}

	if len(tokenIDs) == 0 {
		return make([]float32, p.dims), nil
	}

	seqLen := int64(len(tokenIDs))

	// Convert to int64 for ONNX input.
	inputIDs := make([]int64, seqLen)
	attentionMask := make([]int64, seqLen)
	for i, id := range tokenIDs {
		inputIDs[i] = int64(id)
		attentionMask[i] = 1
	}

	// Run inference.
	vec, err := p.runInference(inputIDs, attentionMask)
	if err != nil {
		return nil, err
	}

	return vec, nil
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
// Each text is processed individually (no batching through ONNX) to avoid
// OOM issues with large context windows on CPU.
func (p *ONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("onnx embedding: provider is closed")
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Tokenize with BOS and EOS.
		tokenIDs := p.tokenizer.EncodeWithBOSAndEOS(text)
		if len(tokenIDs) > p.maxSeqLen {
			tokenIDs = tokenIDs[:p.maxSeqLen]
		}

		if len(tokenIDs) == 0 {
			results[i] = make([]float32, p.dims)
			continue
		}

		seqLen := int64(len(tokenIDs))
		inputIDs := make([]int64, seqLen)
		attentionMask := make([]int64, seqLen)
		for j, id := range tokenIDs {
			inputIDs[j] = int64(id)
			attentionMask[j] = 1
		}

		vec, err := p.runInference(inputIDs, attentionMask)
		if err != nil {
			return nil, fmt.Errorf("batch embed[%d]: %w", i, err)
		}
		results[i] = vec
	}

	return results, nil
}

// EmbedWithPrefix returns a L2-normalized embedding vector for the given text
// with the specified prefix prepended before tokenization.
func (p *ONNXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("onnx embedding: provider is closed")
	}

	// Prepend prefix to text before tokenization.
	if prefix != "" {
		text = prefix + text
	}

	// Tokenize with BOS and EOS markers (EmbeddingGemma expects this).
	tokenIDs := p.tokenizer.EncodeWithBOSAndEOS(text)

	// Truncate to max sequence length if needed.
	if len(tokenIDs) > p.maxSeqLen {
		tokenIDs = tokenIDs[:p.maxSeqLen]
	}

	if len(tokenIDs) == 0 {
		return make([]float32, p.dims), nil
	}

	seqLen := int64(len(tokenIDs))

	// Convert to int64 for ONNX input.
	inputIDs := make([]int64, seqLen)
	attentionMask := make([]int64, seqLen)
	for i, id := range tokenIDs {
		inputIDs[i] = int64(id)
		attentionMask[i] = 1
	}

	// Run inference.
	vec, err := p.runInference(inputIDs, attentionMask)
	if err != nil {
		return nil, err
	}

	return vec, nil
}

// EmbedBatchWithPrefix returns L2-normalized embeddings for multiple texts
// with the specified prefix prepended to each text before tokenization.
// Each text is processed individually (no batching through ONNX) to avoid
// OOM issues with large context windows on CPU.
func (p *ONNXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("onnx embedding: provider is closed")
	}

	results := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Prepend prefix to text before tokenization.
		if prefix != "" {
			text = prefix + text
		}

		// Tokenize with BOS and EOS.
		tokenIDs := p.tokenizer.EncodeWithBOSAndEOS(text)
		if len(tokenIDs) > p.maxSeqLen {
			tokenIDs = tokenIDs[:p.maxSeqLen]
		}

		if len(tokenIDs) == 0 {
			results[i] = make([]float32, p.dims)
			continue
		}

		seqLen := int64(len(tokenIDs))
		inputIDs := make([]int64, seqLen)
		attentionMask := make([]int64, seqLen)
		for j, id := range tokenIDs {
			inputIDs[j] = int64(id)
			attentionMask[j] = 1
		}

		vec, err := p.runInference(inputIDs, attentionMask)
		if err != nil {
			return nil, fmt.Errorf("batch embed[%d]: %w", i, err)
		}
		results[i] = vec
	}

	return results, nil
}

// runInference runs ONNX inference and returns the (MRL-truncated, L2-normalized)
// sentence embedding. The model's sentence_embedding output is already
// mean-pooled internally; we slice the first p.dims components for Matryoshka
// representation learning truncation, then L2-normalize.
//
// Must be called with p.mu.RLock held.
func (p *ONNXEmbeddingProvider) runInference(inputIDs []int64, attentionMask []int64) ([]float32, error) {
	batchSize := int64(1)
	seqLen := int64(len(inputIDs))
	const fullDim = int64(768) // EmbeddingGemma sentence_embedding dimension

	inputIDsTensor, err := onnxruntime.NewTensor(onnxruntime.NewShape(batchSize, seqLen), inputIDs)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDsTensor.Destroy()

	attnMaskTensor, err := onnxruntime.NewTensor(onnxruntime.NewShape(batchSize, seqLen), attentionMask)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attnMaskTensor.Destroy()

	outputTensor, err := onnxruntime.NewEmptyTensor[float32](onnxruntime.NewShape(batchSize, fullDim))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	if err := p.session.Run(
		[]onnxruntime.Value{inputIDsTensor, attnMaskTensor},
		[]onnxruntime.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	pooled := outputTensor.GetData()
	if len(pooled) < p.dims {
		return nil, fmt.Errorf("sentence_embedding returned %d floats, expected at least %d", len(pooled), p.dims)
	}

	// Matryoshka truncation: keep the first p.dims components, then L2-normalize.
	embedding := make([]float32, p.dims)
	copy(embedding, pooled[:p.dims])
	var norm float32
	for _, v := range embedding {
		norm += v * v
	}
	if norm > 1e-9 {
		inv := float32(1.0 / math.Sqrt(float64(norm)))
		for i := range embedding {
			embedding[i] *= inv
		}
	}
	return embedding, nil
}

// Dimensions returns the dimensionality of vectors produced by this provider.
func (p *ONNXEmbeddingProvider) Dimensions() int {
	return p.dims
}

// Name returns a human-readable identifier for the provider.
func (p *ONNXEmbeddingProvider) Name() string {
	return fmt.Sprintf("onnx-embeddinggemma-300m-%dd", p.dims)
}

// ModelHash returns a SHA-256 hex digest of the model file.
func (p *ONNXEmbeddingProvider) ModelHash() string {
	return p.modelHash
}

// Close releases the ONNX session and associated resources.
func (p *ONNXEmbeddingProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true

	if p.session != nil {
		p.session.Destroy()
		p.session = nil
	}

	return nil
}

// fileSHA256 computes the SHA-256 hex digest of a file.
func fileSHA256(path string) (string, error) {
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
