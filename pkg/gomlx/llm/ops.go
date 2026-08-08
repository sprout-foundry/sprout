//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// RMSNorm computes x / sqrt(mean(x^2) + eps) * weight over the last axis.
// Uses the fused mlx_fast_rms_norm kernel: one GPU op instead of seven.
func RMSNorm(x, weight *mlx.Array, eps float32, s *mlx.Stream) (*mlx.Array, error) {
	normed, err := mlx.FastRMSNorm(x, weight, eps, s)
	if err != nil {
		return nil, fmt.Errorf("rms norm: %w", err)
	}
	return normed, nil
}

// LinearNoBias computes y = x @ W^T (no bias addition). The weight W is in
// [out, in] PyTorch layout and is transposed on every call.
func LinearNoBias(x, w *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	wT, err := mlx.Transpose(w, s)
	if err != nil {
		return nil, fmt.Errorf("transpose weight: %w", err)
	}
	defer wT.Free()
	return mlx.MatMul(x, wT, s)
}

// LinearT computes y = x @ W where W is already in [in, out] layout
// (pre-transposed at load). Avoids re-transposing weights on every call —
// important for decode, where each of the ~7 projections runs once per token.
func LinearT(x, wT *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	return mlx.MatMul(x, wT, s)
}

// SiLU computes x * sigmoid(x). Uses the fused Sigmoid kernel.
func SiLU(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	sig, err := mlx.Sigmoid(x, s)
	if err != nil {
		return nil, fmt.Errorf("silu sigmoid: %w", err)
	}
	defer sig.Free()
	return mlx.Multiply(x, sig, s)
}

// Softplus computes log(1 + exp(x)) via log1p(exp(x)).
func Softplus(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	exp, err := mlx.Exp(x, s)
	if err != nil {
		return nil, fmt.Errorf("softplus exp: %w", err)
	}
	defer exp.Free()
	return mlx.Log1p(exp, s)
}


// ApplyRoPEFast applies fused rotary position embeddings using the MLX
// mlx_fast_rope kernel — one Metal op instead of ~10. offset is the absolute
// position of the first token in x (0 for prefill, absolute pos for decode).
// dims = headDim (rotate all of head_dim, HF non-interleaved style).
func ApplyRoPEFast(x *mlx.Array, offset, headDim int, ropeTheta float64, s *mlx.Stream) (*mlx.Array, error) {
	rotated, err := mlx.FastRoPE(x, headDim, false, ropeTheta, 1.0, offset, nil, s)
	if err != nil {
		return nil, fmt.Errorf("fast rope: %w", err)
	}
	return rotated, nil
}
func ApplyCausalMask(scores *mlx.Array, seqLen, startPos, cachedLen int, s *mlx.Stream) (*mlx.Array, error) {
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

	mask, err := mlx.NewArrayFromFloat32(maskData, []int{1, 1, seqLen, totalLen})
	if err != nil {
		return nil, err
	}
	defer mask.Free()
	maskBF16, err := mlx.AsType(mask, mlx.BFloat16, s)
	if err != nil {
		return nil, err
	}
	defer maskBF16.Free()

	return mlx.Add(scores, maskBF16, s)
}

// ExpandKVHeads replicates KV heads to match Q heads for GQA.
func ExpandKVHeads(x *mlx.Array, numHeads, numKVHeads int, s *mlx.Stream) (*mlx.Array, error) {
	if numHeads == numKVHeads {
		return x, nil
	}
	repeats := numHeads / numKVHeads
	return mlx.RepeatAxis(x, repeats, 1, s)
}

// GatherAxis gathers rows from a table by index. For embedding lookups.
func GatherAxis(table, indices *mlx.Array, axis int, sliceSizes []int, s *mlx.Stream) (*mlx.Array, error) {
	return mlx.GatherAxis(table, indices, axis, sliceSizes, s)
}
