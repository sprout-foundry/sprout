//go:build !darwin || !arm64 || !cgo || !mlx

// This is the non-Apple/non-cgo stub for the mlx package. It mirrors the
// public API declared in mlx_cgo.go and mlx_ops.go but every function returns
// an error, so callers that branch on Available() (and stub-out calls when it
// is false) compile and run on every platform. The embedding provider uses
// Available() to pick the MLX path on Apple Silicon and fall back to the ONNX
// backend everywhere else.
//
// The file-level comment above the build tag keeps the package doc (which lives
// in mlx_cgo.go) from being orphaned on stub-only builds: cgo and stub files
// are mutually exclusive, so the package doc only renders when the cgo file is
// present. This stub therefore intentionally has no package doc.
package mlx

import (
	"errors"
)

// errUnavailable is the single error returned by every stub function. It is
// exported indirectly via the function returns; callers only need to check
// Available() before calling, and treat any error from a stub call as "MLX not
// available on this platform".
var errUnavailable = errors.New("mlx: not available on this platform (requires Apple Silicon + cgo)")

// Dtype mirrors the cgo Dtype so constants below have a type. The zero value
// is meaningless; the constants carry the same names as the cgo build.
type Dtype int

// Dtype constants mirror the cgo build. The numeric values need not match the
// mlx_dtype enum because these are never passed to C — they exist only so the
// stub compiles the same set of exported names.
const (
	Bool      Dtype = iota
	UInt8            // explicit iota on subsequent lines for godoc alignment
	UInt16
	UInt32
	UInt64
	Int8
	Int16
	Int32
	Int64
	Float16
	Float32
	Float64
	BFloat16
	Complex64
)

// Available reports whether an Apple Silicon GPU is present and usable.
// Always false in this stub (non-cgo or non-Apple build).
func Available() bool { return false }

// Array is a non-functional placeholder on platforms without MLX.
type Array struct{}

// Free is a no-op on stub builds.
func (a *Array) Free() {}

// RetainArray is unavailable on stub builds.
func RetainArray(a *Array) *Array { return &Array{} }

// AsType is unavailable on stub builds.
func AsType(a *Array, dtype Dtype, s *Stream) (*Array, error) { return nil, errUnavailable }

// Eval always returns errUnavailable on stub builds.
func (a *Array) Eval() error { return errUnavailable }

// Float32Data always returns errUnavailable on stub builds.
func (a *Array) Float32Data() ([]float32, error) { return nil, errUnavailable }

// Int64Data always returns errUnavailable on stub builds.
func (a *Array) Int64Data() ([]int64, error) { return nil, errUnavailable }

// Size returns 0 on stub builds.
func (a *Array) Size() int { return 0 }

// Ndim returns 0 on stub builds.
func (a *Array) Ndim() int { return 0 }

// Shape returns nil on stub builds.
func (a *Array) Shape() []int { return nil }

// Dtype returns the zero value on stub builds.
func (a *Array) Dtype() Dtype { return Dtype(0) }

// Stream is a non-functional placeholder on platforms without MLX.
type Stream struct{}

// Synchronize is a no-op on stub builds.
func (s *Stream) Synchronize() error { return errUnavailable }

// Free is a no-op on stub builds.
func (s *Stream) Free() {}

// DefaultStream returns errUnavailable on stub builds.
func DefaultStream() (*Stream, error) { return nil, errUnavailable }

// DefaultGPUStream returns errUnavailable on stub builds.
func DefaultGPUStream() (*Stream, error) { return nil, errUnavailable }

// NewArrayFromFloat32 returns errUnavailable on stub builds.
func NewArrayFromFloat32(data []float32, shape []int) (*Array, error) {
	return nil, errUnavailable
}

// NewArrayFromInt64 returns errUnavailable on stub builds.
func NewArrayFromInt64(data []int64, shape []int) (*Array, error) {
	return nil, errUnavailable
}

// NewArrayFromInt32 returns errUnavailable on stub builds.
func NewArrayFromInt32(data []int32, shape []int) (*Array, error) {
	return nil, errUnavailable
}

// --- op stubs --------------------------------------------------------------

// Add returns errUnavailable on stub builds.
func Add(a, b *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Subtract returns errUnavailable on stub builds.
func Subtract(a, b *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Multiply returns errUnavailable on stub builds.
func Multiply(a, b *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Divide returns errUnavailable on stub builds.
func Divide(a, b *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// MatMul returns errUnavailable on stub builds.
func MatMul(a, b *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Maximum returns errUnavailable on stub builds.
func Maximum(a, b *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Abs returns errUnavailable on stub builds.
func Abs(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Exp returns errUnavailable on stub builds.
func Exp(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Sqrt returns errUnavailable on stub builds.
func Sqrt(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Tanh returns errUnavailable on stub builds.
func Tanh(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Transpose returns errUnavailable on stub builds.
func Transpose(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// TransposeAxes returns errUnavailable on stub builds.
func TransposeAxes(a *Array, axes []int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Reshape returns errUnavailable on stub builds.
func Reshape(a *Array, shape []int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Mean returns errUnavailable on stub builds.
func Mean(a *Array, axes []int, keepdims bool, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Sum returns errUnavailable on stub builds.
func Sum(a *Array, axes []int, keepdims bool, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Max returns errUnavailable on stub builds.
func Max(a *Array, axes []int, keepdims bool, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Softmax returns errUnavailable on stub builds.
func Softmax(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// SoftmaxAxis returns errUnavailable on stub builds.
func SoftmaxAxis(a *Array, axis int, s *Stream) (*Array, error) { return nil, errUnavailable }

// Split returns errUnavailable on stub builds.
func Split(a *Array, indices []int, s *Stream) ([]*Array, error) {
	return nil, errUnavailable
}

// SplitAxis returns errUnavailable on stub builds.
func SplitAxis(a *Array, indices []int, axis int, s *Stream) ([]*Array, error) {
	return nil, errUnavailable
}

// Gather returns errUnavailable on stub builds.
func Gather(a, indices *Array, axes, sliceSizes []int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// GatherAxis returns errUnavailable on stub builds.
func GatherAxis(a, indices *Array, axis int, sliceSizes []int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Concatenate returns errUnavailable on stub builds.
func Concatenate(arrays []*Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// ConcatenateAxis returns errUnavailable on stub builds.
func ConcatenateAxis(arrays []*Array, axis int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Arange returns errUnavailable on stub builds.
func Arange(start, stop, step float64, dtype Dtype, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Cast returns errUnavailable on stub builds.
func Cast(a *Array, dtype Dtype, s *Stream) (*Array, error) { return nil, errUnavailable }

// SqueezeAxis returns errUnavailable on stub builds.
func SqueezeAxis(a *Array, axis int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Cos returns errUnavailable on stub builds.
func Cos(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Sin returns errUnavailable on stub builds.
func Sin(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Power returns errUnavailable on stub builds.
func Power(a *Array, exp float32, s *Stream) (*Array, error) { return nil, errUnavailable }

// Square returns errUnavailable on stub builds.
func Square(a *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// Slice returns errUnavailable on stub builds.
func Slice(a *Array, start, stop, strides []int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Tril returns errUnavailable on stub builds.
func Tril(a *Array, k int, s *Stream) (*Array, error) { return nil, errUnavailable }

// Where returns errUnavailable on stub builds.
func Where(condition, x, y *Array, s *Stream) (*Array, error) { return nil, errUnavailable }

// RepeatAxis returns errUnavailable on stub builds.
func RepeatAxis(a *Array, repeats, axis int, s *Stream) (*Array, error) {
	return nil, errUnavailable
}

// Pad returns errUnavailable on stub builds.
func Pad(a *Array, axes, low, high []int, padValue *Array, s *Stream) (*Array, error) {
	return nil, errUnavailable
}
