//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// RMSNorm computes x / sqrt(mean(x^2) + eps) * weight over the last axis.
func RMSNorm(x, weight *mlx.Array, eps float32, s *mlx.Stream) (*mlx.Array, error) {
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

// LinearNoBias computes y = x @ W^T (no bias addition).
func LinearNoBias(x, w *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	wT, err := mlx.Transpose(w, s)
	if err != nil {
		return nil, fmt.Errorf("transpose weight: %w", err)
	}
	defer wT.Free()
	return mlx.MatMul(x, wT, s)
}

// SiLU computes x * sigmoid(x) = x / (1 + exp(-x)).
func SiLU(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
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

// ApplyRoPE applies rotary position embeddings (non-interleaved/half-split).
// Standard HuggingFace implementation:
//   - inv_freq[i] = 1 / theta^(2i/head_dim) for i in [0, head_dim/2)
//   - cos/sin = cos/sin(positions * inv_freq), duplicated: cat(freqs, freqs)
//   - output = x * cos + rotate_half(x) * sin
func ApplyRoPE(x *mlx.Array, startPos, headDim int, ropeTheta float64, s *mlx.Stream) (*mlx.Array, error) {
	shape := x.Shape()
	seqLen := shape[2]
	halfDim := headDim / 2

	invFreq := make([]float64, halfDim)
	for i := 0; i < halfDim; i++ {
		invFreq[i] = 1.0 / math.Pow(ropeTheta, float64(2*i)/float64(headDim))
	}

	cosData := make([]float32, seqLen*headDim)
	sinData := make([]float32, seqLen*headDim)
	for pos := 0; pos < seqLen; pos++ {
		absPos := startPos + pos
		for j := 0; j < halfDim; j++ {
			angle := float64(absPos) * invFreq[j]
			c := float32(math.Cos(angle))
			si := float32(math.Sin(angle))
			cosData[pos*headDim+j] = c
			sinData[pos*headDim+j] = si
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

	rotated, err := rotateHalf(x, s)
	if err != nil {
		return nil, fmt.Errorf("rotate_half: %w", err)
	}
	defer rotated.Free()

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
// [x1, x2] → [-x2, x1].
func rotateHalf(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	shape := x.Shape()
	ndim := len(shape)
	headDim := shape[ndim-1]
	halfDim := headDim / 2

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

	return mlx.ConcatenateAxis([]*mlx.Array{negX2, x1}, -1, s)
}

// ApplyCausalMask adds -inf to positions that should not be attended to.
// seqLen is the number of new tokens. cachedLen is how many tokens are already
// in the KV cache before this pass. During prefill, cachedLen=0 and the mask
// is a standard causal mask [seq, seq]. During cached decode (seqLen=1), no
// mask is needed since the single token can attend to all cached tokens.
func ApplyCausalMask(scores *mlx.Array, seqLen, startPos, cachedLen int, s *mlx.Stream) (*mlx.Array, error) {
	totalLen := cachedLen + seqLen
	maskData := make([]float32, seqLen*totalLen)

	// Each new token at position startPos+i can attend to cached positions 0..cachedLen-1
	// and new positions 0..i (inclusive)
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

	return mlx.Add(scores, mask, s)
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
