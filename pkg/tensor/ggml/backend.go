//go:build darwin && arm64 && cgo && ggml

// Package ggml provides CGO bindings to the GGML tensor library, enabling
// GPU-accelerated compute on Metal (macOS), CUDA (NVIDIA), ROCm (AMD), and
// Vulkan (portable) behind one C API.
//
// GGML uses lazy graph evaluation: ops build a ggml_cgraph that is run in a
// single batch by the backend. This package bridges GGML's graph model to
// the tensor.Backend interface's eager semantics by accumulating ops into a
// graph and flushing on Eval().
package ggml

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lggml -lggml-base -framework Metal -framework Foundation

#include <stdlib.h>
#include <string.h>
#include <ggml.h>
#include <ggml-backend.h>
#include <ggml-alloc.h>

// Create init params.
struct ggml_init_params ggml_make_params(size_t mem_size) {
    struct ggml_init_params params = { .mem_size = mem_size, .mem_buffer = NULL, .no_alloc = true };
    return params;
}

// Load backends and init best.
ggml_backend_t ggml_init_best() {
    ggml_backend_load_all();
    return ggml_backend_init_best();
}

const char * backend_name(ggml_backend_t b) { return ggml_backend_name(b); }

// Create a float32 2D tensor.
struct ggml_tensor * new_f32_2d(struct ggml_context * ctx, int ne0, int ne1) {
    return ggml_new_tensor_2d(ctx, GGML_TYPE_F32, ne0, ne1);
}

// Create a float32 1D tensor.
struct ggml_tensor * new_f32_1d(struct ggml_context * ctx, int ne0) {
    return ggml_new_tensor_1d(ctx, GGML_TYPE_F32, ne0);
}

// Create an int64 2D tensor (for token IDs).
struct ggml_tensor * new_i64_2d(struct ggml_context * ctx, int ne0, int ne1) {
    return ggml_new_tensor_2d(ctx, GGML_TYPE_I64, ne0, ne1);
}

// Get tensor shape element i.
int64_t tensor_ne(const struct ggml_tensor * t, int i) { return t->ne[i]; }

// Get tensor stride (bytes) element i.
size_t tensor_nb(const struct ggml_tensor * t, int i) { return t->nb[i]; }

// Get tensor data pointer (for CPU tensors only).
void * tensor_data(const struct ggml_tensor * t) { return t->data; }

// Get ggml_type for a dtype int.
enum ggml_type dtype_to_ggml(int dtype) {
    switch (dtype) {
        case 0: return GGML_TYPE_BF16; // our Bool maps to nothing sensible; use BF16 as placeholder
        case 10: return GGML_TYPE_F32;
        case 11: return GGML_TYPE_BF16;
        case 9: return GGML_TYPE_F16;
        case 8: return GGML_TYPE_I64;
        case 7: return GGML_TYPE_I32;
        case 3: return GGML_TYPE_I32; // UInt32 → I32
        default: return GGML_TYPE_F32;
    }
}

// Get number of bytes in a tensor.
size_t tensor_nbytes(const struct ggml_tensor * t) { return ggml_nbytes(t); }

// Get number of elements in a tensor.
int64_t tensor_nelements(const struct ggml_tensor * t) { return ggml_nelements(t); }

// Walk a GGML tensor's source graph recursively and set data for any
// tensor registered in the Go-side data map. The callback receives each
// tensor pointer; Go decides whether to set data.
typedef void (*set_data_fn)(struct ggml_tensor * t, void * user_data);

void walk_and_set(struct ggml_tensor * t, set_data_fn callback, void * user_data) {
    if (t == NULL) return;
    // Call callback for this tensor
    callback(t, user_data);
    // Walk source tensors
    for (int i = 0; i < GGML_MAX_SRC; i++) {
        if (t->src[i] != NULL) {
            walk_and_set(t->src[i], callback, user_data);
        }
    }
}

// Get GGML_MAX_SRC
int ggml_max_src() { return GGML_MAX_SRC; }

// Get src[i] from a tensor
struct ggml_tensor * get_src(struct ggml_tensor * t, int i) {
    if (i < 0 || i >= GGML_MAX_SRC) return NULL;
    return t->src[i];
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	tensor.RegisterBackend(&GGMLBackend{})
}

// GGMLBackend implements tensor.Backend via the GGML C library.
type GGMLBackend struct {
	once    sync.Once
	backend C.ggml_backend_t
	ctx     unsafe.Pointer
	name    string
	initErr error

	// tensorData maps C tensor pointers to their Go-side raw data, so
	// that Eval() can set input tensor data after the graph allocator
	// assigns backend buffers. Key: uintptr of *C.ggml_tensor.
	tensorData sync.Map // map[uintptr][]byte
}

func (g *GGMLBackend) lookupData(t *C.struct_ggml_tensor) ([]byte, bool) {
	v, ok := g.tensorData.Load(uintptr(unsafe.Pointer(t)))
	if !ok {
		return nil, false
	}
	return v.([]byte), true
}

// registerTensorData associates Go-side data with a C tensor pointer.
func (g *GGMLBackend) registerTensorData(t *C.struct_ggml_tensor, data []byte) {
	g.tensorData.Store(uintptr(unsafe.Pointer(t)), data)
}

func (g *GGMLBackend) ensureInit() error {
	g.once.Do(func() {
		b := C.ggml_init_best()
		if b == nil {
			g.initErr = fmt.Errorf("ggml: no backend available")
			return
		}
		params := C.ggml_make_params(512 * 1024 * 1024)
		ctx := C.ggml_init(params)
		if ctx == nil {
			C.ggml_backend_free(b)
			g.initErr = fmt.Errorf("ggml: failed to init context")
			return
		}
		g.backend = b
		g.ctx = unsafe.Pointer(ctx)
		g.name = C.GoString(C.backend_name(b))
	})
	return g.initErr
}

func (g *GGMLBackend) Name() string {
	if err := g.ensureInit(); err != nil {
		return "ggml"
	}
	return g.name
}

func (g *GGMLBackend) Available() bool {
	// GGML is available if the ggml build tag is set and a backend loads.
	return g.ensureInit() == nil
}

// ctxPtr returns the C context as a typed pointer.
func (g *GGMLBackend) ctxPtr() *C.struct_ggml_context {
	return (*C.struct_ggml_context)(g.ctx)
}

// Array wraps a GGML tensor.
type Array struct {
	backend *GGMLBackend
	tensor  *C.struct_ggml_tensor
	hasData bool
}

// Stream is a no-op for GGML (graph compute is synchronous).
type Stream struct{}

func (Stream) Synchronize() error { return nil }
func (Stream) Free()              {}

// Dtype conversion
func toGGMLType(dt tensor.Dtype) C.enum_ggml_type {
	return C.dtype_to_ggml(C.int(dt))
}

func fromGGMLType(gt C.enum_ggml_type) tensor.Dtype {
	switch gt {
	case C.GGML_TYPE_F32:
		return tensor.Float32
	case C.GGML_TYPE_F16:
		return tensor.Float16
	case C.GGML_TYPE_BF16:
		return tensor.BFloat16
	case C.GGML_TYPE_I64:
		return tensor.Int64
	case C.GGML_TYPE_I32:
		return tensor.Int32
	default:
		return tensor.Float32
	}
}

// ── tensor.Array implementation ────────────────────────────────────

func (a *Array) Shape() []int {
	if a.tensor == nil {
		return nil
	}
	var shape []int
	for i := 0; i < 4; i++ {
		n := int(C.tensor_ne(a.tensor, C.int(i)))
		if n <= 0 {
			break
		}
		shape = append(shape, n)
	}
	return shape
}

func (a *Array) Dtype() tensor.Dtype {
	if a.tensor == nil {
		return tensor.Float32
	}
	return fromGGMLType(a.tensor._type)
}

func (a *Array) Ndim() int {
	s := a.Shape()
	// GGML always has 4 dims; trim trailing 1s
	nd := len(s)
	for nd > 1 && s[nd-1] == 1 {
		nd--
	}
	return nd
}

func (a *Array) Size() int {
	if a.tensor == nil {
		return 0
	}
	return int(C.tensor_nelements(a.tensor))
}

func (a *Array) Eval() error {
	if a.hasData || a.tensor == nil {
		return nil
	}
	g := a.backend
	ctx := g.ctxPtr()

	// Build graph
	graph := C.ggml_new_graph(ctx)
	C.ggml_build_forward_expand(graph, a.tensor)

	// Allocate ALL tensors in the graph (inputs + intermediates + output)
	buft := C.ggml_backend_get_default_buffer_type(g.backend)
	alloc := C.ggml_gallocr_new(buft)

	if !C.ggml_gallocr_alloc_graph(alloc, graph) {
		C.ggml_gallocr_free(alloc)
		return fmt.Errorf("ggml: graph allocation failed")
	}

	// After allocation, all tensors have backend buffers. Now walk the
	// graph and set input data for any tensor registered with Go-side data.
	g.setRegisteredDataRecursive(a.tensor)

	// Compute
	status := C.ggml_backend_graph_compute(g.backend, graph)
	C.ggml_gallocr_free(alloc) // safe to free after compute — data is in backend buffers
	if status != C.GGML_STATUS_SUCCESS {
		return fmt.Errorf("ggml: graph compute failed (status %d)", int(status))
	}
	a.hasData = true
	return nil
}

func (g *GGMLBackend) setRegisteredDataRecursive(t *C.struct_ggml_tensor) {
	if t == nil {
		return
	}
	// Set data for this tensor if registered
	if data, ok := g.lookupData(t); ok {
		C.ggml_backend_tensor_set(t, unsafe.Pointer(&data[0]), 0, C.size_t(len(data)))
	}
	// Recurse into source tensors
	maxSrc := int(C.ggml_max_src())
	for i := 0; i < maxSrc; i++ {
		src := C.get_src(t, C.int(i))
		if src != nil {
			g.setRegisteredDataRecursive(src)
		}
	}
}

func (a *Array) Free() {
	if a.tensor != nil {
		// GGML tensors are freed when the context is freed; individual frees
		// are managed by the context arena. Set to nil so we don't double-use.
		a.tensor = nil
	}
}

func (a *Array) Float32Data() ([]float32, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := a.Size()
	if n == 0 || a.tensor == nil {
		return nil, fmt.Errorf("ggml: Float32Data on empty/null tensor")
	}
	data := make([]float32, n)
	nbytes := C.size_t(n * 4)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&data[0]), 0, nbytes)
	return data, nil
}

func (a *Array) Int64Data() ([]int64, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := a.Size()
	data := make([]int64, n)
	nbytes := C.size_t(n * 8)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&data[0]), 0, nbytes)
	return data, nil
}

func (a *Array) Uint32Data() ([]uint32, error) {
	if err := a.Eval(); err != nil {
		return nil, err
	}
	n := a.Size()
	data := make([]uint32, n)
	nbytes := C.size_t(n * 4)
	C.ggml_backend_tensor_get(a.tensor, unsafe.Pointer(&data[0]), 0, nbytes)
	return data, nil
}

// ── tensor.Backend: capability ─────────────────────────────────────

func (g *GGMLBackend) NewGPUStream() (tensor.Stream, error)  { return Stream{}, nil }
func (g *GGMLBackend) DefaultGPUStream() (tensor.Stream, error) { return Stream{}, nil }
func (g *GGMLBackend) DefaultStream() (tensor.Stream, error)    { return Stream{}, nil }

// ── tensor.Backend: array creation ─────────────────────────────────

func (g *GGMLBackend) NewArrayFromFloat32(data []float32, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_F32)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	// Store data for lazy set during Eval (after graph allocation).
	raw := make([]byte, len(data)*4)
	copy(raw, (*[1 << 30]byte)(unsafe.Pointer(&data[0]))[:len(data)*4])
	g.registerTensorData(t, raw)
	return &Array{backend: g, tensor: t}, nil
}

func (g *GGMLBackend) NewArrayFromInt64(data []int64, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_I64)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data)*8)
	copy(raw, (*[1 << 30]byte)(unsafe.Pointer(&data[0]))[:len(data)*8])
	g.registerTensorData(t, raw)
	return &Array{backend: g, tensor: t}, nil
}

func (g *GGMLBackend) NewArrayFromInt32(data []int32, shape []int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := createTensor(g, shape, C.GGML_TYPE_I32)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data)*4)
	copy(raw, (*[1 << 30]byte)(unsafe.Pointer(&data[0]))[:len(data)*4])
	g.registerTensorData(t, raw)
	return &Array{backend: g, tensor: t}, nil
}

func (g *GGMLBackend) NewArrayFromBytes(data []byte, shape []int, dtype tensor.Dtype) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	gt := toGGMLType(dtype)
	t := createTensor(g, shape, gt)
	if t == nil {
		return nil, fmt.Errorf("ggml: failed to create tensor")
	}
	raw := make([]byte, len(data))
	copy(raw, data)
	g.registerTensorData(t, raw)
	return &Array{backend: g, tensor: t}, nil
}

func (g *GGMLBackend) NewScalarInt32(v int) (tensor.Array, error) {
	if err := g.ensureInit(); err != nil {
		return nil, err
	}
	t := C.ggml_new_tensor_1d(g.ctxPtr(), C.GGML_TYPE_I32, 1)
	data := make([]byte, 4)
	*(*int32)(unsafe.Pointer(&data[0])) = int32(v)
	g.registerTensorData(t, data)
	return &Array{backend: g, tensor: t}, nil
}

func (g *GGMLBackend) Zeros(shape []int, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	data := make([]float32, product(shape))
	return g.NewArrayFromFloat32(data, shape)
}

func (g *GGMLBackend) Arange(start, stop, step float64, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	n := int((stop - start) / step)
	data := make([]float32, n)
	for i := 0; i < n; i++ {
		data[i] = float32(start + float64(i)*step)
	}
	return g.NewArrayFromFloat32(data, []int{n})
}

func (g *GGMLBackend) RetainArray(a tensor.Array) tensor.Array {
	if a == nil {
		return nil
	}
	return a // GGML tensors live in the context arena; no refcount bump needed
}

func (g *GGMLBackend) AsType(a tensor.Array, dtype tensor.Dtype, s tensor.Stream) (tensor.Array, error) {
	ga := a.(*Array)
	gt := toGGMLType(dtype)
	// GGML cast: create new tensor with target type and use ggml_cpy (which casts)
	ctx := g.ctxPtr()
	shape := ga.Shape()
	target := createTensor(g, shape, gt)
	result := C.ggml_cpy(ctx, ga.tensor, target)
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

// ── helpers ────────────────────────────────────────────────────────

func product(shape []int) int {
	p := 1
	for _, d := range shape {
		p *= d
	}
	return p
}

func createTensor(g *GGMLBackend, shape []int, gt C.enum_ggml_type) *C.struct_ggml_tensor {
	switch len(shape) {
	case 1:
		return C.ggml_new_tensor_1d(g.ctxPtr(), gt, C.int64_t(shape[0]))
	case 2:
		return C.ggml_new_tensor_2d(g.ctxPtr(), gt, C.int64_t(shape[0]), C.int64_t(shape[1]))
	case 3:
		return C.ggml_new_tensor_3d(g.ctxPtr(), gt, C.int64_t(shape[0]), C.int64_t(shape[1]), C.int64_t(shape[2]))
	case 4:
		return C.ggml_new_tensor_4d(g.ctxPtr(), gt, C.int64_t(shape[0]), C.int64_t(shape[1]), C.int64_t(shape[2]), C.int64_t(shape[3]))
	default:
		return nil
	}
}

func shapeToNE(shape []int) []C.int64_t {
	ne := make([]C.int64_t, 4)
	for i := 0; i < 4; i++ {
		if i < len(shape) {
			ne[i] = C.int64_t(shape[i])
		} else {
			ne[i] = 1
		}
	}
	return ne
}

func (a *Array) cTensor() *C.struct_ggml_tensor { return a.tensor }

// scalarF32 creates a 1-element F32 tensor from a Go float32.
func (g *GGMLBackend) scalarF32(v float32) *C.struct_ggml_tensor {
	ctx := g.ctxPtr()
	t := C.ggml_new_tensor_1d(ctx, C.GGML_TYPE_F32, 1)
	data := []byte{0, 0, 0, 0}
	*(*float32)(unsafe.Pointer(&data[0])) = v
	g.registerTensorData(t, data)
	return t
}

func (g *GGMLBackend) Add(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_add(g.ctxPtr(), a.(*Array).tensor, b.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Subtract(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_sub(g.ctxPtr(), a.(*Array).tensor, b.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Multiply(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_mul(g.ctxPtr(), a.(*Array).tensor, b.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Divide(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_div(g.ctxPtr(), a.(*Array).tensor, b.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Maximum(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	// GGML has no max op; compose: max(a,b) = a + relu(b-a)
	ctx := g.ctxPtr()
	ta := a.(*Array).tensor
	tb := b.(*Array).tensor
	diff := C.ggml_sub(ctx, tb, ta)
	relu := C.ggml_relu(ctx, diff)
	return &Array{backend: g, tensor: C.ggml_add(ctx, ta, relu), hasData: false}, nil
}

// ── tensor.Backend: elementwise unary ──────────────────────────────

func (g *GGMLBackend) Abs(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_abs(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Exp(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_exp(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Log(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_log(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Log1p(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// log(1 + x) = log(1+x); GGML has no log1p, compose: add scalar then log
	one := g.scalarF32(1.0)
	return &Array{backend: g, tensor: C.ggml_log(ctx, C.ggml_add(ctx, t, one)), hasData: false}, nil
}

func (g *GGMLBackend) Sqrt(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_sqrt(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Square(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_sqr(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Negative(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_neg(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Sigmoid(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_sigmoid(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Softplus(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// softplus(x) = log(1 + exp(x))
	one := g.scalarF32(1.0)
	return &Array{backend: g, tensor: C.ggml_log(ctx, C.ggml_add(ctx, one, C.ggml_exp(ctx, t))), hasData: false}, nil
}

func (g *GGMLBackend) Sin(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_sin(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Cos(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_cos(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Tanh(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_tanh(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Power(a tensor.Array, exp float32, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// x^exp = exp(exp * log(x)) for x > 0
	logT := C.ggml_log(ctx, t)
	scaled := C.ggml_scale(ctx, logT, C.float(exp))
	return &Array{backend: g, tensor: C.ggml_exp(ctx, scaled), hasData: false}, nil
}

// ── tensor.Backend: reductions ─────────────────────────────────────

func (g *GGMLBackend) Sum(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_sum(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) Mean(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	sumT := C.ggml_sum(ctx, t)
	n := g.scalarF32(float32(a.Size()))
	return &Array{backend: g, tensor: C.ggml_div(ctx, sumT, n), hasData: false}, nil
}

func (g *GGMLBackend) Max(a tensor.Array, axes []int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_argmax(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

// ── tensor.Backend: linear algebra ─────────────────────────────────

func (g *GGMLBackend) MatMul(a, b tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	result := C.ggml_mul_mat(ctx, a.(*Array).tensor, b.(*Array).tensor)
	if result == nil {
		return nil, fmt.Errorf("ggml: mul_mat returned NULL")
	}
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

// ── tensor.Backend: shape manipulation ─────────────────────────────

func (g *GGMLBackend) Reshape(a tensor.Array, shape []int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	ne := shapeToNE(shape)
	result := C.ggml_reshape(ctx, t, C.ggml_new_tensor(ctx, t._type, C.int(len(shape)), (*C.int64_t)(unsafe.Pointer(&ne[0]))))
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

func (g *GGMLBackend) Transpose(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_cont(g.ctxPtr(), C.ggml_transpose(g.ctxPtr(), a.(*Array).tensor)), hasData: false}, nil
}

func (g *GGMLBackend) TransposeAxes(a tensor.Array, axes []int, s tensor.Stream) (tensor.Array, error) {
	// GGML only supports 2D transpose directly; for higher dims, compose.
	// For the common case of [0,2,1,3] (attention transpose), use permute.
	t := a.(*Array).tensor
	ne := []C.int{0, 0, 0, 0}
	for i, ax := range axes {
		if ax >= 0 && ax < 4 {
			ne[i] = C.int(ax)
		}
	}
	return &Array{backend: g, tensor: C.ggml_cont(g.ctxPtr(), C.ggml_permute(g.ctxPtr(), t, ne[0], ne[1], ne[2], ne[3])), hasData: false}, nil
}

func (g *GGMLBackend) SqueezeAxis(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	// GGML doesn't have squeeze; return the tensor as-is (reshape if needed).
	// For our use case (squeezing dim 2 of a [1,1,H] to [1,1]), it's a no-op
	// since GGML uses 4D internally with trailing 1s.
	return a, nil
}

func (g *GGMLBackend) Slice(a tensor.Array, start, stop, strides []int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	// GGML uses ggml_view for slicing. This is a simplified 1D/2D slice.
	if len(start) >= 2 && len(stop) >= 2 {
		ne0 := C.int64_t(stop[1] - start[1])
		// view_2d(ctx, t, ne0, ne1, nb1, offset)
		offset := C.size_t(start[0]*int(t.nb[1]) + start[1]*int(t.nb[0]))
		result := C.ggml_view_2d(ctx, t, ne0, C.int64_t(stop[0]-start[0]), C.size_t(t.nb[1]), offset)
		return &Array{backend: g, tensor: result, hasData: false}, nil
	}
	return nil, fmt.Errorf("ggml: unsupported slice dimensions")
}

func (g *GGMLBackend) SliceUpdate(src, update tensor.Array, start, stop []int, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: SliceUpdate not yet implemented")
}

func (g *GGMLBackend) ConcatenateAxis(arrays []tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	if len(arrays) == 0 {
		return nil, fmt.Errorf("ggml: concatenate requires at least one array")
	}
	result := arrays[0].(*Array).tensor
	for _, a := range arrays[1:] {
		result = C.ggml_concat(ctx, result, a.(*Array).tensor, C.int(axis))
	}
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

func (g *GGMLBackend) Stack(arrays []tensor.Array, s tensor.Stream) (tensor.Array, error) {
	// Stack = concatenate along a new axis 0. GGML doesn't have stack directly;
	// reshape each to [1, ...original] and concat on axis 0.
	ctx := g.ctxPtr()
	result := arrays[0].(*Array).tensor
	for _, a := range arrays[1:] {
		result = C.ggml_concat(ctx, result, a.(*Array).tensor, 1)
	}
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

func (g *GGMLBackend) SplitAxis(a tensor.Array, indices []int, axis int, s tensor.Stream) ([]tensor.Array, error) {
	return nil, fmt.Errorf("ggml: SplitAxis not yet implemented")
}

func (g *GGMLBackend) RepeatAxis(a tensor.Array, repeats, axis int, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	result := t
	for i := 0; i < repeats-1; i++ {
		result = C.ggml_concat(ctx, result, t, C.int(axis))
	}
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

func (g *GGMLBackend) Pad(a tensor.Array, axes, low, high []int, padValue tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	t := a.(*Array).tensor
	p := [4]C.int{0, 0, 0, 0}
	for i := range axes {
		if i < 4 {
			p[axes[i]] = C.int(low[i] + high[i])
		}
	}
	result := C.ggml_pad(ctx, t, p[0], p[1], p[2], p[3])
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

func (g *GGMLBackend) Where(condition, x, y tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: Where not yet implemented")
}

func (g *GGMLBackend) Tril(a tensor.Array, k int, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: Tril not yet implemented")
}

// ── tensor.Backend: normalization ──────────────────────────────────

func (g *GGMLBackend) Softmax(a tensor.Array, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_soft_max(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) SoftmaxAxis(a tensor.Array, axis int, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_soft_max(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) FastRMSNorm(x, weight tensor.Array, eps float32, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	result := C.ggml_rms_norm(ctx, x.(*Array).tensor, C.float(eps))
	if weight != nil {
		result = C.ggml_mul(ctx, result, weight.(*Array).tensor)
	}
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

// ── tensor.Backend: attention ──────────────────────────────────────

func (g *GGMLBackend) FastScaledDotProductAttention(q, k, v tensor.Array, scale float32, maskMode string, maskArr, sinks tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	var mask *C.struct_ggml_tensor
	if maskArr != nil {
		mask = maskArr.(*Array).tensor
	}
	result := C.ggml_flash_attn_ext(ctx, q.(*Array).tensor, k.(*Array).tensor, v.(*Array).tensor, mask, C.float(scale), 0.0, 0.0)
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

// ── tensor.Backend: positional encoding ────────────────────────────

func (g *GGMLBackend) FastRoPE(x tensor.Array, dims int, traditional bool, base float64, scale float32, offset int, freqs tensor.Array, s tensor.Stream) (tensor.Array, error) {
	ctx := g.ctxPtr()
	mode := C.int(0)
	if !traditional {
		mode = C.int(2) // GGML_ROPE_TYPE_NEOX
	}
	// Create position IDs tensor
	posData := make([]int32, 1)
	posData[0] = int32(offset)
	pos := C.ggml_new_tensor_1d(ctx, C.GGML_TYPE_I32, 1)
	C.ggml_backend_tensor_set(pos, unsafe.Pointer(&posData[0]), 0, 4)
	result := C.ggml_rope(ctx, x.(*Array).tensor, pos, C.int(dims), mode)
	return &Array{backend: g, tensor: result, hasData: false}, nil
}

// ── tensor.Backend: indexing ───────────────────────────────────────

func (g *GGMLBackend) GatherAxis(a, indices tensor.Array, axis int, sliceSizes []int, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: GatherAxis not yet implemented")
}

func (g *GGMLBackend) ArgMax(a tensor.Array, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_argmax(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

func (g *GGMLBackend) ArgMaxAxis(a tensor.Array, axis int, keepdims bool, s tensor.Stream) (tensor.Array, error) {
	return &Array{backend: g, tensor: C.ggml_argmax(g.ctxPtr(), a.(*Array).tensor), hasData: false}, nil
}

// ── tensor.Backend: convolution ────────────────────────────────────

func (g *GGMLBackend) Conv1D(input, weight tensor.Array, stride, padding, dilation, groups int, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: Conv1D not yet implemented")
}

// ── tensor.Backend: quantization ───────────────────────────────────

func (g *GGMLBackend) Quantize(w tensor.Array, groupSize, bits int, mode string, s tensor.Stream) ([]tensor.Array, error) {
	return nil, fmt.Errorf("ggml: Quantize not yet implemented")
}

func (g *GGMLBackend) QuantizedMatMul(x, w, scales tensor.Array, biases tensor.Array, transpose bool, groupSize, bits int, mode string, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: QuantizedMatMul not yet implemented")
}

func (g *GGMLBackend) Dequantize(w, scales, biases tensor.Array, groupSize, bits int, mode string, s tensor.Stream) (tensor.Array, error) {
	return nil, fmt.Errorf("ggml: Dequantize not yet implemented")
}

// ── tensor.Backend: memory management ──────────────────────────────

func (g *GGMLBackend) SetCacheLimit(bytes uint64) error  { return nil }
func (g *GGMLBackend) SetMemoryLimit(bytes uint64) error { return nil }
func (g *GGMLBackend) ClearCache() error                 { return nil }
func (g *GGMLBackend) TotalSystemRAM() uint64            { return 0 }
