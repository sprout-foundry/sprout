//go:build darwin && arm64 && cgo && mlx

// Package mlx is a thin CGO wrapper around Apple's MLX C API
// (github.com/ml-explore/mlx-c), exposing just the ~30 functions needed to
// run the Jina Code v2 embedding model's forward pass on Apple Silicon GPU
// (Metal). The wrapper handles the two awkward parts of the C API:
//
//   - The handle types (mlx_array, mlx_stream, mlx_device) are small
//     refcounted structs passed by value; ops take mlx_array* output params
//     and return int error codes. The Go wrapper returns (*Array, error) and
//     checks the int.
//   - Array memory: every mlx_array must be freed. The Go *Array wraps the
//     handle, exposes an explicit Free, and registers a finalizer as a safety
//     net so a forgotten Free is reclaimed at GC time.
//
// The package only compiles on darwin/arm64 with cgo. Every other platform
// gets mlx_stub.go, whose Available() returns false so callers can fall back
// to the ONNX backend (see pkg/embedding).
//
// The C signatures below are copied from the upstream mlx-c headers
// (array.h, ops.h, stream.h, device.h, vector.h) and must stay byte-for-byte
// aligned with them — any drift silently corrupts the ABI.
package mlx

/*
#cgo CFLAGS: -DMLX_C_BINDINGS
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lmlx -lmlxc

#include <mlx/c/array.h>
#include <mlx/c/device.h>
#include <mlx/c/ops.h>
#include <mlx/c/stream.h>
#include <mlx/c/vector.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// Dtype is an MLX array element type. Values match the mlx_dtype enum so they
// pass straight through to the C API.
type Dtype C.mlx_dtype

// Dtype constants mirror the mlx_dtype enum order in array.h. Exported as
// Dtype (not the C enum) so callers don't depend on cgo.
const (
	Bool      = Dtype(C.MLX_BOOL)
	UInt8     = Dtype(C.MLX_UINT8)
	UInt16    = Dtype(C.MLX_UINT16)
	UInt32    = Dtype(C.MLX_UINT32)
	UInt64    = Dtype(C.MLX_UINT64)
	Int8      = Dtype(C.MLX_INT8)
	Int16     = Dtype(C.MLX_INT16)
	Int32     = Dtype(C.MLX_INT32)
	Int64     = Dtype(C.MLX_INT64)
	Float16   = Dtype(C.MLX_FLOAT16)
	Float32   = Dtype(C.MLX_FLOAT32)
	Float64   = Dtype(C.MLX_FLOAT64)
	BFloat16  = Dtype(C.MLX_BFLOAT16)
	Complex64 = Dtype(C.MLX_COMPLEX64)
)

// gpuAvailable is set at package init by probing a GPU device. Probing once
// up front avoids a per-call availability check in the hot path, and keeps
// Available() a cheap const-like read.
var gpuAvailable bool

func init() {
	gpuAvailable = probeGPU()
}

// probeGPU reports whether at least one Metal GPU device is usable. MLX falls
// back to CPU when no GPU is present; the embedding provider prefers Metal,
// so it uses Available() to decide whether to even attempt the MLX path.
func probeGPU() bool {
	dev := C.mlx_device_new_type(C.MLX_GPU, 0)
	if dev.ctx == nil {
		return false
	}
	defer C.mlx_device_free(dev)

	var avail C.bool
	if rc := C.mlx_device_is_available(&avail, dev); rc != 0 {
		return false
	}
	return bool(avail)
}

// Available reports whether an Apple Silicon GPU is present and usable.
// False on non-Apple/non-cgo builds (the stub) and on Macs with no GPU.
func Available() bool { return gpuAvailable }

// Array wraps an mlx_array handle. MLX arrays are lazy: op functions build a
// graph and return immediately, and evaluation happens on the next eval or
// data read. Call Free to release the handle promptly; a finalizer catches
// leaks, but explicit freeing matters on the GPU path where pending arrays
// hold Metal buffers.
type Array struct {
	handle C.mlx_array
}

// finalize releases the underlying handle when the Array is garbage collected.
// It is a safety net, not the primary cleanup path — call Free explicitly for
// deterministic release.
func (a *Array) finalize() {
	if a.handle.ctx != nil {
		C.mlx_array_free(a.handle)
		a.handle.ctx = nil
	}
}

// Free releases the underlying MLX handle. Safe to call multiple times.
// After Free, the Array must not be used.
func (a *Array) Free() {
	runtime.SetFinalizer(a, nil)
	a.finalize()
}

// wrap turns a freshly created C.mlx_array into a Go *Array with a finalizer.
// The returned Array owns the handle. Assumes the caller just created the
// handle (new or from an op) and the caller will not free it themselves.
func wrap(h C.mlx_array) *Array {
	a := &Array{handle: h}
	runtime.SetFinalizer(a, (*Array).finalize)
	return a
}

// checkRC converts a non-zero C return code into a Go error with the op name
// for context. MLX uses 0 for success and non-zero for failure; the codes are
// not documented as stable values, so we surface only success/failure.
func checkRC(rc C.int, op string) error {
	if rc != 0 {
		return fmt.Errorf("mlx: %s failed (rc=%d)", op, rc)
	}
	return nil
}

// wrapResult produces a Go *Array from an op output param. On error it frees
// the scratch handle (only if the op populated it) and returns nil. Keep the
// success/free branching here so every op site stays a one-liner (see mlx_ops.go).
func wrapResult(h C.mlx_array, rc C.int, op string) (*Array, error) {
	if rc != 0 {
		if h.ctx != nil {
			C.mlx_array_free(h)
		}
		return nil, fmt.Errorf("mlx: %s failed (rc=%d)", op, rc)
	}
	return wrap(h), nil
}

// NewArrayFromFloat32 creates an Array by copying data into MLX with the
// given shape. The Go slice may be reused or modified after this returns.
func NewArrayFromFloat32(data []float32, shape []int) (*Array, error) {
	if err := checkShape(shape, len(data)); err != nil {
		return nil, err
	}
	return newArrayFromData(cPointer(data), shape, Float32)
}

// NewArrayFromInt64 creates an Array from int64 data. Used for token-ID input
// tensors in transformer models.
func NewArrayFromInt64(data []int64, shape []int) (*Array, error) {
	if err := checkShape(shape, len(data)); err != nil {
		return nil, err
	}
	return newArrayFromData(cPointer(data), shape, Int64)
}

// NewArrayFromInt32 creates an Array from int32 data. Used for attention masks.
func NewArrayFromInt32(data []int32, shape []int) (*Array, error) {
	if err := checkShape(shape, len(data)); err != nil {
		return nil, err
	}
	return newArrayFromData(cPointer(data), shape, Int32)
}

// checkShape validates that shape is non-empty and its element product matches
// the number of elements the caller is about to hand MLX. An empty data slice
// with a zero-element shape is allowed; mismatched counts corrupt the buffer.
func checkShape(shape []int, n int) error {
	if len(shape) == 0 {
		return errors.New("mlx: shape must have at least one dimension")
	}
	product := 1
	for _, d := range shape {
		product *= d
	}
	if product != n {
		return fmt.Errorf("mlx: shape %v has %d elements but data has %d", shape, product, n)
	}
	return nil
}

// cPointer returns a pointer to the first element of a slice, or nil if the
// slice is empty (passing &data[0] on a zero-length slice would panic). The
// returned pointer is only valid while data is live and unmodified.
func cPointer[T any](data []T) unsafe.Pointer {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Pointer(&data[0])
}

// newArrayFromData is the shared constructor. dataPtr must point to a buffer
// whose element type matches dtype and whose element count equals the product
// of shape. MLX copies the buffer, so the caller retains ownership.
func newArrayFromData(dataPtr unsafe.Pointer, shape []int, dtype Dtype) (*Array, error) {
	cShape := make([]C.int, len(shape))
	for i, d := range shape {
		cShape[i] = C.int(d)
	}
	h := C.mlx_array_new_data(
		dataPtr,
		(*C.int)(unsafe.Pointer(&cShape[0])),
		C.int(len(shape)),
		C.mlx_dtype(dtype),
	)
	return wrap(h), nil
}

// handle returns the underlying C handle for op functions. Panics on a freed
// Array so use-after-free fails loudly instead of corrupting MLX state.
func (a *Array) cHandle() C.mlx_array {
	if a.handle.ctx == nil {
		panic("mlx: use of freed Array")
	}
	return a.handle
}

// Eval forces evaluation of a lazy array and returns nil on success. GPU
// arrays are queued on a stream; Eval blocks until the value is materialized.
func (a *Array) Eval() error {
	return checkRC(C.mlx_array_eval(a.cHandle()), "eval")
}

// Float32Data returns the array's data as a float32 slice. It evaluates the
// array first (MLX only exposes a data pointer once the array is materialized)
// and copies out into a freshly allocated Go slice so the caller owns the
// memory. Panics if the array's dtype is not float32.
func (a *Array) Float32Data() ([]float32, error) {
	if got := a.Dtype(); got != Float32 {
		return nil, fmt.Errorf("mlx: Float32Data on %v array", got)
	}
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := int(C.mlx_array_size(a.cHandle()))
	if n == 0 {
		return []float32{}, nil
	}
	ptr := C.mlx_array_data_float32(a.cHandle())
	if ptr == nil {
		return nil, errors.New("mlx: data pointer is null (eval failed?)")
	}
	out := make([]float32, n)
	// mlx_array_data_float32 returns a pointer into MLX-owned memory; back a
	// Go slice with it just long enough to copy, so the result stays valid
	// after the array is freed.
	backed := unsafe.Slice((*float32)(unsafe.Pointer(ptr)), n)
	copy(out, backed)
	return out, nil
}

// Int64Data returns the array's data as an int64 slice, evaluating first.
// See Float32Data for the copy rationale.
func (a *Array) Int64Data() ([]int64, error) {
	if got := a.Dtype(); got != Int64 {
		return nil, fmt.Errorf("mlx: Int64Data on %v array", got)
	}
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := int(C.mlx_array_size(a.cHandle()))
	if n == 0 {
		return []int64{}, nil
	}
	ptr := C.mlx_array_data_int64(a.cHandle())
	if ptr == nil {
		return nil, errors.New("mlx: data pointer is null (eval failed?)")
	}
	out := make([]int64, n)
	backed := unsafe.Slice((*int64)(unsafe.Pointer(ptr)), n)
	copy(out, backed)
	return out, nil
}

// Size returns the total number of elements in the array.
func (a *Array) Size() int {
	return int(C.mlx_array_size(a.cHandle()))
}

// Ndim returns the number of dimensions.
func (a *Array) Ndim() int {
	return int(C.mlx_array_ndim(a.cHandle()))
}

// Shape returns a copy of the array's shape. The C pointer is owned by MLX and
// only valid while the array lives, so we copy into a Go slice.
func (a *Array) Shape() []int {
	ndim := a.Ndim()
	if ndim == 0 {
		return nil
	}
	cShape := C.mlx_array_shape(a.cHandle())
	// mlx_array_shape returns const int*; back a Go slice with it to copy out.
	backed := unsafe.Slice((*C.int)(unsafe.Pointer(cShape)), ndim)
	shape := make([]int, ndim)
	for i := 0; i < ndim; i++ {
		shape[i] = int(backed[i])
	}
	return shape
}

// Dtype returns the array's element type.
func (a *Array) Dtype() Dtype {
	return Dtype(C.mlx_array_dtype(a.cHandle()))
}

// Stream wraps an mlx_stream, the queue ops are submitted to on a device.
type Stream struct {
	handle C.mlx_stream
	dev    C.mlx_device // retained so Free can free both stream and device
}

// cHandle returns the underlying C stream handle.
func (s *Stream) cHandle() C.mlx_stream {
	if s.handle.ctx == nil {
		panic("mlx: use of freed Stream")
	}
	return s.handle
}

// DefaultStream returns the default stream on the default device. When a GPU
// is available this is the Metal command queue; otherwise it is the CPU
// stream. Callers submit ops to this stream and Synchronize to await them.
func DefaultStream() (*Stream, error) {
	var dev C.mlx_device
	if rc := C.mlx_get_default_device(&dev); rc != 0 || dev.ctx == nil {
		return nil, fmt.Errorf("mlx: get default device failed (rc=%d)", rc)
	}
	var stream C.mlx_stream
	if rc := C.mlx_get_default_stream(&stream, dev); rc != 0 || stream.ctx == nil {
		C.mlx_device_free(dev)
		return nil, fmt.Errorf("mlx: get default stream failed (rc=%d)", rc)
	}
	s := &Stream{handle: stream, dev: dev}
	return s, nil
}

// DefaultGPUStream returns the default GPU stream, or an error if no GPU is
// available. Used when the caller wants to force the Metal path explicitly.
func DefaultGPUStream() (*Stream, error) {
	if !gpuAvailable {
		return nil, errors.New("mlx: no GPU available")
	}
	stream := C.mlx_default_gpu_stream_new()
	if stream.ctx == nil {
		return nil, errors.New("mlx: default GPU stream is null")
	}
	// The stream owns its device reference; we hold no device handle to free.
	return &Stream{handle: stream}, nil
}

// Synchronize blocks until all ops queued on this stream complete. Needed
// before reading GPU results that were produced by recently submitted ops.
func (s *Stream) Synchronize() error {
	return checkRC(C.mlx_synchronize(s.cHandle()), "synchronize")
}

// Free releases the stream and (if it owns one) the device it was created
// from. Safe to call multiple times.
func (s *Stream) Free() {
	if s.handle.ctx != nil {
		C.mlx_stream_free(s.handle)
		s.handle.ctx = nil
	}
	if s.dev.ctx != nil {
		C.mlx_device_free(s.dev)
		s.dev.ctx = nil
	}
}
