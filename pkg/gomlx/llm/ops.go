//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// RMSNorm computes x / sqrt(mean(x^2) + eps) * weight over the last axis.
// Uses the fused backend fast_rms_norm kernel: one GPU op instead of seven.
func RMSNorm(x, weight tensor.Array, eps float32, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	normed, err := b.FastRMSNorm(x, weight, eps, s)
	if err != nil {
		return nil, fmt.Errorf("rms norm: %w", err)
	}
	return normed, nil
}

// LinearNoBias computes y = x @ W^T (no bias addition). The weight W is in
// [out, in] PyTorch layout and is transposed on every call.
func LinearNoBias(x, w tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	wT, err := b.Transpose(w, s)
	if err != nil {
		return nil, fmt.Errorf("transpose weight: %w", err)
	}
	defer wT.Free()
	return b.MatMul(x, wT, s)
}

// LinearT computes y = x @ W where W is already in [in, out] layout
// (pre-transposed at load). Avoids re-transposing weights on every call —
// important for decode, where each of the ~7 projections runs once per token.
func LinearT(x, wT tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	return b.MatMul(x, wT, s)
}

// SiLU computes x * sigmoid(x). Uses the fused Sigmoid kernel.
// fusedActivations is an optional backend capability for byte-identical fused
// SiLU/SwiGLU kernels (one op instead of sigmoid+mul / sigmoid+mul+mul).
type fusedActivations interface {
	SiLU(x tensor.Array, s tensor.Stream) (tensor.Array, error)
	SwiGLU(gate, up tensor.Array, s tensor.Stream) (tensor.Array, error)
}

func SiLU(x tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if f, ok := b.(fusedActivations); ok {
		return f.SiLU(x, s)
	}
	sig, err := b.Sigmoid(x, s)
	if err != nil {
		return nil, fmt.Errorf("silu sigmoid: %w", err)
	}
	defer sig.Free()
	return b.Multiply(x, sig, s)
}

// SwiGLU computes SiLU(gate) * up. Used by both dense MLP and MoE expert FFN.
func SwiGLU(up, gate tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if f, ok := b.(fusedActivations); ok {
		return f.SwiGLU(gate, up, s)
	}
	gateSilu, err := SiLU(gate, b, s)
	if err != nil {
		return nil, fmt.Errorf("swiglu silu: %w", err)
	}
	defer gateSilu.Free()
	return b.Multiply(gateSilu, up, s)
}

// Softplus computes log(1 + exp(x)) via log1p(exp(x)).
func Softplus(x tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	exp, err := b.Exp(x, s)
	if err != nil {
		return nil, fmt.Errorf("softplus exp: %w", err)
	}
	defer exp.Free()
	return b.Log1p(exp, s)
}

// ApplyRoPEFast applies fused rotary position embeddings using the backend
// fast_rope kernel — one Metal op instead of ~10. offset is the absolute
// position of the first token in x (0 for prefill, absolute pos for decode).
// dims = headDim (rotate all of head_dim, HF non-interleaved style).
func ApplyRoPEFast(x tensor.Array, offset, headDim int, ropeTheta float64, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	rotated, err := b.FastRoPE(x, headDim, false, ropeTheta, 1.0, offset, nil, s)
	if err != nil {
		return nil, fmt.Errorf("fast rope: %w", err)
	}
	return rotated, nil
}

// ApplyProportionalRoPE applies Gemma4's proportional RoPE. The frequencies
// are base^(2i/dims) for rotated dims and +inf for the rest, matching
// ProportionalRoPE in mlx-lm.
func ApplyProportionalRoPE(x tensor.Array, offset, dims, rotatedDims int, base float64, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	numFreqs := dims / 2
	rotatedFreqs := rotatedDims / 2
	freqs := make([]float32, numFreqs)
	for i := 0; i < rotatedFreqs; i++ {
		exp := 2.0 * float64(i) / float64(dims)
		freqs[i] = float32(math.Pow(base, exp))
	}
	for i := rotatedFreqs; i < numFreqs; i++ {
		freqs[i] = float32(math.Inf(1))
	}
	freqsArr, err := b.NewArrayFromFloat32(freqs, []int{numFreqs})
	if err != nil {
		return nil, err
	}
	defer freqsArr.Free()
	return b.FastRoPE(x, dims, false, 0, 1.0, offset, freqsArr, s)
}

func ApplyCausalMask(scores tensor.Array, seqLen, startPos, cachedLen int, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	totalLen := cachedLen + seqLen
	maskData := make([]float32, seqLen*totalLen)

	for i := 0; i < seqLen; i++ {
		absPos := cachedLen + i
		for j := 0; j < totalLen; j++ {
			if j > absPos {
				maskData[i*totalLen+j] = float32(math.Inf(-1))
			}
		}
	}

	mask, err := b.NewArrayFromFloat32(maskData, []int{1, 1, seqLen, totalLen})
	if err != nil {
		return nil, err
	}
	defer mask.Free()
	maskBF16, err := b.AsType(mask, tensor.BFloat16, s)
	if err != nil {
		return nil, err
	}
	defer maskBF16.Free()

	return b.Add(scores, maskBF16, s)
}

// GatherAxis gathers rows from a table by index. For embedding lookups.
func GatherAxis(table, indices tensor.Array, axis int, sliceSizes []int, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	return b.GatherAxis(table, indices, axis, sliceSizes, s)
}
