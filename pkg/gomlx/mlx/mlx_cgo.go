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
#include <mlx/c/error.h>
#include <mlx/c/ops.h>
#include <mlx/c/stream.h>
#include <mlx/c/vector.h>

// Defined in mlx_error_shim.c; forwards to the exported Go error handler.
void mlx_go_error_handler_shim(const char* msg, void* data);
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// mlxLastError captures the most recent mlx-c error message so checkRC can
// surface it as a Go error. mlx-c's default error handler prints to stderr
// and calls exit(-1), which would kill the whole process on any runtime
// error (e.g. a stream used from the wrong thread). We install a handler
// that records the message instead.
var (
	mlxErrMu   sync.Mutex
	mlxErrMsg  [1024]byte
	mlxErrGrow []byte
)

//export mlxGoErrorHandler
func mlxGoErrorHandler(msg *C.char, data unsafe.Pointer) {
	if msg == nil {
		return
	}
	mlxErrMu.Lock()
	defer mlxErrMu.Unlock()
	// Copy C string into a Go byte slice (C.GoString copies; keep it raw to
	// avoid an extra allocation in the hot error path — errors are rare).
	s := C.GoString(msg)
	if len(s) >= len(mlxErrMsg) {
		mlxErrGrow = []byte(s)
		return
	}
	copy(mlxErrMsg[:], s)
	// Clear any stale growth buffer.
	mlxErrGrow = nil
}

func init() {
	C.mlx_set_error_handler((C.mlx_error_handler_func)(C.mlx_go_error_handler_shim), nil, nil)
}

func lastMLXError() string {
	mlxErrMu.Lock()
	defer mlxErrMu.Unlock()
	if mlxErrGrow != nil {
		return string(mlxErrGrow)
	}
	s := mlxErrMsg[:]
	n := 0
	for n < len(s) && s[n] != 0 {
		n++
	}
	return string(s[:n])
}

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

// Free releases the underlying MLX handle. Safe to call multiple times and
// on nil receivers (which makes ownership handoff to the KV cache clean).
// After Free, the Array must not be used.
func (a *Array) Free() {
	if a == nil {
		return
	}
	runtime.SetFinalizer(a, nil)
	a.finalize()
}

// RetainArray creates a new Go *Array that shares the same underlying MLX
// array, incrementing the C refcount. Both the original and the retained copy
// must be freed independently. Use this when multiple owners need to hold a
// reference to the same array (e.g. KV caching).
func RetainArray(a *Array) *Array {
	retained := C.mlx_array_new()
	C.mlx_array_set(&retained, a.cHandle())
	return wrap(retained)
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
// not documented as stable values, so we surface only success/failure. The
// captured mlx-c error message (via mlxGoErrorHandler) is appended when
// present — otherwise errors like "no Stream in current thread" would surface
// as opaque "failed (rc=1)".
func checkRC(rc C.int, op string) error {
	if rc != 0 {
		if msg := lastMLXError(); msg != "" {
			return fmt.Errorf("mlx: %s: %s", op, msg)
		}
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
		if msg := lastMLXError(); msg != "" {
			return nil, fmt.Errorf("mlx: %s: %s", op, msg)
		}
		return nil, fmt.Errorf("mlx: %s failed (rc=%d)", op, rc)
	}
	return wrap(h), nil
}

// AsType casts an array to a different dtype.
func AsType(a *Array, dtype Dtype, s *Stream) (*Array, error) {
	out := newOutput()
	rc := C.mlx_astype(&out, a.cHandle(), C.mlx_dtype(dtype), s.cHandle())
	return wrapResult(out, rc, "astype")
}

// NewArrayFromBytes creates an Array from a raw byte buffer with an explicit
// dtype. The bytes are copied into MLX-managed memory. Used to load BF16/F16
// weights without a Go-side conversion to float32.
func NewArrayFromBytes(data []byte, shape []int, dtype Dtype) (*Array, error) {
	if len(data) == 0 {
		return nil, errors.New("mlx: empty data buffer")
	}
	return newArrayFromData(unsafe.Pointer(&data[0]), shape, dtype)
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

// NewScalarInt32 creates a 0-dim (scalar) int32 array. Metal custom kernels
// treat ndim==0 arrays as by-value scalar arguments (const constant T&),
// which is how the DeltaNet kernel receives its sequence-length T.
func NewScalarInt32(v int) (*Array, error) {
	h := C.mlx_array_new_int(C.int(v))
	return wrap(h), nil
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

// Uint32Data returns the array's data as a uint32 slice, evaluating first.
// Used for argmax/argmin results, which MLX returns as uint32.
func (a *Array) Uint32Data() ([]uint32, error) {
	if got := a.Dtype(); got != UInt32 {
		return nil, fmt.Errorf("mlx: Uint32Data on %v array", got)
	}
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := int(C.mlx_array_size(a.cHandle()))
	if n == 0 {
		return []uint32{}, nil
	}
	ptr := C.mlx_array_data_uint32(a.cHandle())
	if ptr == nil {
		return nil, errors.New("mlx: data pointer is null (eval failed?)")
	}
	out := make([]uint32, n)
	backed := unsafe.Slice((*uint32)(unsafe.Pointer(ptr)), n)
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

// NewGPUStream creates a brand-new GPU stream. Unlike DefaultGPUStream (which
// returns a process-wide singleton), NewGPUStream gives the caller an
// independent stream that is registered in the CURRENT thread's command
// encoder map on first use. MLX command encoders are thread_local: the
// process singleton breaks whenever the Go runtime migrates a goroutine to a
// different OS thread between model load and generation. Creating a fresh
// stream while holding runtime.LockOSThread avoids that class of
// "There is no Stream(gpu, N) in current thread" failures.
func NewGPUStream() (*Stream, error) {
	if !gpuAvailable {
		return nil, errors.New("mlx: no GPU available")
	}
	var dev C.mlx_device
	if rc := C.mlx_get_default_device(&dev); rc != 0 || dev.ctx == nil {
		return nil, fmt.Errorf("mlx: get default device failed (rc=%d)", rc)
	}
	stream := C.mlx_stream_new_device(dev)
	C.mlx_device_free(dev)
	if stream.ctx == nil {
		return nil, errors.New("mlx: new GPU stream is null")
	}
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
