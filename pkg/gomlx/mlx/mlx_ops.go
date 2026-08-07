//go:build darwin && arm64 && cgo && mlx

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

