//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// scalarBF16 creates a single-element BF16 MLX array from a float32 value.
// Used for constants in the forward pass to keep the computation graph
// homogeneous BF16, avoiding type-promotion kernel overhead.
var scalarCache = map[float32]*mlx.Array{}

func scalarBF16(val float32, s *mlx.Stream) (*mlx.Array, error) {
	if cached, ok := scalarCache[val]; ok && cached != nil {
		return cached, nil
	}
	arr, err := mlx.NewArrayFromFloat32([]float32{val}, []int{1})
	if err != nil {
		return nil, err
	}
	bf16, err := mlx.AsType(arr, mlx.BFloat16, s)
	if err != nil {
		arr.Free()
		return nil, err
	}
	arr.Free()
	scalarCache[val] = bf16
	return bf16, nil
}

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

	epsArr, err := scalarBF16(eps, s)
	if err != nil {
		return nil, err
	}

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
	negOne, err := scalarBF16(-1, s)
	if err != nil {
		return nil, err
	}

	negX, err := mlx.Multiply(x, negOne, s)
	if err != nil {
		return nil, err
	}
	defer negX.Free()

	expNegX, err := mlx.Exp(negX, s)
	if err != nil {
		return nil, err
	}
	defer expNegX.Free()

	one, err := scalarBF16(1, s)
	if err != nil {
		return nil, err
	}

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
			sinData[pos*headDim+halfDim+j] = c
			cosData[pos*headDim+halfDim+j] = c
			sinData[pos*headDim+j] = si
			sinData[pos*headDim+halfDim+j] = si
		}
	}

	cosArr, err := mlx.NewArrayFromFloat32(cosData, []int{1, 1, seqLen, headDim})
	if err != nil {
		return nil, fmt.Errorf("create cos: %w", err)
	}
	defer cosArr.Free()
	cosBF16, err := mlx.AsType(cosArr, mlx.BFloat16, s)
	if err != nil {
		return nil, fmt.Errorf("cast cos: %w", err)
	}
	defer cosBF16.Free()

	sinArr, err := mlx.NewArrayFromFloat32(sinData, []int{1, 1, seqLen, headDim})
	if err != nil {
		return nil, fmt.Errorf("create sin: %w", err)
	}
	defer sinArr.Free()
	sinBF16, err := mlx.AsType(sinArr, mlx.BFloat16, s)
	if err != nil {
		return nil, fmt.Errorf("cast sin: %w", err)
	}
	defer sinBF16.Free()

	rotated, err := rotateHalf(x, s)
	if err != nil {
		return nil, fmt.Errorf("rotate_half: %w", err)
	}
	defer rotated.Free()

	cosPart, err := mlx.Multiply(x, cosBF16, s)
	if err != nil {
		return nil, fmt.Errorf("cos multiply: %w", err)
	}
	defer cosPart.Free()

	sinPart, err := mlx.Multiply(rotated, sinBF16, s)
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

	negOne, err := scalarBF16(-1, s)
	if err != nil {
		return nil, err
	}

	negX2, err := mlx.Multiply(x2, negOne, s)
	if err != nil {
		return nil, fmt.Errorf("negate x2: %w", err)
	}
	defer negX2.Free()

	return mlx.ConcatenateAxis([]*mlx.Array{negX2, x1}, -1, s)
}

// ApplyCausalMask adds -inf to positions that should not be attended to.
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
