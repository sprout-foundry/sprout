//go:build darwin && arm64 && cgo && mlx

package local_llm

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// rmsNorm computes x / sqrt(mean(x^2) + eps) * weight.
// Normalization is over the last axis. This is simpler than LayerNorm — no
// mean subtraction, just RMS scaling.
func rmsNorm(x, weight *mlx.Array, eps float32, s *mlx.Stream) (*mlx.Array, error) {
	lastAxis := x.Ndim() - 1

	sq, err := mlx.Square(x, s)
	if err != nil {
		return nil, fmt.Errorf("rms square: %w", err)
	}
	defer sq.Free()

	meanSq, err := mlx.Mean(sq, []int{lastAxis}, true, s)
	if err != nil {
		return nil, fmt.Errorf("rms mean: %w", err)
	}
	defer meanSq.Free()

	epsArr, err := mlx.NewArrayFromFloat32([]float32{eps}, []int{1})
	if err != nil {
		return nil, err
	}
	defer epsArr.Free()

	meanSqEps, err := mlx.Add(meanSq, epsArr, s)
	if err != nil {
		return nil, fmt.Errorf("rms add eps: %w", err)
	}
	defer meanSqEps.Free()

	rms, err := mlx.Sqrt(meanSqEps, s)
	if err != nil {
		return nil, fmt.Errorf("rms sqrt: %w", err)
	}
	defer rms.Free()

	normalized, err := mlx.Divide(x, rms, s)
	if err != nil {
		return nil, fmt.Errorf("rms divide: %w", err)
	}
	defer normalized.Free()

	return mlx.Multiply(normalized, weight, s)
}

// linearNoBias computes y = x @ W^T. Qwen3 projections have no bias.
func linearNoBias(x, w *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	wT, err := mlx.Transpose(w, s)
	if err != nil {
		return nil, fmt.Errorf("transpose weight: %w", err)
	}
	defer wT.Free()

	return mlx.MatMul(x, wT, s)
}

// silu computes x * sigmoid(x) = x / (1 + exp(-x)).
func silu(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	neg, err := mlx.NewArrayFromFloat32([]float32{-1}, []int{1})
	if err != nil {
		return nil, err
	}
	defer neg.Free()

	negX, err := mlx.Multiply(x, neg, s)
	if err != nil {
		return nil, err
	}
	defer negX.Free()

	expNegX, err := mlx.Exp(negX, s)
	if err != nil {
		return nil, err
	}
	defer expNegX.Free()

	one, err := mlx.NewArrayFromFloat32([]float32{1}, []int{1})
	if err != nil {
		return nil, err
	}
	defer one.Free()

	denom, err := mlx.Add(one, expNegX, s)
	if err != nil {
		return nil, err
	}
	defer denom.Free()

	sig, err := mlx.Divide(one, denom, s)
	if err != nil {
		return nil, err
	}
	defer sig.Free()

	return mlx.Multiply(x, sig, s)
}

// applyRoPE applies rotary position embeddings to a tensor of shape
// [1, heads, seq, head_dim].
//
// Qwen3 uses the standard HuggingFace RoPE (non-interleaved / half-split):
//   - inv_freq[i] = 1 / theta^(2i/head_dim) for i in [0, head_dim/2)
//   - freqs = positions outer inv_freq → [seq, head_dim/2]
//   - cos/sin = cos/sin(freqs), then duplicated: cat(freqs, freqs) → [seq, head_dim]
//   - rotate_half([x1, x2]) → [-x2, x1]
//   - output = x * cos + rotate_half(x) * sin
func applyRoPE(x *mlx.Array, startPos, headDim int, ropeTheta float64, s *mlx.Stream) (*mlx.Array, error) {
	shape := x.Shape() // [1, heads, seq, head_dim]
	seqLen := shape[2]

	halfDim := headDim / 2

	// Build inv_freq[halfDim] = 1 / theta^(2i/headDim)
	invFreq := make([]float64, halfDim)
	for i := 0; i < halfDim; i++ {
		invFreq[i] = 1.0 / math.Pow(ropeTheta, float64(2*i)/float64(headDim))
	}

	// Build cos/sin tables [seq, headDim]
	// cos = cat(cos(freqs), cos(freqs)), same for sin
	cosData := make([]float32, seqLen*headDim)
	sinData := make([]float32, seqLen*headDim)
	for pos := 0; pos < seqLen; pos++ {
		absPos := startPos + pos
		for j := 0; j < halfDim; j++ {
			angle := float64(absPos) * invFreq[j]
			c := float32(math.Cos(angle))
			si := float32(math.Sin(angle))
			// First half
			cosData[pos*headDim+j] = c
			sinData[pos*headDim+j] = si
			// Second half (duplicated)
			cosData[pos*headDim+halfDim+j] = c
			sinData[pos*headDim+halfDim+j] = si
		}
	}

	cosArr, err := mlx.NewArrayFromFloat32(cosData, []int{1, 1, seqLen, headDim})
	if err != nil {
		return nil, fmt.Errorf("create cos: %w", err)
	}
	defer cosArr.Free()

	sinArr, err := mlx.NewArrayFromFloat32(sinData, []int{1, 1, seqLen, headDim})
	if err != nil {
		return nil, fmt.Errorf("create sin: %w", err)
	}
	defer sinArr.Free()

	// rotate_half: [x1, x2] → [-x2, x1]
	rotated, err := rotateHalf(x, s)
	if err != nil {
		return nil, fmt.Errorf("rotate_half: %w", err)
	}
	defer rotated.Free()

	// x * cos + rotate_half(x) * sin
	cosPart, err := mlx.Multiply(x, cosArr, s)
	if err != nil {
		return nil, fmt.Errorf("cos multiply: %w", err)
	}
	defer cosPart.Free()

	sinPart, err := mlx.Multiply(rotated, sinArr, s)
	if err != nil {
		return nil, fmt.Errorf("sin multiply: %w", err)
	}
	defer sinPart.Free()

	return mlx.Add(cosPart, sinPart, s)
}

// rotateHalf splits the last dimension into two halves and rotates:
// [x1, x2] → [-x2, x1]. This is the standard HuggingFace rotate_half.
func rotateHalf(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	shape := x.Shape() // [1, heads, seq, head_dim]
	ndim := len(shape)
	headDim := shape[ndim-1]
	halfDim := headDim / 2

	// Slice first half: [..., :halfDim]
	start1 := make([]int, ndim)
	stop1 := make([]int, ndim)
	for i := range stop1 {
		stop1[i] = shape[i]
	}
	stop1[ndim-1] = halfDim
	strides := make([]int, ndim)
	for i := range strides {
		strides[i] = 1
	}

	x1, err := mlx.Slice(x, start1, stop1, strides, s)
	if err != nil {
		return nil, fmt.Errorf("slice x1: %w", err)
	}
	defer x1.Free()

	// Slice second half: [..., halfDim:]
	start2 := make([]int, ndim)
	start2[ndim-1] = halfDim
	stop2 := make([]int, ndim)
	for i := range stop2 {
		stop2[i] = shape[i]
	}

	x2, err := mlx.Slice(x, start2, stop2, strides, s)
	if err != nil {
		return nil, fmt.Errorf("slice x2: %w", err)
	}
	defer x2.Free()

	// Negate x2: -x2
	negOne, err := mlx.NewArrayFromFloat32([]float32{-1}, []int{1})
	if err != nil {
		return nil, err
	}
	defer negOne.Free()

	negX2, err := mlx.Multiply(x2, negOne, s)
	if err != nil {
		return nil, fmt.Errorf("negate x2: %w", err)
	}
	defer negX2.Free()

	// Concat [-x2, x1] along last axis
	return mlx.ConcatenateAxis([]*mlx.Array{negX2, x1}, -1, s)
}

// applyCausalMask adds -inf to positions above the diagonal so the model
// can only attend to past and present tokens.
func applyCausalMask(scores *mlx.Array, seqLen, startPos int, s *mlx.Stream) (*mlx.Array, error) {
	// scores: [1, heads, seq, seq]
	// Build a lower-triangular mask [seq, seq] and broadcast.
	maskData := make([]float32, seqLen*seqLen)
	for i := 0; i < seqLen; i++ {
		for j := 0; j < seqLen; j++ {
			if j > i {
				maskData[i*seqLen+j] = float32(math.Inf(-1))
			}
		}
	}

	mask, err := mlx.NewArrayFromFloat32(maskData, []int{1, 1, seqLen, seqLen})
	if err != nil {
		return nil, err
	}
	defer mask.Free()

	return mlx.Add(scores, mask, s)
}

// expandKVHeads replicates KV heads to match the number of Q heads for GQA.
// Input: [1, num_kv_heads, seq, head_dim]
// Output: [1, num_heads, seq, head_dim]
// Each KV head is repeated num_heads/num_kv_heads times.
func expandKVHeads(x *mlx.Array, numHeads, numKVHeads int, s *mlx.Stream) (*mlx.Array, error) {
	if numHeads == numKVHeads {
		return x, nil // no copy needed; caller must not Free the result
	}
	repeats := numHeads / numKVHeads
	// Repeat each KV head `repeats` times along the head axis (axis=1)
	return mlx.RepeatAxis(x, repeats, 1, s)
}
