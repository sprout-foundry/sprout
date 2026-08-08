//go:build darwin && arm64 && cgo && mlx

package embedding

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

// linear computes y = x @ W^T + b. MLX matmul computes x @ W, so we transpose
// W first when the weight is stored in [out, in] format (PyTorch convention).
// If bias is nil, no bias is added.
func linear(x, w, b *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	wT, err := mlx.Transpose(w, s)
	if err != nil {
		return nil, fmt.Errorf("transpose weight: %w", err)
	}
	defer wT.Free()

	out, err := mlx.MatMul(x, wT, s)
	if err != nil {
		return nil, fmt.Errorf("matmul: %w", err)
	}

	if b != nil {
		defer out.Free()
		return mlx.Add(out, b, s)
	}
	return out, nil
}

// layerNorm computes (x - mean) / sqrt(var + eps) * weight + bias.
// MLX C API has no mlx_layer_norm, so we compose it from primitives.
// Normalization is over the last axis.
func layerNorm(x, weight, bias *mlx.Array, eps float32, s *mlx.Stream) (*mlx.Array, error) {
	// Normalize over the last axis. Resolve negative axis now since the C
	// API may not handle them.
	ndim := x.Ndim()
	lastAxis := ndim - 1
	if lastAxis < 0 {
		lastAxis = 0
	}

	mean, err := mlx.Mean(x, []int{lastAxis}, true, s)
	if err != nil {
		return nil, fmt.Errorf("ln mean: %w", err)
	}
	defer mean.Free()

	centered, err := mlx.Subtract(x, mean, s)
	if err != nil {
		return nil, fmt.Errorf("ln subtract: %w", err)
	}
	defer centered.Free()

	sq, err := mlx.Multiply(centered, centered, s)
	if err != nil {
		return nil, fmt.Errorf("ln square: %w", err)
	}
	defer sq.Free()

	variance, err := mlx.Mean(sq, []int{lastAxis}, true, s)
	if err != nil {
		return nil, fmt.Errorf("ln var: %w", err)
	}
	defer variance.Free()

	// eps array
	epsArr, err := mlx.NewArrayFromFloat32([]float32{eps}, []int{1})
	if err != nil {
		return nil, err
	}
	defer epsArr.Free()

	varEps, err := mlx.Add(variance, epsArr, s)
	if err != nil {
		return nil, fmt.Errorf("ln var+eps: %w", err)
	}
	defer varEps.Free()

	std, err := mlx.Sqrt(varEps, s)
	if err != nil {
		return nil, fmt.Errorf("ln sqrt: %w", err)
	}
	defer std.Free()

	normalized, err := mlx.Divide(centered, std, s)
	if err != nil {
		return nil, fmt.Errorf("ln divide: %w", err)
	}
	defer normalized.Free()

	scaled, err := mlx.Multiply(normalized, weight, s)
	if err != nil {
		return nil, fmt.Errorf("ln scale: %w", err)
	}
	defer scaled.Free()

	return mlx.Add(scaled, bias, s)
}

// gelu computes the tanh approximation of GELU:
//
//	0.5 * x * (1 + tanh(sqrt(2/π) * (x + 0.044715 * x³)))
//
// The exact GELU (using erf) is also available via mlx.Exp and mlx.Tanh,
// but the tanh approximation is standard for Jina/BERT models.
func gelu(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	// x^3
	x3, err := mlx.Multiply(x, x, s)
	if err != nil {
		return nil, err
	}
	defer x3.Free()
	x3, err = mlx.Multiply(x3, x, s)
	if err != nil {
		return nil, err
	}
	defer x3.Free()

	// 0.044715 * x^3
	coeff, err := mlx.NewArrayFromFloat32([]float32{0.044715}, []int{1})
	if err != nil {
		return nil, err
	}
	defer coeff.Free()

	inner2, err := mlx.Multiply(x3, coeff, s)
	if err != nil {
		return nil, err
	}
	defer inner2.Free()

	// x + 0.044715 * x^3
	inner, err := mlx.Add(x, inner2, s)
	if err != nil {
		return nil, err
	}
	defer inner.Free()

	// sqrt(2/π) ≈ 0.7978845608
	sqrtConst, err := mlx.NewArrayFromFloat32([]float32{0.7978845608}, []int{1})
	if err != nil {
		return nil, err
	}
	defer sqrtConst.Free()

	inner, err = mlx.Multiply(inner, sqrtConst, s)
	if err != nil {
		return nil, err
	}
	defer inner.Free()

	// tanh(...)
	tanhOut, err := mlx.Tanh(inner, s)
	if err != nil {
		return nil, err
	}
	defer tanhOut.Free()

	// 1 + tanh(...)
	one, err := mlx.NewArrayFromFloat32([]float32{1.0}, []int{1})
	if err != nil {
		return nil, err
	}
	defer one.Free()

	onePlusTanh, err := mlx.Add(one, tanhOut, s)
	if err != nil {
		return nil, err
	}
	defer onePlusTanh.Free()

	// 0.5 * x
	half, err := mlx.NewArrayFromFloat32([]float32{0.5}, []int{1})
	if err != nil {
		return nil, err
	}
	defer half.Free()

	halfX, err := mlx.Multiply(x, half, s)
	if err != nil {
		return nil, err
	}
	defer halfX.Free()

	return mlx.Multiply(halfX, onePlusTanh, s)
}

// embeddingLookup gathers rows from a weight matrix [vocab, hidden] using
// token IDs [batch, seq]. Returns [batch, seq, hidden].
func embeddingLookup(table, ids *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	tableShape := table.Shape()
	sliceSizes := make([]int, len(tableShape))
	sliceSizes[0] = 1
	for i := 1; i < len(tableShape); i++ {
		sliceSizes[i] = tableShape[i]
	}
	result, err := mlx.GatherAxis(table, ids, 0, sliceSizes, s)
	if err != nil {
		return nil, err
	}
	defer result.Free()

	// GatherAxis produces [batch, seq, 1, hidden] — squeeze the extra dim.
	return mlx.SqueezeAxis(result, 2, s)
}

// splitLastDim splits a tensor along its last axis into two halves.
// Used by GEGLU: the up_gated_layer output [batch, seq, 2*intermediate]
// splits into up=[batch, seq, intermediate] and gate=[batch, seq, intermediate].
func splitLastDim(x *mlx.Array, intermediate int, s *mlx.Stream) (*mlx.Array, *mlx.Array, error) {
	shape := x.Shape()
	batch := shape[0]
	seq := shape[1]

	// Split along the last axis at position `intermediate`.
	// MLX split_sections returns a vector of arrays.
	results, err := mlx.SplitAxis(x, []int{intermediate}, 2, s)
	if err != nil {
		return nil, nil, err
	}
	if len(results) != 2 {
		for _, r := range results {
			r.Free()
		}
		return nil, nil, fmt.Errorf("split returned %d arrays, expected 2", len(results))
	}

	// Reshape to explicit [batch, seq, intermediate] since split preserves the
	// split dimension size.
	up, err := mlx.Reshape(results[0], []int{batch, seq, intermediate}, s)
	if err != nil {
		results[0].Free()
		results[1].Free()
		return nil, nil, err
	}
	results[0].Free()

	gate, err := mlx.Reshape(results[1], []int{batch, seq, intermediate}, s)
	if err != nil {
		up.Free()
		results[1].Free()
		return nil, nil, err
	}
	results[1].Free()

	return up, gate, nil
}

// buildALiBiBias constructs the ALiBi positional bias tensor [1, heads, seq, seq].
// alibi[h, i, j] = -slope[h] * |i - j|
func buildALiBiBias(heads, seq int, s *mlx.Stream) (*mlx.Array, error) {
	slopes := getAlibiSlopes(heads)

	// Build [1, heads, seq, seq] as a flat float32 slice
	totalSize := heads * seq * seq
	data := make([]float32, totalSize)

	for h := 0; h < heads; h++ {
		slope := slopes[h]
		for i := 0; i < seq; i++ {
			for j := 0; j < seq; j++ {
				rel := math.Abs(float64(i - j))
				idx := h*seq*seq + i*seq + j
				data[idx] = float32(-slope * rel)
			}
		}
	}

	return mlx.NewArrayFromFloat32(data, []int{1, heads, seq, seq})
}

// getAlibiSlopes returns the ALiBi head slopes for n_heads.
// Adapted from the Jina/Press reference implementation.
func getAlibiSlopes(n int) []float64 {
	powerOf2 := func(n2 int) []float64 {
		start := math.Pow(2, -(math.Pow(2, -(math.Log2(float64(n2)) - 3))))
		ratio := start
		result := make([]float64, n2)
		for i := 0; i < n2; i++ {
			result[i] = start * math.Pow(ratio, float64(i))
		}
		return result
	}

	if math.Log2(float64(n)) == math.Trunc(math.Log2(float64(n))) {
		return powerOf2(n)
	}

	closest := int(math.Pow(2, math.Floor(math.Log2(float64(n)))))
	// Python: power_of_2(closest) + _get_alibi_slopes(2*closest)[0::2][:n-closest]
	// The [0::2] takes every other element (stride 2), then [:n-closest]
	// trims to the needed count.
	full := getAlibiSlopes(2 * closest)
	needed := n - closest
	strided := make([]float64, 0, needed)
	for i := 0; i < len(full) && len(strided) < needed; i += 2 {
		strided = append(strided, full[i])
	}
	return append(powerOf2(closest), strided...)
}

// meanPoolNorm computes attention-masked mean pooling over the sequence dimension,
// then L2-normalizes the result.
func meanPoolNorm(hidden, mask *mlx.Array, seq, dims int, s *mlx.Stream) (*mlx.Array, error) {
	// mask: [batch, seq] → [batch, seq, 1]
	maskShape := mask.Shape()
	batch := maskShape[0]
	mask3, err := mlx.Reshape(mask, []int{batch, seq, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("reshape mask: %w", err)
	}
	defer mask3.Free()

	// Cast mask to float32 if needed
	maskF, err := mlx.Cast(mask3, mlx.Float32, s)
	if err != nil {
		return nil, fmt.Errorf("cast mask: %w", err)
	}
	defer maskF.Free()

	// masked = hidden * mask3
	masked, err := mlx.Multiply(hidden, maskF, s)
	if err != nil {
		return nil, fmt.Errorf("masked multiply: %w", err)
	}
	defer masked.Free()

	// sum over seq axis (axis=1, keepdims=false)
	pooled, err := mlx.Sum(masked, []int{1}, false, s)
	if err != nil {
		return nil, fmt.Errorf("pool sum: %w", err)
	}
	defer pooled.Free()

	// counts = mask.sum(axis=1, keepdims=false) → [batch, 1]
	counts, err := mlx.Sum(maskF, []int{1}, false, s)
	if err != nil {
		return nil, fmt.Errorf("pool count: %w", err)
	}
	defer counts.Free()

	// Reshape counts to [batch, 1] for broadcasting
	counts2, err := mlx.Reshape(counts, []int{batch, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("reshape counts: %w", err)
	}
	defer counts2.Free()

	// Add epsilon to avoid division by zero
	eps, err := mlx.NewArrayFromFloat32([]float32{1e-9}, []int{1})
	if err != nil {
		return nil, err
	}
	defer eps.Free()

	countsSafe, err := mlx.Add(counts2, eps, s)
	if err != nil {
		return nil, err
	}
	defer countsSafe.Free()

	pooledNorm, err := mlx.Divide(pooled, countsSafe, s)
	if err != nil {
		return nil, fmt.Errorf("pool divide: %w", err)
	}
	defer pooledNorm.Free()

	// L2 normalize
	sq, err := mlx.Multiply(pooledNorm, pooledNorm, s)
	if err != nil {
		return nil, err
	}
	defer sq.Free()

	sqSum, err := mlx.Sum(sq, []int{1}, true, s)
	if err != nil {
		return nil, err
	}
	defer sqSum.Free()

	norm, err := mlx.Sqrt(sqSum, s)
	if err != nil {
		return nil, err
	}
	defer norm.Free()

	normSafe, err := mlx.Add(norm, eps, s)
	if err != nil {
		return nil, err
	}
	defer normSafe.Free()

	return mlx.Divide(pooledNorm, normSafe, s)
}
