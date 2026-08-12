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

// newOutput allocates an empty mlx_array handle for use as an op's output
// parameter. MLX op functions overwrite the pointed-to handle; the caller wraps
// the result on success (see wrapResult) or frees it on error.
func newOutput() C.mlx_array {
	return C.mlx_array_new()
}

// cIntPtrs converts a Go []int into a (*C.int, count) pair suitable for passing
// to C functions that take `const int*` + `size_t`. Returns (nil, 0) for an
// empty slice so callers can pass those straight through. The returned backing
// slice must stay in scope until the C call returns; since every op here is
// synchronous that is just "the rest of this function".
func cIntPtrs(s []int) (*C.int, []C.int) {
	if len(s) == 0 {
		return nil, nil
	}
	cs := make([]C.int, len(s))
	for i, v := range s {
		cs[i] = C.int(v)
	}
	return &cs[0], cs
}

// ----------------------------------------------------------------------------
// Binary elementwise ops
// ------------------------------------------------------------

// Add returns a + b.
func Add(a, b *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_add(&out, a.cHandle(), b.cHandle(), s.cHandle())
	return wrapResult(out, rc, "add")
}

// Subtract returns a - b.
func Subtract(a, b *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_subtract(&out, a.cHandle(), b.cHandle(), s.cHandle())
	return wrapResult(out, rc, "subtract")
}

// Multiply returns a * b (elementwise).
func Multiply(a, b *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_multiply(&out, a.cHandle(), b.cHandle(), s.cHandle())
	return wrapResult(out, rc, "multiply")
}

// Divide returns a / b (elementwise).
func Divide(a, b *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_divide(&out, a.cHandle(), b.cHandle(), s.cHandle())
	return wrapResult(out, rc, "divide")
}

// MatMul returns the matrix product a @ b.
func MatMul(a, b *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_matmul(&out, a.cHandle(), b.cHandle(), s.cHandle())
	return wrapResult(out, rc, "matmul")
}

// Quantize converts a float weight matrix into MLX's quantized representation
// and returns [weight, scales, biases] (biases nil when mode produces none).
// w must be a 2D [out, in] matrix in PyTorch layout. groupSize is the group
// size for scale/bias (typically 64 or 128); bits is 2-8 (4, 6, 8 common).
// mode is "affine" (default) or a grouped mode. The returned weight is an
// int32 array in [out, in*bits/32] packed layout, matching what
// QuantizedMatMul expects with transpose=true.
func Quantize(w *Array, groupSize, bits int, mode string, s *Stream) ([]*Array, error) {
	var vec C.mlx_vector_array = C.mlx_vector_array_new()
	defer C.mlx_vector_array_free(vec)
	gs := C.mlx_optional_int{value: C.int(groupSize), has_value: true}
	bs := C.mlx_optional_int{value: C.int(bits), has_value: true}
	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))
	rc := C.mlx_quantize(&vec, w.cHandle(), gs, bs, cMode, C.mlx_array{}, s.cHandle())
	if rc != 0 {
		return nil, fmt.Errorf("mlx: quantize: %s", lastMLXError())
	}
	n := int(C.mlx_vector_array_size(vec))
	out := make([]*Array, 0, n)
	for i := 0; i < n; i++ {
		var h C.mlx_array
		if grc := C.mlx_vector_array_get(&h, vec, C.size_t(i)); grc != 0 {
			for _, a := range out {
				a.Free()
			}
			return nil, fmt.Errorf("mlx: quantize: read output %d: %s", i, lastMLXError())
		}
		out = append(out, wrap(h))
	}
	return out, nil
}

// Dequantize expands a quantized weight back to its full float representation
// (the inverse of Quantize). w is the packed int32 weight [out, in*bits/32];
// scales is [out, in/groupSize]; biases is [out, in/groupSize] or nil for
// modes without bias. Returns the dequantized [out, in] array in the same
// dtype as w's original scale factor (typically float32 or bfloat16).
// Used for quantized embedding lookups where gather+dequantize on the
// selected rows is cheaper than a full dequant + plain matmul.
func Dequantize(w, scales, biases *Array, groupSize, bits int, mode string, s *Stream) (*Array, error) {
	out := newOutput()
	gs := C.mlx_optional_int{value: C.int(groupSize), has_value: true}
	bs := C.mlx_optional_int{value: C.int(bits), has_value: true}
	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))
	var biasH C.mlx_array
	if biases != nil {
		biasH = biases.cHandle()
	}
	rc := C.mlx_dequantize(&out, w.cHandle(), scales.cHandle(), biasH, gs, bs, cMode, C.mlx_array{}, C.mlx_optional_dtype{}, s.cHandle())
	return wrapResult(out, rc, "dequantize")
}

// QuantizedMatMul computes x @ dequant(w)^T with weights quantized by
// Quantize (or loaded from an MLX-format safetensors file). w is the packed
// int32 weight [out, in*bits/32]; scales is [out, in/groupSize]; biases is
// [out, in/groupSize] or nil for modes without bias. transpose should be
// true when w is stored in [out, in] PyTorch layout (the mlx-lm convention).
func QuantizedMatMul(x, w, scales *Array, biases *Array, transpose bool, groupSize, bits int, mode string, s *Stream) (*Array, error) {
	out := newOutput()
	gs := C.mlx_optional_int{value: C.int(groupSize), has_value: true}
	bs := C.mlx_optional_int{value: C.int(bits), has_value: true}
	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))
	var biasH C.mlx_array
	if biases != nil {
		biasH = biases.cHandle()
	}
	rc := C.mlx_quantized_matmul(&out, x.cHandle(), w.cHandle(), scales.cHandle(), biasH, C.bool(transpose), gs, bs, cMode, s.cHandle())
	return wrapResult(out, rc, "quantized_matmul")
}

// GatherQuantizedMatMul computes x @ dequant(w[indices])^T per-expert.
// w is [num_experts, out, in_packed], scales/biases are [num_experts, out, in/group].
// rhs_indices selects which expert each token uses. Used for MoE inference.
func GatherQuantizedMatMul(x, w, scales, biases *Array, lhsIndices, rhsIndices *Array, transpose bool, groupSize, bits int, mode string, sortedIndices bool, s *Stream) (*Array, error) {
	out := newOutput()
	gs := C.mlx_optional_int{value: C.int(groupSize), has_value: true}
	bs := C.mlx_optional_int{value: C.int(bits), has_value: true}
	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))
	var biasH C.mlx_array
	if biases != nil {
		biasH = biases.cHandle()
	}
	var lhsH C.mlx_array
	if lhsIndices != nil {
		lhsH = lhsIndices.cHandle()
	}
	rc := C.mlx_gather_qmm(&out, x.cHandle(), w.cHandle(), scales.cHandle(), biasH, lhsH, rhsIndices.cHandle(), C.bool(transpose), gs, bs, cMode, C.bool(sortedIndices), s.cHandle())
	return wrapResult(out, rc, "gather_qmm")
}

// Maximum returns the elementwise max of a and b.
func Maximum(a, b *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_maximum(&out, a.cHandle(), b.cHandle(), s.cHandle())
	return wrapResult(out, rc, "maximum")
}

// ------------------------------------------------------------
// Unary elementwise ops
// ------------------------------------------------------------

// Abs returns the elementwise absolute value.
func Abs(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_abs(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "abs")
}

// Exp returns the elementwise e^x.
func Exp(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_exp(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "exp")
}

// Log returns the elementwise natural logarithm.
func Log(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_log(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "log")
}

// Log1p returns the elementwise log(1 + x).
func Log1p(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_log1p(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "log1p")
}

// Softplus returns the elementwise softplus: log(1 + exp(x)), computed
// stably as log1p(exp(x)). Used by the DeltaNet decay gate.
func Softplus(a *Array, s *Stream) (*Array, error) {
	ex, err := Exp(a, s)
	if err != nil {
		return nil, fmt.Errorf("softplus exp: %w", err)
	}
	defer ex.Free()
	return Log1p(ex, s)
}

// Sqrt returns the elementwise square root.
func Sqrt(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_sqrt(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "sqrt")
}

// Rsqrt returns the elementwise reciprocal square root (1/sqrt(x)).
func Rsqrt(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_rsqrt(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "rsqrt")
}

// Sigmoid returns the elementwise logistic sigmoid (1 / (1 + exp(-x))).
func Sigmoid(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_sigmoid(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "sigmoid")
}

// Negative returns the elementwise negation (-x).
func Negative(a *Array, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_negative(&out, a.cHandle(), s.cHandle())
	return wrapResult(out, rc, "negative")
}

// Zeros creates a new array of the given shape filled with zeros.
func Zeros(shape []int, dtype Dtype, s *Stream) (*Array, error) {
	out := newOutput()
	shapePtr, _ := cIntPtrs(shape)
	rc := C.mlx_zeros(&out, shapePtr, C.size_t(len(shape)), C.mlx_dtype(dtype), s.cHandle())
	return wrapResult(out, rc, "zeros")
}

// SliceUpdate writes update into src at the region [start, stop) and returns
// a new array sharing src's buffer. For the KV cache this performs an
// in-place-style write of one token into a preallocated buffer.
func SliceUpdate(src, update *Array, start, stop []int, s *Stream) (*Array, error) {
	out := newOutput()
	startPtr, _ := cIntPtrs(start)
	stopPtr, _ := cIntPtrs(stop)
	strides := []int{1, 1, 1, 1}
	stridesPtr, _ := cIntPtrs(strides)
	rc := C.mlx_slice_update(&out, src.cHandle(), update.cHandle(),
		startPtr, C.size_t(len(start)), stopPtr, C.size_t(len(stop)), stridesPtr, C.size_t(len(strides)), s.cHandle())
	return wrapResult(out, rc, "slice_update")
}

// Conv1D applies a 1D convolution. input is [B, L, C_in]; weight is
// [C_out, C_in/groups, kernel]. stride/padding/dilation are as documented
// for mlx_conv1d. For the DeltaNet block: groups == C_in (depthwise) and
// C_out == C_in.
func Conv1D(input, weight *Array, stride, padding, dilation, groups int, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_conv1d(&out, input.cHandle(), weight.cHandle(),
		C.int(stride), C.int(padding), C.int(dilation), C.int(groups), s.cHandle())
	return wrapResult(out, rc, "conv1d")
}
