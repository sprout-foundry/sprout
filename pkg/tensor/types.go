// Package tensor defines the backend-agnostic tensor compute interface.
//
// The interface is implemented by platform-specific backends:
//   - metal (Apple Silicon, via MLX CGO bindings)
//   - cuda (NVIDIA, future via GGML)
//   - rocm (AMD, future via GGML)
//   - vulkan (portable, future via GGML)
//
// The model layer (pkg/gomlx/llm/) uses this interface exclusively, so the
// same forward pass, tokenizer, KV cache, MTP, and sampling code runs on
// every GPU vendor with no changes.
//
// Phase 1 (this file): defines the interface. The metal backend
// (pkg/gomlx/mlx/) implements it by wrapping existing CGO calls. No
// behavioral change — the interface mirrors the existing mlx package API
// exactly.
package tensor

// Dtype is a tensor element type.
// The integer values match the mlx_dtype C enum order exactly (see
// mlx/c/array.h) so that Metal backend ops can cast directly without
// a conversion table.
type Dtype int

const (
	Bool      Dtype = iota // 0  MLX_BOOL
	UInt8                  // 1  MLX_UINT8
	UInt16                 // 2  MLX_UINT16
	UInt32                 // 3  MLX_UINT32
	UInt64                 // 4  MLX_UINT64
	Int8                   // 5  MLX_INT8
	Int16                  // 6  MLX_INT16
	Int32                  // 7  MLX_INT32
	Int64                  // 8  MLX_INT64
	Float16                // 9  MLX_FLOAT16
	Float32                // 10 MLX_FLOAT32
	Float64                // 11 MLX_FLOAT64
	BFloat16               // 12 MLX_BFLOAT16
	Complex64              // 13 MLX_COMPLEX64
)

// Array is a multi-dimensional tensor handle. Concrete implementations
// (mlx.Array, ggml.Tensor) hold vendor-specific GPU memory. Arrays are
// reference-counted by the backend; callers must Free when done.
type Array interface {
	Shape() []int
	Dtype() Dtype
	Ndim() int
	Size() int
	Eval() error
	// AsyncEval schedules the array's graph for evaluation without blocking.
	// Backends without an async dispatch queue may implement this as a
	// synchronous Eval.
	AsyncEval() error
	Free()
	Float32Data() ([]float32, error)
	RawBytes() ([]byte, error)
	Int64Data() ([]int64, error)
	Uint32Data() ([]uint32, error)
}

// Stream is a compute command queue. Ops submitted to the same stream
// execute in order; ops across streams may overlap.
type Stream interface {
	Synchronize() error
	Free()
}

// MetalKernelConfig configures a fused Metal kernel launch (Metal backend
// only). Other backends ignore this.
type MetalKernelConfig interface {
	AddOutputArg(shape []int, dtype Dtype) error
	AddTemplateArgBool(name string, value bool) error
	AddTemplateArgInt(name string, value int) error
	AddTemplateArgDtype(name string, dtype Dtype) error
	SetGrid(g1, g2, g3 int) error
	SetThreadGroup(t1, t2, t3 int) error
	SetInitValue(v float32) error
	Free()
}

// MetalKernel is a compiled Metal kernel (Metal backend only).
type MetalKernel interface {
	Apply(inputs []Array, config MetalKernelConfig, s Stream) ([]Array, error)
	Free()
}

// Closure is a compiled compute graph closure (Metal backend only).
type Closure interface {
	Apply(inputs []Array) ([]Array, error)
	Compile(inputShapes []Array) (Closure, error)
	Free()
}

// Backend provides tensor operations for a specific GPU vendor. The model
// layer calls these methods instead of platform-specific functions.
//
// IMPORTANT: every method that returns (Array, error) creates a new Array
// that the caller must Free. Input Arrays are borrowed (not consumed) unless
// documented otherwise.
type Backend interface {
	// Capability
	Available() bool
	Name() string

	// Stream management
	NewGPUStream() (Stream, error)
	DefaultGPUStream() (Stream, error)
	DefaultStream() (Stream, error)

	// Array creation
	NewArrayFromFloat32(data []float32, shape []int) (Array, error)
	NewArrayFromInt64(data []int64, shape []int) (Array, error)
	NewArrayFromInt32(data []int32, shape []int) (Array, error)
	NewArrayFromBytes(data []byte, shape []int, dtype Dtype) (Array, error)
	NewScalarInt32(v int) (Array, error)
	Zeros(shape []int, dtype Dtype, s Stream) (Array, error)
	Arange(start, stop, step float64, dtype Dtype, s Stream) (Array, error)

	// Array utilities
	RetainArray(a Array) Array
	AsType(a Array, dtype Dtype, s Stream) (Array, error)

	// Elementwise binary
	Add(a, b Array, s Stream) (Array, error)
	Subtract(a, b Array, s Stream) (Array, error)
	Multiply(a, b Array, s Stream) (Array, error)
	Divide(a, b Array, s Stream) (Array, error)
	Maximum(a, b Array, s Stream) (Array, error)

	// Elementwise unary
	Abs(a Array, s Stream) (Array, error)
	Exp(a Array, s Stream) (Array, error)
	Log(a Array, s Stream) (Array, error)
	Log1p(a Array, s Stream) (Array, error)
	Sqrt(a Array, s Stream) (Array, error)
	Square(a Array, s Stream) (Array, error)
	Negative(a Array, s Stream) (Array, error)
	Sigmoid(a Array, s Stream) (Array, error)
	Softplus(a Array, s Stream) (Array, error)
	Sin(a Array, s Stream) (Array, error)
	Cos(a Array, s Stream) (Array, error)
	Tanh(a Array, s Stream) (Array, error)
	Power(a Array, exp float32, s Stream) (Array, error)

	// Reductions
	Sum(a Array, axes []int, keepdims bool, s Stream) (Array, error)
	Mean(a Array, axes []int, keepdims bool, s Stream) (Array, error)
	Max(a Array, axes []int, keepdims bool, s Stream) (Array, error)

	// Linear algebra
	MatMul(a, b Array, s Stream) (Array, error)

	// Shape manipulation
	Reshape(a Array, shape []int, s Stream) (Array, error)
	Transpose(a Array, s Stream) (Array, error)
	TransposeAxes(a Array, axes []int, s Stream) (Array, error)
	SqueezeAxis(a Array, axis int, s Stream) (Array, error)
	Slice(a Array, start, stop, strides []int, s Stream) (Array, error)
	SliceUpdate(src, update Array, start, stop []int, s Stream) (Array, error)
	ConcatenateAxis(arrays []Array, axis int, s Stream) (Array, error)
	Stack(arrays []Array, s Stream) (Array, error)
	SplitAxis(a Array, indices []int, axis int, s Stream) ([]Array, error)
	RepeatAxis(a Array, repeats, axis int, s Stream) (Array, error)
	Pad(a Array, axes, low, high []int, padValue Array, s Stream) (Array, error)
	Where(condition, x, y Array, s Stream) (Array, error)
	Tril(a Array, k int, s Stream) (Array, error)

	// Normalization
	Softmax(a Array, s Stream) (Array, error)
	SoftmaxAxis(a Array, axis int, s Stream) (Array, error)
	FastRMSNorm(x, weight Array, eps float32, s Stream) (Array, error)

	// Attention
	FastScaledDotProductAttention(q, k, v Array, scale float32, maskMode string, maskArr, sinks Array, s Stream) (Array, error)

	// Positional encoding
	FastRoPE(x Array, dims int, traditional bool, base float64, scale float32, offset int, freqs Array, s Stream) (Array, error)

	// Indexing
	GatherAxis(a, indices Array, axis int, sliceSizes []int, s Stream) (Array, error)
	ArgMax(a Array, keepdims bool, s Stream) (Array, error)
	ArgMaxAxis(a Array, axis int, keepdims bool, s Stream) (Array, error)
	ArgPartitionAxis(a Array, kth, axis int, s Stream) (Array, error)
	TakeAlongAxis(a, indices Array, axis int, s Stream) (Array, error)

	// Convolution
	Conv1D(input, weight Array, stride, padding, dilation, groups int, s Stream) (Array, error)

	// Quantization
	Quantize(w Array, groupSize, bits int, mode string, s Stream) ([]Array, error)
	QuantizedMatMul(x, w, scales Array, biases Array, transpose bool, groupSize, bits int, mode string, s Stream) (Array, error)
	GatherQuantizedMatMul(x, w, scales, biases, lhsIndices, rhsIndices Array, transpose bool, groupSize, bits int, mode string, sortedIndices bool, s Stream) (Array, error)
	Dequantize(w, scales, biases Array, groupSize, bits int, mode string, s Stream) (Array, error)

	// Memory management
	SetCacheLimit(bytes uint64) error
	SetMemoryLimit(bytes uint64) error
	ClearCache() error
	TotalSystemRAM() uint64

	// AsyncEvalBatch schedules multiple arrays for evaluation in a single
	// dispatch, rather than each array paying its own graph-walk/encode cost
	// via a separate AsyncEval call. Backends without native batching may
	// fall back to evaluating each array in turn. Empty input is a no-op.
	AsyncEvalBatch(arrays []Array) error

	// Compilation
	EnableCompile() error

	// Capability
	NativeQuantization() bool
}
