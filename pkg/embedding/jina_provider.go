//go:build !wasm && cgo

package embedding

import (
	"context"
	"fmt"
	"math"
	"sync"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// JinaONNXEmbeddingProvider implements EmbeddingProvider using Jina Code v2
// (jinaai/jina-embeddings-v2-base-code) via ONNX Runtime.
//
// Unlike EmbeddingGemma (which exports a pre-pooled sentence_embedding output),
// Jina's ONNX graph outputs last_hidden_state [batch, seq_len, 768]. Mean
// pooling with the attention mask is done in Go, matching Jina's reference
// implementation (sentence-transformers mean pooling).
//
// Jina is symmetric: it does NOT use task prefixes. EmbedWithPrefix and
// EmbedBatchWithPrefix delegate to Embed / EmbedBatch, ignoring the prefix —
// the query and document are embedded identically. This is the model's design,
// not a shortcut: CodeRankEmbed/Jina-Code are trained for symmetric code
// retrieval, while EmbeddingGemma uses asymmetric task-specific prefixes.
//
// The ByteLevelTokenizer (GPT-2 BPE with bytes_to_unicode mapping) is required
// for correct tokenization — the GemmaTokenizer (SentencePiece ▁ normalization)
// produces wrong token IDs for Jina inputs.
type JinaONNXEmbeddingProvider struct {
	mu        sync.RWMutex
	runtime   *ONNXRuntime
	session   *onnxruntime.DynamicAdvancedSession
	tokenizer *ByteLevelTokenizer
	dims      int // output dimensions (768)
	modelHash string
	closed    bool

	bosID int32
	eosID int32

	maxSeqLen int
}

// NewJinaONNXEmbeddingProvider creates a Jina Code v2 embedding provider.
// The runtime must already be initialized. modelPath points to the .onnx
// model file, tokenizerPath points to tokenizer.json.
func NewJinaONNXEmbeddingProvider(ctx context.Context, runtime *ONNXRuntime, modelPath, tokenizerPath string) (*JinaONNXEmbeddingProvider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tokenizer, err := NewByteLevelTokenizer(tokenizerPath)
	if err != nil {
		return nil, fmt.Errorf("jina embedding: load tokenizer: %w", err)
	}

	intraThreads := 4
	session, err := runtime.NewDynamicSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"last_hidden_state"},
		SessionOption{IntraOpNumThreads: intraThreads},
	)
	if err != nil {
		return nil, fmt.Errorf("jina embedding: create session: %w", err)
	}

	hash, err := fileSHA256(modelPath)
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("jina embedding: hash model: %w", err)
	}

	return &JinaONNXEmbeddingProvider{
		runtime:   runtime,
		session:   session,
		tokenizer: tokenizer,
		dims:      768,
		modelHash: hash,
		bosID:     tokenizer.BOSID(),
		eosID:     tokenizer.EOSID(),
		maxSeqLen: 2048,
	}, nil
}

// Embed returns an L2-normalized, mean-pooled 768-dim embedding for text.
func (p *JinaONNXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("jina embedding: provider is closed")
	}

	tokenIDs := p.tokenize(text)
	if len(tokenIDs) == 0 {
		return make([]float32, p.dims), nil
	}

	return p.runInference(ctx, tokenIDs)
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
// Sequences are right-padded to the longest in the batch; attention mask
// ensures padded positions don't affect mean pooling.
func (p *JinaONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, fmt.Errorf("jina embedding: provider is closed")
	}

	if len(texts) == 0 {
		return nil, nil
	}

	// Tokenize all texts.
	seqs := make([][]int32, len(texts))
	maxLen := 0
	for i, text := range texts {
		seqs[i] = p.tokenize(text)
		if len(seqs[i]) > maxLen {
			maxLen = len(seqs[i])
		}
	}
	if maxLen == 0 {
		results := make([][]float32, len(texts))
		for i := range results {
			results[i] = make([]float32, p.dims)
		}
		return results, nil
	}
	if maxLen > p.maxSeqLen {
		maxLen = p.maxSeqLen
	}

	results := make([][]float32, len(texts))

	// Process in chunks of defaultBatchChunkSize, matching the Gemma provider's
	// attention-budget approach: batch rows × seqLen² is the memory cost.
	chunkSize := defaultBatchChunkSize
	for start := 0; start < len(texts); start += chunkSize {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		end := start + chunkSize
		if end > len(texts) {
			end = len(texts)
		}

		batchVecs, err := p.runInferenceBatch(ctx, seqs[start:end], maxLen)
		if err != nil {
			return results, fmt.Errorf("jina embedding: batch [%d:%d]: %w", start, end, err)
		}
		copy(results[start:end], batchVecs)
	}

	return results, nil
}

// EmbedWithPrefix ignores the prefix (Jina is symmetric) and delegates to Embed.
func (p *JinaONNXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	_ = prefix
	return p.Embed(ctx, text)
}

// EmbedBatchWithPrefix ignores the prefix (Jina is symmetric) and delegates to EmbedBatch.
func (p *JinaONNXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	_ = prefix
	return p.EmbedBatch(ctx, texts)
}

// tokenize encodes text with BOS/EOS wrapping and truncation.
func (p *JinaONNXEmbeddingProvider) tokenize(text string) []int32 {
	tokenIDs := p.tokenizer.Encode(text)
	if p.bosID >= 0 {
		tokenIDs = append([]int32{p.bosID}, tokenIDs...)
	}
	if p.eosID >= 0 {
		tokenIDs = append(tokenIDs, p.eosID)
	}
	if len(tokenIDs) > p.maxSeqLen {
		tokenIDs = tokenIDs[:p.maxSeqLen]
	}
	return tokenIDs
}

// runInference runs a single-sequence ONNX inference and returns the mean-pooled,
// L2-normalized embedding.
func (p *JinaONNXEmbeddingProvider) runInference(ctx context.Context, tokenIDs []int32) ([]float32, error) {
	release, err := acquireInference(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	batchSize := int64(1)
	seqLen := int64(len(tokenIDs))
	hiddenDim := int64(p.dims)

	inputIDs := make([]int64, len(tokenIDs))
	attentionMask := make([]int64, len(tokenIDs))
	for i, id := range tokenIDs {
		inputIDs[i] = int64(id)
		attentionMask[i] = 1
	}

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

	outputTensor, err := onnxruntime.NewEmptyTensor[float32](onnxruntime.NewShape(batchSize, seqLen, hiddenDim))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	if err := runSessionWithOptions(ctx, p.session,
		[]onnxruntime.Value{inputIDsTensor, attnMaskTensor},
		[]onnxruntime.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("run inference: %w", err)
	}

	hidden := outputTensor.GetData()
	return meanPoolAndNormalize(hidden, int(seqLen), int(hiddenDim)), nil
}

// runInferenceBatch runs batched ONNX inference on padded sequences and returns
// mean-pooled, L2-normalized embeddings per row.
func (p *JinaONNXEmbeddingProvider) runInferenceBatch(ctx context.Context, seqs [][]int32, maxLen int) ([][]float32, error) {
	release, err := acquireInference(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	batchSize := int64(len(seqs))
	seqLen := int64(maxLen)
	hiddenDim := int64(p.dims)

	inputIDs := make([]int64, batchSize*seqLen)
	attentionMask := make([]int64, batchSize*seqLen)

	for i, seq := range seqs {
		for j, id := range seq {
			if j >= maxLen {
				break
			}
			inputIDs[i*int(seqLen)+j] = int64(id)
			attentionMask[i*int(seqLen)+j] = 1
		}
	}

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

	outputTensor, err := onnxruntime.NewEmptyTensor[float32](onnxruntime.NewShape(batchSize, seqLen, hiddenDim))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	if err := runSessionWithOptions(ctx, p.session,
		[]onnxruntime.Value{inputIDsTensor, attnMaskTensor},
		[]onnxruntime.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("run batched inference: %w", err)
	}

	hidden := outputTensor.GetData()
	results := make([][]float32, batchSize)
	for i := int64(0); i < batchSize; i++ {
		offset := i * seqLen * hiddenDim
		row := hidden[offset : offset+seqLen*hiddenDim]
		// Use the actual (unpadded) sequence length for correct mean pooling.
		actualLen := len(seqs[i])
		if actualLen > maxLen {
			actualLen = maxLen
		}
		if actualLen == 0 {
			results[i] = make([]float32, hiddenDim)
			continue
		}
		results[i] = meanPoolAndNormalize(row, actualLen, int(hiddenDim))
	}

	return results, nil
}

// meanPoolAndNormalize computes attention-masked mean pooling over the sequence
// dimension, then L2-normalizes the result. hidden is a flat [seqLen, hiddenDim]
// slice from the last_hidden_state output.
func meanPoolAndNormalize(hidden []float32, seqLen, hiddenDim int) []float32 {
	pooled := make([]float32, hiddenDim)
	for i := 0; i < seqLen; i++ {
		offset := i * hiddenDim
		for j := 0; j < hiddenDim; j++ {
			pooled[j] += hidden[offset+j]
		}
	}
	invSeq := float32(1.0 / float64(seqLen))
	for j := range pooled {
		pooled[j] *= invSeq
	}

	var norm float32
	for _, v := range pooled {
		norm += v * v
	}
	if norm > 1e-9 {
		inv := float32(1.0 / math.Sqrt(float64(norm)))
		for j := range pooled {
			pooled[j] *= inv
		}
	}
	return pooled
}

// Dimensions returns 768.
func (p *JinaONNXEmbeddingProvider) Dimensions() int { return p.dims }

// Name returns the model identifier.
func (p *JinaONNXEmbeddingProvider) Name() string { return "jina-code-v2" }

// ModelHash returns the SHA-256 of the model file.
func (p *JinaONNXEmbeddingProvider) ModelHash() string { return p.modelHash }

// Close releases the ONNX session.
func (p *JinaONNXEmbeddingProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true
	p.session.Destroy()
	return nil
}
