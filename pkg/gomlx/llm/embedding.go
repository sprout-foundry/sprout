//go:build darwin && arm64 && cgo

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
//
// For non-standard bit widths (e.g. 5-bit) where MLX's dequantize rejects
// the gathered rows, we dequantize the full table at load time instead
// (see LoadEmbedding's fallback path).
func (e *Embedding) Lookup(ids tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if e.qW == nil {
		// Full precision path (also used when 5-bit embedding was
		// pre-dequantized at load time).
		if e.w != nil {
			return b.GatherAxis(e.w, ids, 0, []int{1, e.w.Shape()[1]}, s)
		}
	}
	if e.w != nil {
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

		// For bit widths where MLX's per-row dequantize fails (e.g. 5-bit
		// affine), dequantize the full embedding table now via
		// QuantizedMatMul with an identity matrix. This trades memory
		// for a working embedding lookup. The logits path still uses the
		// quantized weights via QuantizedMatMul for speed.
		actualBits := inferQuantBits(w.Shape(), scales.Shape(), quant.GroupSize, quant.Bits)
		var fullPrecW tensor.Array
		if actualBits != 2 && actualBits != 3 && actualBits != 4 && actualBits != 6 && actualBits != 8 {
			fullPrecW, err = dequantizeFullTable(w, scales, biases, actualBits, quant.GroupSize, quant.Mode, b, s)
			if err != nil {
				w.Free()
				scales.Free()
				if biases != nil {
					biases.Free()
				}
				return nil, fmt.Errorf("embedding %s full dequantize: %w", base, err)
			}
		}

		return &Embedding{
			qW:         w,
			qScales:    scales,
			qBiases:    biases,
			qGroupSize: quant.GroupSize,
			qBits:      actualBits,
			qMode:      quant.Mode,
			w:          fullPrecW,
		}, nil
	}

	if quant != nil && !sf.Has(base+".scales") {
		// Quantize at load time (GO_QUANTIZE path). The tied lm_head logits
		// projection uses the embedding, so quantizing here also speeds up
		// the logits matmul.
		w, err := sf.Get(name, s)
		if err != nil {
			return nil, fmt.Errorf("embedding %s: %w", base, err)
		}
		defer w.Free()
		parts, err := b.Quantize(w, quant.GroupSize, quant.Bits, quant.Mode, s)
		if err != nil {
			return nil, fmt.Errorf("quantize embedding %s: %w", base, err)
		}
		for _, p := range parts {
			if err := p.Eval(); err != nil {
				for _, q := range parts {
					q.Free()
				}
				return nil, fmt.Errorf("eval quantized embedding %s: %w", base, err)
			}
		}
		e := &Embedding{
			qW:         parts[0],
			qScales:    parts[1],
			qGroupSize: quant.GroupSize,
			qBits:      quant.Bits,
			qMode:      quant.Mode,
		}
		if len(parts) > 2 {
			e.qBiases = parts[2]
		}
		return e, nil
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

// dequantizeFullTable dequantizes the entire embedding table using
// QuantizedMatMul. Since QuantizedMatMul fuses the dequantize step
// internally (unlike the standalone Dequantize op which rejects certain
// bit widths), we multiply with a [1, vocab] one-hot-like identity to
// extract the full table. In practice we just call Dequantize on the full
// table — MLX's shape check passes when scales/biases match the full matrix
// rather than gathered slices. If that still fails, we use the quantized
// matmul path with a proper identity matrix.
func dequantizeFullTable(w, scales, biases tensor.Array, bits, groupSize int, mode string, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	// Try full-table Dequantize first (cheapest).
	if biases != nil {
		out, err := b.Dequantize(w, scales, biases, groupSize, bits, mode, s)
		if err == nil {
			if err := out.Eval(); err == nil {
				return out, nil
			}
			out.Free()
		}
	}

	// Fall back: QuantizedMatMul with identity. w is [vocab, packed_hidden],
	// transpose=true means the matmul does x @ dequant(w)^T. We want
	// dequant(w) directly. With transpose=true and x=I[hidden,hidden],
	// result = I @ dequant(w)^T = dequant(w)^T. We'd then transpose back.
	// Instead, use transpose=false and craft x = I[vocab,vocab]... but that's
	// huge. Better: just dequantize via the matmul with x being all token IDs
	// — but that's the embedding lookup itself.
	//
	// Simplest working approach: create identity [hidden, hidden] and use
	// transpose=true to get dequant(w)^T, then transpose back.
	hidden := scales.Shape()[1] * groupSize
	vocab := w.Shape()[0]
	_ = vocab

	eyeData := make([]float32, hidden*hidden)
	for i := 0; i < hidden; i++ {
		eyeData[i*hidden+i] = 1.0
	}
	eye, err := b.NewArrayFromFloat32(eyeData, []int{1, hidden, hidden})
	if err != nil {
		return nil, fmt.Errorf("create identity: %w", err)
	}
	defer eye.Free()

	// w^T dequantized via matmul: result = [1, hidden, vocab]
	dqT, err := b.QuantizedMatMul(eye, w, scales, biases, true, groupSize, bits, mode, s)
	if err != nil {
		return nil, fmt.Errorf("quantized matmul dequantize: %w", err)
	}
	if err := dqT.Eval(); err != nil {
		dqT.Free()
		return nil, fmt.Errorf("eval dequantized: %w", err)
	}

	// Transpose [1, hidden, vocab] → [vocab, hidden] (drop batch dim)
	result, err := b.TransposeAxes(dqT, []int{2, 0, 1}, s)
	if err != nil {
		dqT.Free()
		return nil, fmt.Errorf("transpose dequantized: %w", err)
	}
	dqT.Free()
	// Reshape to [vocab, hidden]
	result, err = b.Reshape(result, []int{vocab, hidden}, s)
	if err != nil {
		return nil, fmt.Errorf("reshape dequantized: %w", err)
	}
	return result, nil
}
