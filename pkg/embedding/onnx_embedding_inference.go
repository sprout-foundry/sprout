//go:build !wasm && cgo

package embedding

// onnx_embedding_inference.go — ONNX execution internals, split out of
// onnx_embedding_provider.go. runInference / runInferenceBatch pack
// input_ids + attention_mask into ORT tensors, run the session, and apply
// Matryoshka truncation + per-row L2 normalization; runWithOptions wraps
// the session call with ctx-aware termination. Both inference helpers
// require p.mu.RLock held by the caller.
import (
	"context"
	"fmt"
	"math"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// runInference runs ONNX inference and returns the (MRL-truncated, L2-normalized)
// sentence embedding. The model's sentence_embedding output is already
// mean-pooled internally; we slice the first p.dims components for Matryoshka
// representation learning truncation, then L2-normalize.
//
// Must be called with p.mu.RLock held.
func (p *ONNXEmbeddingProvider) runInference(ctx context.Context, inputIDs []int64, attentionMask []int64) ([]float32, error) {
	release, err := acquireInference(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

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

	if err := p.runWithOptions(ctx,
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
// runWithOptions wraps session.RunWithOptions with ctx-aware termination.
//
// See runSessionWithOptions in onnx_run_options.go for the rationale and the
// watchdog implementation. Kept as a thin pass-through so the call sites in
// this file read as `p.runWithOptions(ctx, ...)` rather than handing the
// session pointer through every call.
func (p *ONNXEmbeddingProvider) runWithOptions(
	ctx context.Context,
	inputs, outputs []onnxruntime.Value,
) error {
	return runSessionWithOptions(ctx, p.session, inputs, outputs)
}

// Must be called with p.mu.RLock held.
func (p *ONNXEmbeddingProvider) runInferenceBatch(ctx context.Context, inputIDs []int64, attentionMask []int64, batchSize, seqLen int64) ([][]float32, error) {
	// Gate before allocating: the tensors and the Run's activations are the
	// memory this bounds, so the permit has to cover both.
	release, err := acquireInference(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

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

	if err := p.runWithOptions(ctx,
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
