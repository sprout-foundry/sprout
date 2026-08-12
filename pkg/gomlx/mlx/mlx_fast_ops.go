//go:build darwin && arm64 && cgo

package mlx

/*
#include <stdlib.h>
#include <mlx/c/array.h>
#include <mlx/c/ops.h>
#include <mlx/c/fast.h>
#include <mlx/c/optional.h>
#include <mlx/c/stream.h>
#include <mlx/c/vector.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// ArgMaxAxis returns the indices of the maximum values along the given axis.
func ArgMaxAxis(a *Array, axis int, keepdims bool, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_argmax_axis(&out, a.cHandle(), C.int(axis), C.bool(keepdims), s.cHandle())
	return wrapResult(out, rc, "argmax_axis")
}

// ArgPartitionAxis returns indices that partition the array along axis so that
// the element at kth is in sorted position, with smaller elements before it.
// Used for top-k expert routing in MoE.
func ArgPartitionAxis(a *Array, kth, axis int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_argpartition_axis(&out, a.cHandle(), C.int(kth), C.int(axis), s.cHandle())
	return wrapResult(out, rc, "argpartition_axis")
}

// TakeAlongAxis gathers elements from a using indices along the given axis.
// Used for MoE top-k score extraction.
func TakeAlongAxis(a, indices *Array, axis int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_take_along_axis(&out, a.cHandle(), indices.cHandle(), C.int(axis), s.cHandle())
	return wrapResult(out, rc, "take_along_axis")
}

// ArgMax returns the index of the maximum value over the flattened array.
func ArgMax(a *Array, keepdims bool, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_argmax(&out, a.cHandle(), C.bool(keepdims), s.cHandle())
	return wrapResult(out, rc, "argmax")
}

// FastRMSNorm applies fused RMSNorm: x * rsqrt(mean(x^2) + eps) * weight.
// weight may be nil. Reduces ~7 separate ops to a single fused Metal kernel.
func FastRMSNorm(x, weight *Array, eps float32, s *Stream) (*Array, error) {
	out := newOutput()
	var w C.mlx_array
	if weight != nil {
		w = weight.cHandle()
	}
	rc := C.mlx_fast_rms_norm(&out, x.cHandle(), w, C.float(eps), s.cHandle())
	return wrapResult(out, rc, "fast_rms_norm")
}

// FastRoPE applies fused rotary position embeddings.
// dims is the number of rotary dims (half of head_dim for non-interleaved).
// traditional=true uses the GPT-J style, false uses the HF non-interleaved.
// base is the rope theta; freqs may be nil to compute internally.
func FastRoPE(x *Array, dims int, traditional bool, base float64, scale float32, offset int, freqs *Array, s *Stream) (*Array, error) {
	out := newOutput()
	var f C.mlx_array
	if freqs != nil {
		f = freqs.cHandle()
	}
	// When freqs are provided, base must not have a value (MLX constraint).
	hasBase := C.bool(true)
	if freqs != nil {
		hasBase = C.bool(false)
	}
	opt := C.mlx_optional_float{value: C.float(base), has_value: hasBase}
	rc := C.mlx_fast_rope(&out, x.cHandle(), C.int(dims), C.bool(traditional), opt, C.float(scale), C.int(offset), f, s.cHandle())
	return wrapResult(out, rc, "fast_rope")
}

// FastScaledDotProductAttention applies fused attention with optional mask.
// maskMode is one of "none", "causal", "generic".
func FastScaledDotProductAttention(q, k, v *Array, scale float32, maskMode string, maskArr, sinks *Array, s *Stream) (*Array, error) {
	out := newOutput()
	mode := C.CString(maskMode)
	defer C.free(unsafe.Pointer(mode))
	var m, snk C.mlx_array
	if maskArr != nil {
		m = maskArr.cHandle()
	}
	if sinks != nil {
		snk = sinks.cHandle()
	}
	rc := C.mlx_fast_scaled_dot_product_attention(&out, q.cHandle(), k.cHandle(), v.cHandle(), C.float(scale), mode, m, snk, s.cHandle())
	return wrapResult(out, rc, "fast_sdpa")
}

// Tanh returns the elementwise hyperbolic tangent.
func Tanh(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_tanh(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "tanh")
}

// ------------------------------------------------------------
// Shape / layout ops
// ------------------------------------------------------------

// Transpose returns the reverse-axis transpose of a (all dimensions swapped).
func Transpose(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_transpose(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "transpose")
}

// TransposeAxes permutes the axes of a according to the given permutation.
// The permutation must be a valid reordering of [0..ndim-1].
func TransposeAxes(a *Array, axes []int, s *Stream) (*Array, error) {
	cAxes, _ := cIntPtrs(axes)
	out := newOutput()
	rc := C.mlx_transpose_axes(&out, a.cHandle(), cAxes, C.size_t(len(axes)), s.cHandle())
	return wrapResult(out, rc, "transpose_axes")
}

// Reshape returns a view of a with the given shape. The total element count
// must match; -1 is supported for one inferred dimension.
func Reshape(a *Array, shape []int, s *Stream) (*Array, error) {
	cShape, _ := cIntPtrs(shape)
	if cShape == nil {
		return nil, fmt.Errorf("mlx: reshape requires a non-empty shape")
	}
	out := newOutput()
	rc := C.mlx_reshape(&out, a.cHandle(), cShape, C.size_t(len(shape)), s.cHandle())
	return wrapResult(out, rc, "reshape")
}

// ------------------------------------------------------------
// Reductions
// ------------------------------------------------------------

// Mean returns the mean of a over the given axes. When keepdims is true the
// reduced axes are retained as size-1 dimensions.
func Mean(a *Array, axes []int, keepdims bool, s *Stream) (*Array, error) {
	cAxes, _ := cIntPtrs(axes)
	out := newOutput()
	rc := C.mlx_mean_axes(&out, a.cHandle(), cAxes, C.size_t(len(axes)), C.bool(keepdims), s.cHandle())
	return wrapResult(out, rc, "mean")
}

// Sum returns the sum of a over the given axes.
func Sum(a *Array, axes []int, keepdims bool, s *Stream) (*Array, error) {
	cAxes, _ := cIntPtrs(axes)
	out := newOutput()
	rc := C.mlx_sum_axes(&out, a.cHandle(), cAxes, C.size_t(len(axes)), C.bool(keepdims), s.cHandle())
	return wrapResult(out, rc, "sum")
}

// Max returns the max of a over the given axes.
func Max(a *Array, axes []int, keepdims bool, s *Stream) (*Array, error) {
	cAxes, _ := cIntPtrs(axes)
	out := newOutput()
	rc := C.mlx_max_axes(&out, a.cHandle(), cAxes, C.size_t(len(axes)), C.bool(keepdims), s.cHandle())
	return wrapResult(out, rc, "max")
}

// ------------------------------------------------------------
// Softmax
// ------------------------------------------------------------

// Softmax applies softmax along the last axis of a. Uses the standard (non-
// precise) kernel; pass precise=true via SoftmaxAxisPrecise for higher numerical
// accuracy at a performance cost.
func Softmax(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_softmax(&out, a.cHandle(), C.bool(false), s.cHandle())
	return wrapResult(out, rc, "softmax")
}

// SoftmaxAxis applies softmax along the given axis.
func SoftmaxAxis(a *Array, axis int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_softmax_axis(&out, a.cHandle(), C.int(axis), C.bool(false), s.cHandle())
	return wrapResult(out, rc, "softmax_axis")
}

// ------------------------------------------------------------
// Split, Gather, Concatenate
// ------------------------------------------------------------

// Split splits a along axis 0 at the given indices. indices are section
// boundaries (like np.split); e.g. [2, 4] on a size-6 axis yields three
// sections [0:2], [2:4], [4:6]. The caller owns each returned Array.
func Split(a *Array, indices []int, s *Stream) ([]*Array, error) {
	return SplitAxis(a, indices, 0, s)
}

// SplitAxis splits a along the given axis. When len(indices)==1, it splits
// into 2 equal parts via mlx_split (requires the axis size to be even).
// For multiple boundaries, uses mlx_split_sections with section-boundary indices.
func SplitAxis(a *Array, indices []int, axis int, s *Stream) ([]*Array, error) {
	vec := C.mlx_vector_array_new()
	var rc C.int
	if len(indices) == 1 {
		// Single boundary → two sections: use mlx_split with num_splits=2.
		rc = C.mlx_split(&vec, a.cHandle(), C.int(len(indices)+1), C.int(axis), s.cHandle())
	} else {
		cIdx, _ := cIntPtrs(indices)
		rc = C.mlx_split_sections(&vec, a.cHandle(), cIdx, C.size_t(len(indices)), C.int(axis), s.cHandle())
	}
	if rc != 0 {
		C.mlx_vector_array_free(vec)
		return nil, fmt.Errorf("mlx: split failed (rc=%d)", rc)
	}
	defer C.mlx_vector_array_free(vec)

	n := int(C.mlx_vector_array_size(vec))
	results := make([]*Array, n)
	for i := 0; i < n; i++ {
		elem := C.mlx_array_new()
		grc := C.mlx_vector_array_get(&elem, vec, C.size_t(i))
		if grc != 0 {
			C.mlx_array_free(elem)
			for _, r := range results {
				if r != nil {
					r.Free()
				}
			}
			return nil, fmt.Errorf("mlx: split: extract element %d failed (rc=%d)", i, grc)
		}
		results[i] = wrap(elem)
	}
	return results, nil
}

// Gather indexes a along the given axes using indices. sliceSizes specifies the
// size of each dimension in the output (for each axis of a, in order). This is
// the MLX equivalent of JAX-style gather; for simple single-axis embedding
// table lookup use GatherAxis.
func Gather(a, indices *Array, axes, sliceSizes []int, s *Stream) (*Array, error) {
	cAxes, _ := cIntPtrs(axes)
	cSlice, _ := cIntPtrs(sliceSizes)

	// mlx_gather takes a mlx_vector_array of index arrays (one per gathered
	// axis); the Go API accepts a single index array, so wrap it in a vector.
	idxVec := C.mlx_vector_array_new_value(indices.cHandle())
	defer C.mlx_vector_array_free(idxVec)

	out := newOutput()
	rc := C.mlx_gather(&out, a.cHandle(), idxVec, cAxes, C.size_t(len(axes)), cSlice, C.size_t(len(sliceSizes)), s.cHandle())
	return wrapResult(out, rc, "gather")
}

// GatherAxis gathers along a single axis using a single index array — the
// common embedding-lookup shape (e.g. gather rows of a weight matrix by token
// ID). sliceSizes is the per-dimension output size (typically the input shape
// with the gathered axis replaced by its vocab entry width).
func GatherAxis(a, indices *Array, axis int, sliceSizes []int, s *Stream) (*Array, error) {
	cSlice, _ := cIntPtrs(sliceSizes)
	out := newOutput()
	rc := C.mlx_gather_single(&out, a.cHandle(), indices.cHandle(), C.int(axis), cSlice, C.size_t(len(sliceSizes)), s.cHandle())
	return wrapResult(out, rc, "gather_axis")
}

// Concatenate joins arrays along the last axis. Each input Array retains its
// own handle; the result is a new array.
func Concatenate(arrays []*Array, s *Stream) (*Array, error) {
	return ConcatenateAxis(arrays, -1, s)
}

// Stack joins arrays along a NEW leading axis (axis 0). All inputs must
// have identical shape; the result shape is [N, ...] + input shape.
func Stack(arrays []*Array, s *Stream) (*Array, error) {
	if len(arrays) == 0 {
		return nil, fmt.Errorf("mlx: stack requires at least one array")
	}
	cHandles := make([]C.mlx_array, len(arrays))
	for i, arr := range arrays {
		cHandles[i] = arr.cHandle()
	}
	vec := C.mlx_vector_array_new_data(&cHandles[0], C.size_t(len(arrays)))
	defer C.mlx_vector_array_free(vec)

	out := newOutput()
	rc := C.mlx_stack(&out, vec, s.cHandle())
	return wrapResult(out, rc, "stack")
}

// ConcatenateAxis joins arrays along the given axis.
func ConcatenateAxis(arrays []*Array, axis int, s *Stream) (*Array, error) {
	if len(arrays) == 0 {
		return nil, fmt.Errorf("mlx: concatenate requires at least one array")
	}
	// Build a mlx_vector_array from the input handles. mlx_vector_array_new_data
	// copies the mlx_array structs (which are refcounted pointers), so the
	// caller's handles stay valid.
	cHandles := make([]C.mlx_array, len(arrays))
	for i, arr := range arrays {
		cHandles[i] = arr.cHandle()
	}
	vec := C.mlx_vector_array_new_data(&cHandles[0], C.size_t(len(arrays)))
	defer C.mlx_vector_array_free(vec)

	out := newOutput()
	rc := C.mlx_concatenate_axis(&out, vec, C.int(axis), s.cHandle())
	return wrapResult(out, rc, "concatenate")
}

// ------------------------------------------------------------
// Range generation
// ------------------------------------------------------------

// Arange returns a 1-D array of values from start (inclusive) to stop
// (exclusive) stepping by step, with the given element dtype.
func Arange(start, stop, step float64, dtype Dtype, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_arange(&out, C.double(start), C.double(stop), C.double(step), C.mlx_dtype(dtype), s.cHandle())
	return wrapResult(out, rc, "arange")
}

// Cast (astype) returns a copy of a with the element type changed to dtype.
func Cast(a *Array, dtype Dtype, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_astype(&out, a.cHandle(), C.mlx_dtype(dtype), s.cHandle())
	return wrapResult(out, rc, "astype")
}

// SqueezeAxis removes a size-1 dimension at the given axis.
func SqueezeAxis(a *Array, axis int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_squeeze_axis(&out, a.cHandle(), C.int(axis), s.cHandle())
	return wrapResult(out, rc, "squeeze_axis")
}

// ------------------------------------------------------------
// Trig + power ops (for RoPE)
// ------------------------------------------------------------

// Cos returns the elementwise cosine.
func Cos(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_cos(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "cos")
}

// Sin returns the elementwise sine.
func Sin(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_sin(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "sin")
}

// Power returns a raised to the scalar power exp.
func Power(a *Array, exp float32, s *Stream) (*Array, error) {
	expArr, err := NewArrayFromFloat32([]float32{exp}, []int{1})
	if err != nil {
		return nil, err
	}
	defer expArr.Free()
	out := newOutput()
	rc := C.mlx_power(&out, a.cHandle(), expArr.cHandle(), s.cHandle())
	return wrapResult(out, rc, "power")
}

// Square returns a*a (elementwise).
func Square(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_square(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "square")
}

// ------------------------------------------------------------
// Indexing / masking ops
// ------------------------------------------------------------

// Slice extracts a sub-array along every axis. start, stop, and strides must
// have the same length as the array's ndim. A stride of 0 is invalid in MLX.
func Slice(a *Array, start, stop, strides []int, s *Stream) (*Array, error) {
	cStart, _ := cIntPtrs(start)
	cStop, _ := cIntPtrs(stop)
	cStrides, _ := cIntPtrs(strides)
	out := newOutput()
	rc := C.mlx_slice(&out, a.cHandle(),
		cStart, C.size_t(len(start)),
		cStop, C.size_t(len(stop)),
		cStrides, C.size_t(len(strides)),
		s.cHandle())
	return wrapResult(out, rc, "slice")
}

// Tril returns the lower-triangular part of a (elements below the k-th
// diagonal kept, others zeroed). Used to build causal attention masks.
func Tril(a *Array, k int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_tril(&out, a.cHandle(), C.int(k), s.cHandle())
	return wrapResult(out, rc, "tril")
}

// Where selects elements from x where condition is true, else from y.
func Where(condition, x, y *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_where(&out, condition.cHandle(), x.cHandle(), y.cHandle(), s.cHandle())
	return wrapResult(out, rc, "where")
}

// RepeatAxis repeats the array along an axis the given number of times.
func RepeatAxis(a *Array, repeats, axis int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_repeat_axis(&out, a.cHandle(), C.int(repeats), C.int(axis), s.cHandle())
	return wrapResult(out, rc, "repeat_axis")
}

// Pad pads an array along specified axes with a constant value. axes, low,
// and high must all have the same length.
func Pad(a *Array, axes, low, high []int, padValue *Array, s *Stream) (*Array, error) {
	cAxes, _ := cIntPtrs(axes)
	cLow, _ := cIntPtrs(low)
	cHigh, _ := cIntPtrs(high)
	mode := C.CString("constant")
	defer C.free(unsafe.Pointer(mode))
	out := newOutput()
	rc := C.mlx_pad(&out, a.cHandle(),
		cAxes, C.size_t(len(axes)),
		cLow, C.size_t(len(low)),
		cHigh, C.size_t(len(high)),
		padValue.cHandle(), mode, s.cHandle())
	return wrapResult(out, rc, "pad")
}
