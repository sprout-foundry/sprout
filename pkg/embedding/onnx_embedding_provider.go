//go:build !wasm && cgo

package embedding

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"
	goruntime "runtime"
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
	dims      int // output dimensions (after MRL truncation)
	fullDims  int // model's native output dimensions (e.g., 768)
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
// fullDims is the model's native output dimension (used for tensor allocation).
func NewONNXEmbeddingProvider(ctx context.Context, runtime *ONNXRuntime, modelPath, tokenizerPath string, dims, fullDims int) (*ONNXEmbeddingProvider, error) {
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
	//
	// Intra-op parallelism unlocks the batched-embedding speedup: with the
	// previous IntraOpNumThreads=1 setting, ORT processes each matmul on
	// one core, so feeding a [batch, seq] tensor takes the same wallclock
	// as N sequential per-row calls. The bench harness (default ORT
	// threading on M1+) showed ~1.7× batched-vs-single speedup; we get
	// roughly the same here. Cap at min(NumCPU, 4) so we don't starve
	// other sprout work (the chat loop, file watchers, etc.) on a small
	// machine, and to keep this consistent across CI runners.
	intraThreads := goruntime.NumCPU()
	if intraThreads > 4 {
		intraThreads = 4
	}
	if intraThreads < 1 {
		intraThreads = 1
	}
	session, err := runtime.NewDynamicSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"sentence_embedding"},
		SessionOption{
			IntraOpNumThreads: intraThreads,
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

	if fullDims <= 0 {
		session.Destroy()
		return nil, fmt.Errorf("onnx embedding: fullDims must be positive, got %d", fullDims)
	}
	if dims <= 0 || dims > fullDims {
		session.Destroy()
		return nil, fmt.Errorf("onnx embedding: dims must be between 1 and %d, got %d", fullDims, dims)
	}

	return &ONNXEmbeddingProvider{
		runtime:   runtime,
		session:   session,
		tokenizer: tokenizer,
		dims:      dims,
		fullDims:  fullDims,
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
		// Empty text produces a zero vector with undefined similarity.
		// Cosine similarity with a zero vector is 0, so it never matches
		// above any meaningful threshold, but callers should ideally
		// validate input before embedding.
		debugLogf("onnx embedding: empty text produced zero-token embedding")
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
// Tokenizes all texts up-front, then runs ONNX inference in batches of
// defaultBatchChunkSize through a single padded [batch, seq] tensor —
// much faster than the previous per-text loop (verified ~6× speedup
// during codebase indexing on M1+, since memory bandwidth amortizes
// across the chunk).
func (p *ONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	return p.embedBatchInternal(ctx, texts, "")
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
// Same batched ONNX execution as EmbedBatch — see that method's doc.
func (p *ONNXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	return p.embedBatchInternal(ctx, texts, prefix)
}

// defaultBatchChunkSize bounds memory per ORT call. With 32 rows at
// the 2048-token max seq, the input tensors are ~512 KB each (int64 ×
// 32 × 2048) and the output is ~96 KB (float32 × 32 × 768). The
// embedding model itself (~168 MB Q4f16 weights) stays loaded once;
// per-call allocations are negligible. Throughput scales near-linearly
// up to ~16–32 rows, after which memory bandwidth dominates and bigger
// chunks stop helping.
const defaultBatchChunkSize = 32

// embedBatchInternal is the shared body for EmbedBatch and
// EmbedBatchWithPrefix. Tokenizes every input up front, partitions
// into fixed-size chunks, and runs each chunk through ORT as a single
// padded [batch, seq_len] tensor.
//
// Empty-string inputs (tokenize to zero tokens) get a zero embedding
// without an ORT call — matches the prior per-text behavior and keeps
// the chunk batches packed only with rows that need work.
func (p *ONNXEmbeddingProvider) embedBatchInternal(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
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

	// Pre-tokenize every row so the chunk loop can size each batch by
	// the longest non-empty row in that chunk (no point padding to the
	// global max if a chunk's contents are all short).
	tokIDs := make([][]int32, len(texts))
	results := make([][]float32, len(texts))
	for i, text := range texts {
		if prefix != "" {
			text = prefix + text
		}
		ids := p.tokenizer.EncodeWithBOSAndEOS(text)
		if len(ids) > p.maxSeqLen {
			ids = ids[:p.maxSeqLen]
		}
		tokIDs[i] = ids
		if len(ids) == 0 {
			// Empty input → zero vector. Mirrors single-call Embed().
			results[i] = make([]float32, p.dims)
		}
	}

	for start := 0; start < len(texts); start += defaultBatchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + defaultBatchChunkSize
		if end > len(texts) {
			end = len(texts)
		}

		// Collect indices of rows in this chunk that actually need
		// inference (skip ones we already filled with the zero vector).
		nonEmptyIdx := make([]int, 0, end-start)
		maxLen := 0
		for i := start; i < end; i++ {
			if len(tokIDs[i]) == 0 {
				continue
			}
			nonEmptyIdx = append(nonEmptyIdx, i)
			if len(tokIDs[i]) > maxLen {
				maxLen = len(tokIDs[i])
			}
		}
		if len(nonEmptyIdx) == 0 {
			continue
		}

		batchSize := int64(len(nonEmptyIdx))
		seqLen := int64(maxLen)

		// Pack input_ids + attention_mask as [batch * seq] row-major.
		// Padding positions get id=0 and mask=0; the attention mask
		// lets the model ignore them, so the per-row output matches
		// what unpadded inference would have produced.
		inputIDs := make([]int64, batchSize*seqLen)
		attnMask := make([]int64, batchSize*seqLen)
		for bi, idx := range nonEmptyIdx {
			ids := tokIDs[idx]
			row := int64(bi) * seqLen
			for j, id := range ids {
				inputIDs[row+int64(j)] = int64(id)
				attnMask[row+int64(j)] = 1
			}
		}

		batchVecs, err := p.runInferenceBatch(inputIDs, attnMask, batchSize, seqLen)
		if err != nil {
			return nil, fmt.Errorf("batch embed[%d:%d]: %w", start, end, err)
		}
		for bi, idx := range nonEmptyIdx {
			results[idx] = batchVecs[bi]
		}
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
	fullDim := int64(p.fullDims)

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

// runInferenceBatch runs ONNX inference on a packed [batch, seq_len]
// input and returns one MRL-truncated, L2-normalized embedding per
// row. The caller is responsible for padding inputIDs/attentionMask
// to a uniform seq_len; rows whose attention mask is 0 for trailing
// positions get the model's "ignore this token" behavior so the
// per-row output matches what unpadded inference would have produced.
//
// Must be called with p.mu.RLock held.
func (p *ONNXEmbeddingProvider) runInferenceBatch(inputIDs []int64, attentionMask []int64, batchSize, seqLen int64) ([][]float32, error) {
	fullDim := int64(p.fullDims)

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
		return nil, fmt.Errorf("run batched inference: %w", err)
	}

	pooled := outputTensor.GetData()
	if int64(len(pooled)) < batchSize*fullDim {
		return nil, fmt.Errorf("sentence_embedding returned %d floats, expected at least %d", len(pooled), batchSize*fullDim)
	}

	results := make([][]float32, batchSize)
	for i := int64(0); i < batchSize; i++ {
		row := pooled[i*fullDim : i*fullDim+int64(p.dims)]
		embedding := make([]float32, p.dims)
		copy(embedding, row)

		// Matryoshka L2-normalize per row.
		var norm float32
		for _, v := range embedding {
			norm += v * v
		}
		if norm > 1e-9 {
			inv := float32(1.0 / math.Sqrt(float64(norm)))
			for j := range embedding {
				embedding[j] *= inv
			}
		}
		results[i] = embedding
	}
	return results, nil
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
