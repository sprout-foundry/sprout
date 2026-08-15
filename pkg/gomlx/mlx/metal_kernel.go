//go:build darwin && arm64 && cgo

package mlx

/*
#include <stdlib.h>
#include <mlx/c/array.h>
#include <mlx/c/fast.h>
#include <mlx/c/stream.h>
#include <mlx/c/vector.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// MetalKernel wraps an mlx_fast_metal_kernel: a user-supplied Metal compute
// kernel that MLX compiles and dispatches as a single fused launch. This is
// the mechanism mlx-lm uses for its custom fused ops (e.g. the Gated DeltaNet
// scan), where a per-step Python ops loop would be order-of-magnitude slower.
//
// The kernel source is a Metal Shading Language function body. Template
// parameters (declared via AddTemplateArg*) and input arrays (declared via
// NewMetalKernel's inputNames) are resolved by MLX before compilation. Outputs
// are declared via MetalKernelConfig.AddOutputArg and returned by Apply in
// declaration order.
type MetalKernel struct {
	handle C.mlx_fast_metal_kernel
}

// NewMetalKernel creates a fused Metal kernel from MSL source. inputNames and
// outputNames must match the buffer parameters referenced by source.
//
// ensureRowContiguous makes MLX insert a contiguous() pass on inputs that are
// not already row-contiguous (important for sliced/transposed arrays).
// atomicOutputs is for kernels that write outputs atomically; the DeltaNet
// kernel does not, so pass false.
func NewMetalKernel(name string, inputNames, outputNames []string, source string, ensureRowContiguous bool, atomicOutputs bool) (*MetalKernel, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	cSource := C.CString(source)
	defer C.free(unsafe.Pointer(cSource))

	// The C++ metal_kernel takes the header as a std::string — a NULL CString
	// would invoke the std::string(const char*) constructor on NULL, which is
	// UB and crashes. Always pass an empty (non-NULL) string.
	cHeader := C.CString("")
	defer C.free(unsafe.Pointer(cHeader))

	inVec := cStringVector(inputNames)
	defer C.mlx_vector_string_free(inVec)
	outVec := cStringVector(outputNames)
	defer C.mlx_vector_string_free(outVec)

	h := C.mlx_fast_metal_kernel_new(cName, inVec, outVec, cSource, cHeader, C.bool(ensureRowContiguous), C.bool(atomicOutputs))
	if h.ctx == nil {
		if msg := lastMLXError(); msg != "" {
			return nil, fmt.Errorf("mlx: metal kernel %q: %s", name, msg)
		}
		return nil, fmt.Errorf("mlx: metal kernel %q creation failed", name)
	}
	return &MetalKernel{handle: h}, nil
}

// Free releases the compiled kernel.
func (k *MetalKernel) Free() {
	if k == nil || k.handle.ctx == nil {
		return
	}
	C.mlx_fast_metal_kernel_free(k.handle)
	k.handle.ctx = nil
}

// MetalKernelConfig holds per-invocation kernel settings: output shapes,
// grid/threadgroup sizes, and template parameter values.
type MetalKernelConfig struct {
	handle C.mlx_fast_metal_kernel_config
}

// NewMetalKernelConfig creates an empty config. Configure it with
// AddOutputArg, SetGrid, SetThreadGroup, and AddTemplateArg* before calling
// Apply.
func NewMetalKernelConfig() *MetalKernelConfig {
	return &MetalKernelConfig{handle: C.mlx_fast_metal_kernel_config_new()}
}

// Free releases the config.
func (c *MetalKernelConfig) Free() {
	if c == nil || c.handle.ctx == nil {
		return
	}
	C.mlx_fast_metal_kernel_config_free(c.handle)
	c.handle.ctx = nil
}

// AddOutputArg declares a kernel output buffer with the given shape and dtype.
// Outputs are returned from Apply in the order they were declared.
func (c *MetalKernelConfig) AddOutputArg(shape []int, dtype Dtype) error {
	cShape, _ := cIntPtrs(shape)
	rc := C.mlx_fast_metal_kernel_config_add_output_arg(c.handle, cShape, C.size_t(len(shape)), C.mlx_dtype(dtype))
	return checkRC(rc, "metal kernel config add_output_arg")
}

// SetGrid sets the launch grid (threads per dimension).
func (c *MetalKernelConfig) SetGrid(g1, g2, g3 int) error {
	rc := C.mlx_fast_metal_kernel_config_set_grid(c.handle, C.int(g1), C.int(g2), C.int(g3))
	return checkRC(rc, "metal kernel config set_grid")
}

// SetThreadGroup sets the threadgroup size per dimension.
func (c *MetalKernelConfig) SetThreadGroup(t1, t2, t3 int) error {
	rc := C.mlx_fast_metal_kernel_config_set_thread_group(c.handle, C.int(t1), C.int(t2), C.int(t3))
	return checkRC(rc, "metal kernel config set_thread_group")
}

// SetInitValue sets the initial value for output buffers (default 0).
func (c *MetalKernelConfig) SetInitValue(v float32) error {
	rc := C.mlx_fast_metal_kernel_config_set_init_value(c.handle, C.float(v))
	return checkRC(rc, "metal kernel config set_init_value")
}

// AddTemplateArgDtype binds a template parameter to an MLX dtype (e.g. "InT"
// or "StT" for the DeltaNet kernel's input/state types).
func (c *MetalKernelConfig) AddTemplateArgDtype(name string, dtype Dtype) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	rc := C.mlx_fast_metal_kernel_config_add_template_arg_dtype(c.handle, cName, C.mlx_dtype(dtype))
	return checkRC(rc, "metal kernel config add_template_arg_dtype")
}

// AddTemplateArgInt binds a template parameter to an integer constant (e.g.
// "Dk", "Dv", "Hk", "Hv" for the DeltaNet kernel's shape constants).
func (c *MetalKernelConfig) AddTemplateArgInt(name string, value int) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	rc := C.mlx_fast_metal_kernel_config_add_template_arg_int(c.handle, cName, C.int(value))
	return checkRC(rc, "metal kernel config add_template_arg_int")
}

// AddTemplateArgBool binds a template parameter to a boolean constant.
func (c *MetalKernelConfig) AddTemplateArgBool(name string, value bool) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))
	rc := C.mlx_fast_metal_kernel_config_add_template_arg_bool(c.handle, cName, C.bool(value))
	return checkRC(rc, "metal kernel config add_template_arg_bool")
}

// Apply dispatches the kernel. inputs must match the kernel's inputNames in
// order. It returns the output arrays in the order they were declared via
// AddOutputArg. The caller owns each returned Array.
func (k *MetalKernel) Apply(inputs []*Array, config *MetalKernelConfig, s *Stream) ([]*Array, error) {
	if k == nil || k.handle.ctx == nil {
		return nil, fmt.Errorf("mlx: metal kernel not initialized")
	}
	cHandles := make([]C.mlx_array, len(inputs))
	for i, arr := range inputs {
		cHandles[i] = arr.cHandle()
	}
	var inVec C.mlx_vector_array
	if len(cHandles) > 0 {
		inVec = C.mlx_vector_array_new_data(&cHandles[0], C.size_t(len(cHandles)))
	} else {
		inVec = C.mlx_vector_array_new()
	}
	defer C.mlx_vector_array_free(inVec)

	var outVec C.mlx_vector_array
	rc := C.mlx_fast_metal_kernel_apply(&outVec, k.handle, inVec, config.handle, s.cHandle())
	if rc != 0 {
		C.mlx_vector_array_free(outVec)
		if msg := lastMLXError(); msg != "" {
			return nil, fmt.Errorf("mlx: metal kernel apply: %s", msg)
		}
		return nil, fmt.Errorf("mlx: metal kernel apply failed (rc=%d)", rc)
	}
	defer C.mlx_vector_array_free(outVec)

	n := int(C.mlx_vector_array_size(outVec))
	results := make([]*Array, n)
	for i := 0; i < n; i++ {
		elem := C.mlx_array_new()
		grc := C.mlx_vector_array_get(&elem, outVec, C.size_t(i))
		if grc != 0 {
			C.mlx_array_free(elem)
			for _, r := range results {
				if r != nil {
					r.Free()
				}
			}
			return nil, fmt.Errorf("mlx: metal kernel apply: extract output %d failed (rc=%d)", i, grc)
		}
		results[i] = wrap(elem)
	}
	return results, nil
}

// cStringVector builds a mlx_vector_string from Go strings. The caller must
// free the result with mlx_vector_string_free.
func cStringVector(names []string) C.mlx_vector_string {
	if len(names) == 0 {
		return C.mlx_vector_string_new()
	}
	ptrs := make([]*C.char, len(names))
	for i, n := range names {
		ptrs[i] = C.CString(n)
	}
	// new_data copies the C strings, so the temporary CStrings can be freed
	// immediately after.
	vec := C.mlx_vector_string_new_data(&ptrs[0], C.size_t(len(names)))
	for _, p := range ptrs {
		C.free(unsafe.Pointer(p))
	}
	return vec
}
