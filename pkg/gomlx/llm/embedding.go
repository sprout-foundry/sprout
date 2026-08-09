//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// Embedding is a word embedding with two representations:
//
//   - Full precision: w is [vocab, hidden] (pre-transposed copy wT [hidden,
//     vocab] used for the tied lm_head logits projection).
//   - Quantized: qW is the packed int32 weight [vocab, hidden*bits/32]
//     (PyTorch layout), qScales/qBiases are per-group scale and bias.
//     Lookup gathers the packed rows + scale/bias rows and dequantizes;
//     the logits projection runs mlx_quantized_matmul (as_linear path).
//
// Quantized embeddings appear on mlx-community models (e.g. Qwen3.5 4-bit),
// where the tied lm_head is the quantized embedding itself.
type Embedding struct {
	w  tensor.Array // [vocab, hidden], nil when quantized
	wT tensor.Array // [hidden, vocab] full precision logits weight, nil when quantized

	qW         tensor.Array // packed int32 [vocab, hidden*bits/32]
	qScales    tensor.Array // [vocab, hidden/group_size]
	qBiases    tensor.Array // [vocab, hidden/group_size], optional
	qGroupSize int
	qBits      int
	qMode      string
}

// EmbeddingIsQuantized reports whether the embedding holds packed weights.
func (e *Embedding) EmbeddingIsQuantized() bool { return e.qW != nil }

// EmbeddingShape returns the hidden dimension (the row width after
// dequantization). For full precision it is len(e.w.Shape())-th dim size;
// for quantized it is qScales row count * group size.
func (e *Embedding) EmbeddingShape() int {
	if e.qW != nil {
		return e.qScales.Shape()[1] * e.qGroupSize
	}
	return e.w.Shape()[1]
}

// Lookup gathers rows by token ids and returns [.., ids_shape, hidden].
// ids is typically [1, seqLen]. For quantized embeddings it gathers the
// packed + scale + bias rows and dequantizes them (matches mlx-lm's
// QuantizedEmbedding.__call__: mx.dequantize(weight[x], scales[x], biases[x])).
func (e *Embedding) Lookup(ids tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if e.qW == nil {
		return b.GatherAxis(e.w, ids, 0, []int{1, e.w.Shape()[1]}, s)
	}

	// Gather packed rows: [..., vocab_packed_dim] per gathered index.
	packedDim := e.qW.Shape()[1]
	wRows, err := b.GatherAxis(e.qW, ids, 0, []int{1, packedDim}, s)
	if err != nil {
		return nil, fmt.Errorf("embed gather packed: %w", err)
	}
	defer wRows.Free()

	groupDim := e.qScales.Shape()[1]
	sRows, err := b.GatherAxis(e.qScales, ids, 0, []int{1, groupDim}, s)
	if err != nil {
		return nil, fmt.Errorf("embed gather scales: %w", err)
	}
	defer sRows.Free()

	var bRows tensor.Array
	if e.qBiases != nil {
		bRows, err = b.GatherAxis(e.qBiases, ids, 0, []int{1, groupDim}, s)
		if err != nil {
			return nil, fmt.Errorf("embed gather biases: %w", err)
		}
		defer bRows.Free()
	}

	return b.Dequantize(wRows, sRows, bRows, e.qGroupSize, e.qBits, e.qMode, s)
}

// Logits projects h (last-token hidden state, [1, hidden]) to vocab scores.
// Full precision uses the pre-transposed weight; quantized uses
// mlx_quantized_matmul with transpose=true (the as_linear path in mlx-lm).
func (e *Embedding) Logits(h tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if e.qW == nil {
		return b.MatMul(h, e.wT, s)
	}
	return b.QuantizedMatMul(h, e.qW, e.qScales, e.qBiases, true, e.qGroupSize, e.qBits, e.qMode, s)
}

// Free releases all arrays held by the embedding.
func (e *Embedding) Free() {
	freeArr(e.w)
	freeArr(e.wT)
	freeArr(e.qW)
	freeArr(e.qScales)
	freeArr(e.qBiases)
}

// LoadEmbedding loads the embedding at `name` ("embed_tokens.weight" or a
// quantized triplet "embed_tokens.weight"/".scales"/".biases"). When quant is
// nil or the file has no quantized triplet, it loads full precision (and
// pre-transposes for the logits path). When the file stores a quantized
// triplet, it loads the packed form directly.
//
// `name` may be the full weight key ("...embed_tokens.weight") or the base
// ("...embed_tokens"); the triplet check strips a trailing ".weight".
func LoadEmbedding(sf *SafetensorsFile, name string, b tensor.Backend, s tensor.Stream, quant *QuantConfig) (*Embedding, error) {
	base := strings.TrimSuffix(name, ".weight")

	if quant != nil && sf.Has(base+".scales") {
		w, err := sf.Get(base+".weight", s)
		if err != nil {
			return nil, fmt.Errorf("embedding %s: %w", base, err)
		}
		scales, err := sf.Get(base+".scales", s)
		if err != nil {
			w.Free()
			return nil, fmt.Errorf("embedding %s.scales: %w", base, err)
		}
		var biases tensor.Array
		if sf.Has(base + ".biases") {
			biases, err = sf.Get(base+".biases", s)
			if err != nil {
				w.Free()
				scales.Free()
				return nil, fmt.Errorf("embedding %s.biases: %w", base, err)
			}
		}
		return &Embedding{
			qW:         w,
			qScales:    scales,
			qBiases:    biases,
			qGroupSize: quant.GroupSize,
			qBits:      quant.Bits,
			qMode:      quant.Mode,
		}, nil
	}

	w, err := sf.Get(name, s)
	if err != nil {
		return nil, fmt.Errorf("embedding %s: %w", name, err)
	}
	// Pre-transpose for the logits projection (tied lm_head).
	wT, err := b.Transpose(w, s)
	if err != nil {
		w.Free()
		return nil, fmt.Errorf("transpose embedding %s: %w", name, err)
	}
	if err := wT.Eval(); err != nil {
		w.Free()
		wT.Free()
		return nil, fmt.Errorf("eval embedding %s: %w", name, err)
	}
	return &Embedding{w: w, wT: wT}, nil
}
