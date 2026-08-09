//go:build !darwin || !arm64 || !cgo || !mlx

package mlx

import "github.com/sprout-foundry/sprout/pkg/tensor"

func init() {
	tensor.RegisterBackend(&stubBackend{})
}

type stubBackend struct{}

func (*stubBackend) Name() string   { return "metal" }
func (*stubBackend) Available() bool { return false }

func (b *stubBackend) NewGPUStream() (tensor.Stream, error) { return nil, errUnavailable }
func (b *stubBackend) DefaultGPUStream() (tensor.Stream, error) { return nil, errUnavailable }
func (b *stubBackend) DefaultStream() (tensor.Stream, error) { return nil, errUnavailable }
func (b *stubBackend) NewArrayFromFloat32([]float32, []int) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) NewArrayFromInt64([]int64, []int) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) NewArrayFromInt32([]int32, []int) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) NewArrayFromBytes([]byte, []int, tensor.Dtype) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) NewScalarInt32(int) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Zeros([]int, tensor.Dtype, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Arange(float64, float64, float64, tensor.Dtype, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) RetainArray(tensor.Array) tensor.Array { return nil }
func (b *stubBackend) AsType(tensor.Array, tensor.Dtype, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Add(tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Subtract(tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Multiply(tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Divide(tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Maximum(tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Abs(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Exp(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Log(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Log1p(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Sqrt(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Square(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Negative(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Sigmoid(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Softplus(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Sin(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Cos(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Tanh(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Power(tensor.Array, float32, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Sum(tensor.Array, []int, bool, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Mean(tensor.Array, []int, bool, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Max(tensor.Array, []int, bool, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) MatMul(tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Reshape(tensor.Array, []int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Transpose(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) TransposeAxes(tensor.Array, []int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) SqueezeAxis(tensor.Array, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Slice(tensor.Array, []int, []int, []int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) SliceUpdate(tensor.Array, tensor.Array, []int, []int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) ConcatenateAxis([]tensor.Array, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Stack([]tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) SplitAxis(tensor.Array, []int, int, tensor.Stream) ([]tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) RepeatAxis(tensor.Array, int, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Pad(tensor.Array, []int, []int, []int, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Where(tensor.Array, tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Tril(tensor.Array, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Softmax(tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) SoftmaxAxis(tensor.Array, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) FastRMSNorm(tensor.Array, tensor.Array, float32, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) FastScaledDotProductAttention(tensor.Array, tensor.Array, tensor.Array, float32, string, tensor.Array, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) FastRoPE(tensor.Array, int, bool, float64, float32, int, tensor.Array, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) GatherAxis(tensor.Array, tensor.Array, int, []int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) ArgMax(tensor.Array, bool, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) ArgMaxAxis(tensor.Array, int, bool, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) ArgPartitionAxis(tensor.Array, int, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) TakeAlongAxis(tensor.Array, tensor.Array, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Conv1D(tensor.Array, tensor.Array, int, int, int, int, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Quantize(tensor.Array, int, int, string, tensor.Stream) ([]tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) QuantizedMatMul(tensor.Array, tensor.Array, tensor.Array, tensor.Array, bool, int, int, string, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) GatherQuantizedMatMul(tensor.Array, tensor.Array, tensor.Array, tensor.Array, tensor.Array, tensor.Array, bool, int, int, string, bool, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) Dequantize(tensor.Array, tensor.Array, tensor.Array, int, int, string, tensor.Stream) (tensor.Array, error) { return nil, errUnavailable }
func (b *stubBackend) SetCacheLimit(uint64) error { return errUnavailable }
func (b *stubBackend) SetMemoryLimit(uint64) error { return errUnavailable }
func (b *stubBackend) ClearCache() error { return errUnavailable }
func (b *stubBackend) TotalSystemRAM() uint64 { return 0 }

func (b *stubBackend) EnableCompile() error { return errUnavailable }
