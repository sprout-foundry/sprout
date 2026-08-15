//go:build darwin && arm64 && cgo

package mlx

/*
#include <stdlib.h>
#include <mlx/c/closure.h>
#include <mlx/c/compile.h>
#include <mlx/c/metal.h>
#include <mlx/c/vector.h>

// Defined in mlx_closure_shim.c; simple entry points around the C closure
// API (cgo cannot bind the function-pointer variants directly).
mlx_closure mlx_go_closure_create(int id);
mlx_vector_array mlx_go_vector_array_new(const mlx_array* data, size_t size);
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// ClosureFunc is a Go implementation of an MLX closure: a pure function from
// a list of arrays to a list of arrays. It must not capture mutable Go state
// that changes the graph structure between calls — the compiled path runs it
// once on placeholder inputs to capture the graph and never calls it again.
type ClosureFunc func(inputs []*Array) ([]*Array, error)

// Closure wraps an mlx_closure handle (an mlx::core::Closure). Apply runs it
// on a vector of input arrays; Compile wraps it in a compiled closure.
type Closure struct {
	handle   C.mlx_closure
	id       int      // registry id carried in the C payload
	template *Closure // for compiled closures: the traced source closure,
	// kept alive because the first apply of a compiled closure runs the
	// original body once on placeholder inputs to capture the graph.
}

var (
	closureMu     sync.Mutex
	closureNextID int
	closures      = map[int]ClosureFunc{}
)

// NewClosure registers fn and creates an MLX closure whose body is the C
// shim. The payload is a C-malloc'd copy of the registry id (never a Go
// pointer — MLX may invoke the body from a C-created scheduler thread, where
// passing Go pointers into C is not safe). The id is freed when MLX drops
// the closure.
func NewClosure(fn ClosureFunc) (*Closure, error) {
	if fn == nil {
		return nil, errors.New("mlx: nil closure func")
	}
	closureMu.Lock()
	id := closureNextID
	closureNextID++
	closures[id] = fn
	closureMu.Unlock()

	h := C.mlx_go_closure_create(C.int(id))
	if h.ctx == nil {
		closureMu.Lock()
		delete(closures, id)
		closureMu.Unlock()
		return nil, fmt.Errorf("mlx: mlx_closure_new_func_payload failed: %s", lastMLXError())
	}
	c := &Closure{handle: h, id: id}
	runtime.SetFinalizer(c, (*Closure).free)
	return c, nil
}

func (c *Closure) free() {
	if c == nil || c.handle.ctx == nil {
		return
	}
	// Dropping the C closure frees the payload via its dtor, so the registry
	// entry is the only thing to clean up here. The closureMu snapshot
	// protects the lookup in mlxGoClosureApply.
	closureMu.Lock()
	delete(closures, c.id)
	closureMu.Unlock()
	C.mlx_closure_free(c.handle)
	c.handle = C.mlx_closure{}
	// Compiled closures hold a template ref; release it now that the trace
	// source is no longer needed (its registry entry dies with it).
	if c.template != nil {
		c.template.free()
		c.template = nil
	}
}

// Free explicitly releases the underlying C closure. Safe to call multiple
// times; the finalizer also reclaims it if this is never called.
func (c *Closure) Free() {
	c.free()
}

// Apply runs the closure on inputs and returns the output arrays. The
// returned arrays are new references owned by the caller; Free each when
// done (or let the finalizer reclaim them).
func (c *Closure) Apply(inputs []*Array) ([]*Array, error) {
	if c.handle.ctx == nil {
		return nil, errors.New("mlx: use of freed closure")
	}
	n := len(inputs)
	raw := make([]C.mlx_array, n)
	for i, a := range inputs {
		if a == nil {
			return nil, fmt.Errorf("mlx: closure input %d is nil", i)
		}
		raw[i] = a.cHandle()
	}
	var inVec C.mlx_vector_array
	if n > 0 {
		inVec = C.mlx_go_vector_array_new(&raw[0], C.size_t(n))
	} else {
		inVec = C.mlx_vector_array_new()
	}
	if inVec.ctx == nil {
		return nil, fmt.Errorf("mlx: building closure input vector: %s", lastMLXError())
	}
	defer C.mlx_vector_array_free(inVec)

	var outVec C.mlx_vector_array
	if rc := C.mlx_closure_apply(&outVec, c.handle, inVec); rc != 0 {
		return nil, fmt.Errorf("mlx: closure apply: %s", lastMLXError())
	}
	return vectorToArrays(outVec), nil
}

// Compile returns a compiled version of this closure. With shapeless=false
// the compiled graph is cached per exact input shape; with shapeless=true it
// accepts any shape at the cost of a slightly less optimized graph. The
// returned closure is independent of c — the caller must Free it when done.
func (c *Closure) Compile(shapeless bool) (*Closure, error) {
	if c.handle.ctx == nil {
		return nil, errors.New("mlx: use of freed closure")
	}
	var compiled C.mlx_closure
	rc := C.mlx_compile(&compiled, c.handle, C.bool(shapeless))
	if rc != 0 {
		return nil, fmt.Errorf("mlx: mlx_compile: %s", lastMLXError())
	}
	out := &Closure{handle: compiled, id: -1, template: c}
	runtime.SetFinalizer(out, (*Closure).free)
	return out, nil
}

// CompileMode selects how mlx_compile transforms the traced graph.
type CompileMode int

const (
	// CompileModeDisabled disables compilation entirely.
	CompileModeDisabled CompileMode = 0
	// CompileModeNoSimplify skips algebraic simplification of the graph.
	CompileModeNoSimplify CompileMode = 1
	// CompileModeNoFuse caches the scheduled execution plan but does NOT
	// fuse kernels — each traced op runs its stock kernel with eager-path
	// intermediate precision. Without this, fusion keeps fp32 intermediates
	// where the eager path rounds to bf16 between kernels, changing
	// numerics enough to flip near-tie argmax tokens.
	CompileModeNoFuse CompileMode = 2
	// CompileModeEnabled is MLX's default: simplify + fuse.
	CompileModeEnabled CompileMode = 3
)

// SetCompileMode sets the global transform mode used by Compile.
func SetCompileMode(m CompileMode) error {
	rc := C.mlx_set_compile_mode(C.mlx_compile_mode(m))
	if rc != 0 {
		return fmt.Errorf("mlx: mlx_set_compile_mode: %s", lastMLXError())
	}
	return nil
}

// StartMetalCapture records a Metal GPU trace (a .gputrace bundle) to path.
// Apple's Instruments/Trace utility opens it for per-kernel GPU timing.
// Diagnostic only — used to attribute decode-step GPU cost per kernel.
func StartMetalCapture(path string) error {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	rc := C.mlx_metal_start_capture(cPath)
	if rc != 0 {
		return fmt.Errorf("mlx: mlx_metal_start_capture: %s", lastMLXError())
	}
	return nil
}

// StopMetalCapture ends a capture started by StartMetalCapture.
func StopMetalCapture() error {
	rc := C.mlx_metal_stop_capture()
	if rc != 0 {
		return fmt.Errorf("mlx: mlx_metal_stop_capture: %s", lastMLXError())
	}
	return nil
}

// vectorToArrays converts an owned mlx_vector_array into Go *Array wrappers.
// Each wrapper takes ownership of one ref (the vector holds its own), and
// the vector itself is freed by the caller. The wrappers must be freed by
// the caller — they carry the normal finalizer.
func vectorToArrays(vec C.mlx_vector_array) []*Array {
	n := int(C.mlx_vector_array_size(vec))
	out := make([]*Array, 0, n)
	for i := 0; i < n; i++ {
		var h C.mlx_array
		if rc := C.mlx_vector_array_get(&h, vec, C.size_t(i)); rc != 0 {
			break
		}
		out = append(out, wrap(h))
	}
	C.mlx_vector_array_free(vec)
	return out
}

// wrapBorrowed wraps a C handle whose lifetime is owned by someone else (an
// input vector) and installs no finalizer. The caller must Free it before
// the borrow ends; used only inside the closure callback where the input
// vector outlives the call.
func wrapBorrowed(h C.mlx_array) *Array { return &Array{handle: h} }

//export mlxGoClosureApply
func mlxGoClosureApply(res *C.mlx_vector_array, input C.mlx_vector_array, payload unsafe.Pointer) C.int {
	id := int(*(*C.int)(payload))
	closureMu.Lock()
	fn, ok := closures[id]
	closureMu.Unlock()
	if !ok {
		return 1
	}

	n := int(C.mlx_vector_array_size(input))
	inputs := make([]*Array, 0, n)
	for i := 0; i < n; i++ {
		var h C.mlx_array
		if rc := C.mlx_vector_array_get(&h, input, C.size_t(i)); rc != 0 {
			return rc
		}
		inputs = append(inputs, wrapBorrowed(h))
	}
	// The borrowed wrappers must not outlive the input vector; drop them
	// (and their underlying extra refs) as soon as fn returns.
	defer func() {
		for _, a := range inputs {
			if a != nil {
				a.Free()
			}
		}
	}()

	outputs, err := fn(inputs)
	if err != nil {
		return 1
	}
	raw := make([]C.mlx_array, len(outputs))
	for i, o := range outputs {
		if o == nil {
			return 1
		}
		raw[i] = o.cHandle()
	}
	if len(raw) == 0 {
		*res = C.mlx_vector_array_new()
		return 0
	}
	*res = C.mlx_go_vector_array_new(&raw[0], C.size_t(len(raw)))
	if res.ctx == nil {
		return 1
	}
	return 0
}
