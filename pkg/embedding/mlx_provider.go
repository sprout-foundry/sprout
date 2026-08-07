//go:build darwin && arm64 && cgo && mlx

package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// MLXEmbeddingProvider implements EmbeddingProvider using Jina Code v2 via
// Apple's MLX framework (Metal GPU) on Apple Silicon. It implements the
// model's forward pass directly in Go using the pkg/mlx CGO wrapper, loading
// weights from a safetensors file (not ONNX).
//
// Architecture: JinaBertV2 with ALiBi positional encoding, QK-LayerNorm,
// GEGLU FFN, and triple post-norm residuals. See the Python prototype
// (/tmp/sprout/mlx_proto_v3.py) for the reference implementation.
//
// On non-Apple-Silicon platforms, the build-tag-gated stub (mlx_provider_stub.go)
// provides a constructor that returns an error, so callers fall back to the
// ONNX provider.
type MLXEmbeddingProvider struct {
	mu        sync.RWMutex
	stream    *mlx.Stream
	weights   *jinaWeights
	tokenizer *ByteLevelTokenizer
	dims      int
	modelHash string
	closed    bool
	maxSeqLen int
}

// jinaWeights holds all MLX arrays for the 12-layer Jina Code v2 model.
// Weights are loaded once at init and reused for every inference call.
type jinaWeights struct {
	wordEmb      *mlx.Array
	tokEmb       *mlx.Array
	embNormW     *mlx.Array
	embNormB     *mlx.Array
	layers       [numJinaLayers]*jinaLayerWeights
}

type jinaLayerWeights struct {
	qProjW, qProjB     *mlx.Array
	kProjW, kProjB     *mlx.Array
	vProjW, vProjB     *mlx.Array
	outProjW, outProjB *mlx.Array
	qLnW, qLnB         *mlx.Array
	kLnW, kLnB         *mlx.Array
	attnLnW, attnLnB   *mlx.Array
	ln1W, ln1B         *mlx.Array
	ln2W, ln2B         *mlx.Array
	gateUpW            *mlx.Array
	downW, downB       *mlx.Array
}

const (
	numJinaLayers    = 12
	jinaHidden       = 768
	jinaHeads        = 12
	jinaHeadDim      = jinaHidden / jinaHeads
	jinaIntermediate = 3072
	jinaEps          = 1e-12
)

// NewMLXEmbeddingProvider creates a Jina Code v2 provider backed by MLX.
// The model is loaded from a safetensors file at modelPath.
func NewMLXEmbeddingProvider(ctx context.Context, modelPath, tokenizerPath string) (*MLXEmbeddingProvider, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !mlx.Available() {
		return nil, fmt.Errorf("mlx: GPU not available")
	}

	stream, err := mlx.DefaultStream()
	if err != nil {
		return nil, fmt.Errorf("mlx: create stream: %w", err)
	}

	tokenizer, err := NewByteLevelTokenizer(tokenizerPath)
	if err != nil {
		stream.Free()
		return nil, fmt.Errorf("mlx: load tokenizer: %w", err)
	}

	weights, err := loadJinaSafetensors(modelPath, stream)
	if err != nil {
		stream.Free()
		return nil, fmt.Errorf("mlx: load weights: %w", err)
	}

	hash, err := computeFileSHA256(modelPath)
	if err != nil {
		cleanupWeights(weights, stream)
		return nil, fmt.Errorf("mlx: hash model: %w", err)
	}

	p := &MLXEmbeddingProvider{
		stream:    stream,
		weights:   weights,
		tokenizer: tokenizer,
		dims:      jinaHidden,
		modelHash: hash,
		maxSeqLen: 2048,
	}

	log.Printf("mlx: Jina Code v2 loaded on GPU (%d layers, %d-dim)", numJinaLayers, jinaHidden)
	return p, nil
}

func computeFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// Embed returns an L2-normalized, mean-pooled embedding for a single text.
func (p *MLXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, fmt.Errorf("mlx embedding: provider is closed")
	}
	p.mu.RUnlock()

	tokenIDs := p.tokenize(text)
	if len(tokenIDs) == 0 {
		return make([]float32, p.dims), nil
	}

	release, err := acquireInference(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	return p.runInference(ctx, tokenIDs)
}

// EmbedBatch returns L2-normalized embeddings for multiple texts.
func (p *MLXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return nil, fmt.Errorf("mlx embedding: provider is closed")
	}
	p.mu.RUnlock()

	if len(texts) == 0 {
		return nil, nil
	}

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

	release, err := acquireInference(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

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
			return results, fmt.Errorf("mlx embedding: batch [%d:%d]: %w", start, end, err)
		}
		copy(results[start:end], batchVecs)
	}

	return results, nil
}

func (p *MLXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	_ = prefix
	return p.Embed(ctx, text)
}

func (p *MLXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	_ = prefix
	return p.EmbedBatch(ctx, texts)
}

func (p *MLXEmbeddingProvider) tokenize(text string) []int32 {
	tokenIDs := p.tokenizer.Encode(text)
	if p.tokenizer.BOSID() >= 0 {
		tokenIDs = append([]int32{int32(p.tokenizer.BOSID())}, tokenIDs...)
	}
	if p.tokenizer.EOSID() >= 0 {
		tokenIDs = append(tokenIDs, int32(p.tokenizer.EOSID()))
	}
	if len(tokenIDs) > p.maxSeqLen {
		tokenIDs = tokenIDs[:p.maxSeqLen]
	}
	return tokenIDs
}

func (p *MLXEmbeddingProvider) runInference(ctx context.Context, tokenIDs []int32) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Create a fresh GPU stream on this OS thread — MLX streams are thread-local.
	s, err := mlx.DefaultGPUStream()
	if err != nil {
		return nil, fmt.Errorf("get GPU stream: %w", err)
	}
	defer s.Free()
	bsz := 1
	seq := len(tokenIDs)

	idData := make([]int64, seq)
	for i, id := range tokenIDs {
		idData[i] = int64(id)
	}
	idsArr, err := mlx.NewArrayFromInt64(idData, []int{bsz, seq})
	if err != nil {
		return nil, fmt.Errorf("create ids tensor: %w", err)
	}
	defer idsArr.Free()

	hidden, err := p.forward(idsArr, seq, nil, s)
	if err != nil {
		return nil, err
	}
	defer hidden.Free()

	maskData := make([]float32, seq)
	for i := range maskData {
		maskData[i] = 1.0
	}
	maskArr, err := mlx.NewArrayFromFloat32(maskData, []int{bsz, seq})
	if err != nil {
		return nil, fmt.Errorf("create mask: %w", err)
	}
	defer maskArr.Free()

	pooled, err := meanPoolNorm(hidden, maskArr, seq, p.dims, s)
	if err != nil {
		return nil, err
	}
	defer pooled.Free()

	// Synchronize before reading data — MLX ops are lazy and queued on the
	// stream; reading data on a different goroutine without sync causes
	// "no Stream in current thread" errors.
	if err := s.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}
	return pooled.Float32Data()
}

func (p *MLXEmbeddingProvider) runInferenceBatch(ctx context.Context, seqs [][]int32, maxLen int) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, err := mlx.DefaultGPUStream()
	if err != nil {
		return nil, fmt.Errorf("get GPU stream: %w", err)
	}
	defer s.Free()
	batchSize := len(seqs)

	idData := make([]int64, batchSize*maxLen)
	for i, seq := range seqs {
		for j, id := range seq {
			if j >= maxLen {
				break
			}
			idData[i*maxLen+j] = int64(id)
		}
	}
	idsArr, err := mlx.NewArrayFromInt64(idData, []int{batchSize, maxLen})
	if err != nil {
		return nil, fmt.Errorf("create ids tensor: %w", err)
	}
	defer idsArr.Free()

	// Build additive attention mask for padding: [batch, 1, 1, maxLen]
	// Real tokens get 0, padding positions get -1e9.
	attnMaskData := make([]float32, batchSize*maxLen)
	for i, seq := range seqs {
		ln := len(seq)
		if ln > maxLen {
			ln = maxLen
		}
		for j := 0; j < maxLen; j++ {
			if j >= ln {
				attnMaskData[i*maxLen+j] = -1e9
			}
		}
	}
	attnMask, err := mlx.NewArrayFromFloat32(attnMaskData, []int{batchSize, 1, 1, maxLen})
	if err != nil {
		return nil, fmt.Errorf("create attn mask: %w", err)
	}
	defer attnMask.Free()

	hidden, err := p.forward(idsArr, maxLen, attnMask, s)
	if err != nil {
		return nil, err
	}
	defer hidden.Free()

	maskData := make([]float32, batchSize*maxLen)
	for i, seq := range seqs {
		ln := len(seq)
		if ln > maxLen {
			ln = maxLen
		}
		for j := 0; j < ln; j++ {
			maskData[i*maxLen+j] = 1.0
		}
	}
	maskArr, err := mlx.NewArrayFromFloat32(maskData, []int{batchSize, maxLen})
	if err != nil {
		return nil, fmt.Errorf("create mask: %w", err)
	}
	defer maskArr.Free()

	pooled, err := meanPoolNorm(hidden, maskArr, maxLen, p.dims, s)
	if err != nil {
		return nil, err
	}
	defer pooled.Free()

	// Synchronize before reading data
	if err := s.Synchronize(); err != nil {
		return nil, fmt.Errorf("synchronize: %w", err)
	}
	pooledData, err := pooled.Float32Data()
	if err != nil {
		return nil, err
	}

	results := make([][]float32, batchSize)
	for i := 0; i < batchSize; i++ {
		results[i] = make([]float32, p.dims)
		copy(results[i], pooledData[i*p.dims:(i+1)*p.dims])
	}
	return results, nil
}

func (p *MLXEmbeddingProvider) Dimensions() int { return p.dims }

func (p *MLXEmbeddingProvider) Name() string { return "jina-code-v2-mlx" }

func (p *MLXEmbeddingProvider) ModelHash() string { return p.modelHash }

func (p *MLXEmbeddingProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true
	cleanupWeights(p.weights, p.stream)
	if p.stream != nil {
		p.stream.Free()
	}
	return nil
}

func cleanupWeights(w *jinaWeights, s *mlx.Stream) {
	if w == nil {
		return
	}
	freeArr(w.wordEmb)
	freeArr(w.tokEmb)
	freeArr(w.embNormW)
	freeArr(w.embNormB)
	for _, l := range w.layers {
		if l != nil {
			freeArr(l.qProjW)
			freeArr(l.qProjB)
			freeArr(l.kProjW)
			freeArr(l.kProjB)
			freeArr(l.vProjW)
			freeArr(l.vProjB)
			freeArr(l.outProjW)
			freeArr(l.outProjB)
			freeArr(l.qLnW)
			freeArr(l.qLnB)
			freeArr(l.kLnW)
			freeArr(l.kLnB)
			freeArr(l.attnLnW)
			freeArr(l.attnLnB)
			freeArr(l.ln1W)
			freeArr(l.ln1B)
			freeArr(l.ln2W)
			freeArr(l.ln2B)
			freeArr(l.gateUpW)
			freeArr(l.downW)
			freeArr(l.downB)
		}
	}
}

func freeArr(a *mlx.Array) {
	if a != nil {
		a.Free()
	}
}
