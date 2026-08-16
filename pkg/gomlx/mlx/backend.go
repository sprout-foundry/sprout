//go:build darwin && arm64 && cgo

// Package mlx is the Apple Metal tensor backend, implemented as a thin CGO
// wrapper around Apple's MLX C API. It implements the tensor.Backend interface.
//
// On non-Apple platforms, mlx_stub.go provides a no-op Backend whose
// Available() returns false.
package mlx

import (
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	tensor.RegisterBackend(&MetalBackend{})
}

// MetalBackend implements tensor.Backend via the existing mlx CGO functions.
// It is a zero-allocation adapter: every method delegates directly to the
// package-level function, casting between the interface types and the
// concrete *mlx.Array / *mlx.Stream.
type MetalBackend struct{}

func (*MetalBackend) Name() string    { return "metal" }
func (*MetalBackend) Available() bool { return gpuAvailable }

func (b *MetalBackend) NewGPUStream() (tensor.Stream, error) {
	s, err := NewGPUStream()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (b *MetalBackend) DefaultGPUStream() (tensor.Stream, error) {
	s, err := DefaultGPUStream()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (b *MetalBackend) DefaultStream() (tensor.Stream, error) {
	s, err := DefaultStream()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (b *MetalBackend) NewArrayFromFloat32(data []float32, shape []int) (tensor.Array, error) {
	return NewArrayFromFloat32(data, shape)
}

func (b *MetalBackend) NewArrayFromInt64(data []int64, shape []int) (tensor.Array, error) {
	return NewArrayFromInt64(data, shape)
}

func (b *MetalBackend) NewArrayFromInt32(data []int32, shape []int) (tensor.Array, error) {
	return NewArrayFromInt32(data, shape)
}

func (b *MetalBackend) NewArrayFromBytes(data []byte, shape []int, dtype tensor.Dtype) (tensor.Array, error) {
	return NewArrayFromBytes(data, shape, mlxDtype(dtype))
}

func (b *MetalBackend) NewScalarInt32(v int) (tensor.Array, error) {
	return NewScalarInt32(v)
}

func (b *MetalBackend) Zeros(shape []int, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	return Zeros(shape, mlxDtype(dtype), toStream(s))
}

func (b *MetalBackend) Arange(start, stop, step float64, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	return Arange(start, stop, step, mlxDtype(dtype), toStream(s))
}

func (b *MetalBackend) RetainArray(a tensor.Array) tensor.Array {
	if a == nil {
		return nil
	}
	return RetainArray(a.(*Array))
}

func (b *MetalBackend) AsType(a tensor.Array, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	return AsType(a.(*Array), mlxDtype(dtype), toStream(s))
}

func (b *MetalBackend) Add(a, x tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Add(a.(*Array), x.(*Array), toStream(s))
}

func (b *MetalBackend) Subtract(a, x tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Subtract(a.(*Array), x.(*Array), toStream(s))
}

func (b *MetalBackend) Multiply(a, x tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Multiply(a.(*Array), x.(*Array), toStream(s))
}

func (b *MetalBackend) Divide(a, x tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Divide(a.(*Array), x.(*Array), toStream(s))
}

func (b *MetalBackend) Maximum(a, x tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Maximum(a.(*Array), x.(*Array), toStream(s))
}

func (b *MetalBackend) Abs(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Abs(a.(*Array), toStream(s))
}

func (b *MetalBackend) Exp(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Exp(a.(*Array), toStream(s))
}

func (b *MetalBackend) Log(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Log(a.(*Array), toStream(s))
}

func (b *MetalBackend) Log1p(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Log1p(a.(*Array), toStream(s))
}

func (b *MetalBackend) Sqrt(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Sqrt(a.(*Array), toStream(s))
}

func (b *MetalBackend) Square(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Square(a.(*Array), toStream(s))
}

func (b *MetalBackend) Negative(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Negative(a.(*Array), toStream(s))
}

func (b *MetalBackend) Sigmoid(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Sigmoid(a.(*Array), toStream(s))
}

func (b *MetalBackend) Softplus(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Softplus(a.(*Array), toStream(s))
}

func (b *MetalBackend) Sin(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Sin(a.(*Array), toStream(s))
}

func (b *MetalBackend) Cos(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Cos(a.(*Array), toStream(s))
}

func (b *MetalBackend) Tanh(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Tanh(a.(*Array), toStream(s))
}

func (b *MetalBackend) Power(a tensor.Array, exp float32, s tensor.Stream) (tensor.Array, error) {
	return Power(a.(*Array), exp, toStream(s))
}

func (b *MetalBackend) Sum(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return Sum(a.(*Array), axes, keepdims, toStream(s))
}

func (b *MetalBackend) Mean(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return Mean(a.(*Array), axes, keepdims, toStream(s))
}

func (b *MetalBackend) Max(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return Max(a.(*Array), axes, keepdims, toStream(s))
}

func (b *MetalBackend) MatMul(a, x tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return MatMul(a.(*Array), x.(*Array), toStream(s))
}

func (b *MetalBackend) Reshape(a tensor.Array, shape []int, s tensor.Stream) (tensor.Array, error) {
	return Reshape(a.(*Array), shape, toStream(s))
}

func (b *MetalBackend) Transpose(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Transpose(a.(*Array), toStream(s))
}

func (b *MetalBackend) TransposeAxes(a tensor.Array, axes []int, s tensor.Stream) (tensor.Array, error) {
	return TransposeAxes(a.(*Array), axes, toStream(s))
}

func (b *MetalBackend) SqueezeAxis(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return SqueezeAxis(a.(*Array), axis, toStream(s))
}

func (b *MetalBackend) Slice(a tensor.Array, start, stop, strides []int, s tensor.Stream) (tensor.Array, error) {
	return Slice(a.(*Array), start, stop, strides, toStream(s))
}

func (b *MetalBackend) SliceUpdate(src, update tensor.Array, start, stop []int, s tensor.Stream) (tensor.Array, error) {
	return SliceUpdate(src.(*Array), update.(*Array), start, stop, toStream(s))
}

func (b *MetalBackend) ConcatenateAxis(arrays []tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	mlxArrays := make([]*Array, len(arrays))
	for i, a := range arrays {
		mlxArrays[i] = a.(*Array)
	}
	return ConcatenateAxis(mlxArrays, axis, toStream(s))
}

func (b *MetalBackend) Stack(arrays []tensor.Array, s tensor.Stream) (tensor.Array, error) {
	mlxArrays := make([]*Array, len(arrays))
	for i, a := range arrays {
		mlxArrays[i] = a.(*Array)
	}
	return Stack(mlxArrays, toStream(s))
}

func (b *MetalBackend) SplitAxis(a tensor.Array, indices []int, axis int, s tensor.Stream) ([]tensor.Array, error) {
	results, err := SplitAxis(a.(*Array), indices, axis, toStream(s))
	if err != nil {
		return nil, err
	}
	out := make([]tensor.Array, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out, nil
}

func (b *MetalBackend) RepeatAxis(a tensor.Array, repeats, axis int, s tensor.Stream) (tensor.Array, error) {
	return RepeatAxis(a.(*Array), repeats, axis, toStream(s))
}

func (b *MetalBackend) Pad(a tensor.Array, axes, low, high []int, padValue tensor.Array, s tensor.Stream) (tensor.Array, error) {
	var pv *Array
	if padValue != nil {
		pv = padValue.(*Array)
	}
	return Pad(a.(*Array), axes, low, high, pv, toStream(s))
}

func (b *MetalBackend) Where(condition, x, y tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Where(condition.(*Array), x.(*Array), y.(*Array), toStream(s))
}

func (b *MetalBackend) Tril(a tensor.Array, k int, s tensor.Stream) (tensor.Array, error) {
	return Tril(a.(*Array), k, toStream(s))
}

func (b *MetalBackend) Softmax(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return Softmax(a.(*Array), toStream(s))
}

func (b *MetalBackend) SoftmaxAxis(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return SoftmaxAxis(a.(*Array), axis, toStream(s))
}

// SoftmaxAxisPrecise applies softmax with fp32 accumulation (precise=true),
// matching mlx-lm's router softmax over hundreds of experts.
func (b *MetalBackend) SoftmaxAxisPrecise(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return SoftmaxAxisPrecise(a.(*Array), axis, toStream(s))
}

func (b *MetalBackend) FastRMSNorm(x, weight tensor.Array, eps float32, s tensor.Stream) (tensor.Array, error) {
	var w *Array
	if weight != nil {
		w = weight.(*Array)
	}
	return FastRMSNorm(x.(*Array), w, eps, toStream(s))
}

func (b *MetalBackend) FastScaledDotProductAttention(q, k, v tensor.Array, scale float32, maskMode string, maskArr, sinks tensor.Array, s tensor.Stream) (tensor.Array, error) {
	var ma, sa *Array
	if maskArr != nil {
		ma = maskArr.(*Array)
	}
	if sinks != nil {
		sa = sinks.(*Array)
	}
	return FastScaledDotProductAttention(q.(*Array), k.(*Array), v.(*Array), scale, maskMode, ma, sa, toStream(s))
}

func (b *MetalBackend) FastRoPE(x tensor.Array, dims int, traditional bool, base float64, scale float32, offset int, freqs tensor.Array, s tensor.Stream) (tensor.Array, error) {
	var f *Array
	if freqs != nil {
		f = freqs.(*Array)
	}
	return FastRoPE(x.(*Array), dims, traditional, base, scale, offset, f, toStream(s))
}

func (b *MetalBackend) GatherAxis(a, indices tensor.Array, axis int, sliceSizes []int, s tensor.Stream) (tensor.Array, error) {
	return GatherAxis(a.(*Array), indices.(*Array), axis, sliceSizes, toStream(s))
}

func (b *MetalBackend) ArgMax(a tensor.Array, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return ArgMax(a.(*Array), keepdims, toStream(s))
}

func (b *MetalBackend) ArgMaxAxis(a tensor.Array, axis int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return ArgMaxAxis(a.(*Array), axis, keepdims, toStream(s))
}

func (b *MetalBackend) ArgPartitionAxis(a tensor.Array, kth, axis int, s tensor.Stream) (tensor.Array, error) {
	return ArgPartitionAxis(a.(*Array), kth, axis, toStream(s))
}

func (b *MetalBackend) TakeAlongAxis(a, indices tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return TakeAlongAxis(a.(*Array), indices.(*Array), axis, toStream(s))
}

func (b *MetalBackend) Conv1D(input, weight tensor.Array, stride, padding, dilation, groups int, s tensor.Stream) (tensor.Array, error) {
	return Conv1D(input.(*Array), weight.(*Array), stride, padding, dilation, groups, toStream(s))
}

func (b *MetalBackend) Quantize(w tensor.Array, groupSize, bits int, mode string, s tensor.Stream) ([]tensor.Array, error) {
	results, err := Quantize(w.(*Array), groupSize, bits, mode, toStream(s))
	if err != nil {
		return nil, err
	}
	out := make([]tensor.Array, len(results))
	for i, r := range results {
		out[i] = r
	}
	return out, nil
}

func (b *MetalBackend) QuantizedMatMul(x, w, scales tensor.Array, biases tensor.Array, transpose bool, groupSize, bits int, mode string, s tensor.Stream) (tensor.Array, error) {
	var bs *Array
	if biases != nil {
		bs = biases.(*Array)
	}
	return QuantizedMatMul(x.(*Array), w.(*Array), scales.(*Array), bs, transpose, groupSize, bits, mode, toStream(s))
}

func (b *MetalBackend) GatherQuantizedMatMul(x, w, scales, biases, lhsIndices, rhsIndices tensor.Array, transpose bool, groupSize, bits int, mode string, sortedIndices bool, s tensor.Stream) (tensor.Array, error) {
	var bs *Array
	if biases != nil {
		bs = biases.(*Array)
	}
	var li *Array
	if lhsIndices != nil {
		li = lhsIndices.(*Array)
	}
	return GatherQuantizedMatMul(x.(*Array), w.(*Array), scales.(*Array), bs, li, rhsIndices.(*Array), transpose, groupSize, bits, mode, sortedIndices, toStream(s))
}

func (b *MetalBackend) Dequantize(w, scales, biases tensor.Array, groupSize, bits int, mode string, s tensor.Stream) (tensor.Array, error) {
	var bs *Array
	if biases != nil {
		bs = biases.(*Array)
	}
	return Dequantize(w.(*Array), scales.(*Array), bs, groupSize, bits, mode, toStream(s))
}

func (b *MetalBackend) SetCacheLimit(bytes uint64) error  { return SetCacheLimit(bytes) }
func (b *MetalBackend) SetMemoryLimit(bytes uint64) error { return SetMemoryLimit(bytes) }
func (b *MetalBackend) ClearCache() error                 { return ClearCache() }
func (b *MetalBackend) TotalSystemRAM() uint64            { return TotalSystemRAM() }

func (b *MetalBackend) AsyncEvalBatch(arrays []tensor.Array) error {
	if len(arrays) == 0 {
		return nil
	}
	converted := make([]*Array, len(arrays))
	for i, a := range arrays {
		converted[i] = a.(*Array)
	}
	return AsyncEvalBatch(converted)
}

// mlxDtype converts tensor.Dtype to the internal mlx Dtype.
// They share the same integer ordering (both follow the mlx_dtype enum).
func mlxDtype(dt tensor.Dtype) Dtype { return Dtype(dt) }

// toStream extracts the concrete *mlx.Stream from a tensor.Stream.
func toStream(s tensor.Stream) *Stream {
	if s == nil {
		return nil
	}
	return s.(*Stream)
}

func (b *MetalBackend) EnableCompile() error { return EnableCompile() }

func (*MetalBackend) NativeQuantization() bool { return true }
