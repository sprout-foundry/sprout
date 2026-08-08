//go:build darwin && arm64 && cgo && ggml

// Package ggml provides CGO bindings to the GGML tensor library, enabling
// GPU-accelerated compute on Metal (macOS), CUDA (NVIDIA), ROCm (AMD), and
// Vulkan (portable) behind one C API.
//
// Unlike MLX (eager evaluation), GGML uses lazy graph evaluation: ops build
// a compute graph that is run in a single batch by the backend. This package
// bridges GGML's graph model to the tensor.Backend interface's eager semantics
// by building single-op graphs under the hood.
package ggml

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lggml -lggml-base -framework Metal -framework Foundation
#include <stdlib.h>
#include <string.h>
#include <ggml.h>
#include <ggml-backend.h>
#include <ggml-alloc.h>

// Helper: create a ggml_init_params struct for Go.
struct ggml_init_params ggml_make_params(size_t mem_size) {
    struct ggml_init_params params = {
        .mem_size = mem_size,
        .mem_buffer = NULL,
        .no_alloc = true, // backend allocates; ctx just holds tensor metadata
    };
    return params;
}

// Helper: init best available backend.
// GGML uses dynamic plugin loading — backends (Metal, CUDA, etc.) are in
// .so/.dylib files loaded at runtime by ggml_backend_load_all().
ggml_backend_t ggml_try_metal_backend() {
    // Load all backend plugins from the default search paths
    ggml_backend_load_all();
    // init_best picks the first available GPU backend, or CPU as fallback
    return ggml_backend_init_best();
}

// Helper: init best available backend.
ggml_backend_t ggml_try_best_backend() {
    return ggml_backend_init_best();
}

// Helper: get backend name.
const char * ggml_backend_name_cgo(ggml_backend_t backend) {
    return ggml_backend_name(backend);
}

// Helper: check if backend is metal.
bool ggml_backend_is_metal(ggml_backend_t backend) {
    const char * name = ggml_backend_name(backend);
    return name != NULL && strstr(name, "Metal") != NULL;
}

// Helper: create a tensor and set its data from a Go float32 buffer.
struct ggml_tensor * ggml_new_f32_tensor_2d(
    struct ggml_context * ctx,
    int ne0, int ne1
) {
    return ggml_new_tensor_2d(ctx, GGML_TYPE_F32, ne0, ne1);
}

// (C helpers for graph building are inline above; the Go MatMulF32 method
// uses the GGML API directly via cgo for full control over the lifecycle.)
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Backend wraps a ggml_backend_t handle.
type Backend struct {
	backend C.ggml_backend_t
	ctx     unsafe.Pointer // *C.struct_ggml_context (opaque)
	name    string
}

// InitMetal creates a GGML Metal backend. Returns error if Metal is unavailable.
func InitMetal() (*Backend, error) {
	b := C.ggml_try_metal_backend()
	if b == nil {
		return nil, fmt.Errorf("ggml: Metal backend not available")
	}

	params := C.ggml_make_params(256 * 1024 * 1024) // 256MB metadata arena
	ctx := C.ggml_init(params)
	if ctx == nil {
		C.ggml_backend_free(b)
		return nil, fmt.Errorf("ggml: failed to init context")
	}

	name := C.GoString(C.ggml_backend_name_cgo(b))
	return &Backend{backend: b, ctx: unsafe.Pointer(ctx), name: name}, nil
}

// Name returns the backend name (e.g. "Metal", "CPU").
func (b *Backend) Name() string { return b.name }

// Free releases all GGML resources.
func (b *Backend) Free() {
	if b.ctx != nil {
		C.ggml_free((*C.struct_ggml_context)(b.ctx))
		b.ctx = nil
	}
	if b.backend != nil {
		C.ggml_backend_free(b.backend)
		b.backend = nil
	}
}

// MatMulF32 computes C = A @ B for float32 matrices.
// A is [M, K], B is [K, N], result C is [M, N]. All row-major.
func (b *Backend) MatMulF32(a, c []float32, M, K, N int) ([]float32, error) {
	if len(a) != M*K || len(c) != K*N {
		return nil, fmt.Errorf("ggml: dimension mismatch: A[%d*%d] B[%d*%d]", M, K, K, N)
	}

	ctx := (*C.struct_ggml_context)(b.ctx)

	// GGML mul_mat: ggml_mul_mat(ctx, A, B) computes B @ A^T where
	// A->ne[0] == B->ne[0] = shared K dimension. Result has ne[0]=A->ne[1],
	// ne[1]=B->ne[1]. For C[M,N] = A[M,K] @ B[K,N]:
	//   A: ne[0]=K, ne[1]=M → our row-major [M,K] maps directly
	//   B: ne[0]=K, ne[1]=N → needs K contiguous; our row-major [K,N] has N
	//     contiguous, so transpose the data.
	// Result: ne[0]=M, ne[1]=N → column-major [M,N], transpose to read.

	bTransposed := make([]float32, K*N)
	for k := 0; k < K; k++ {
		for n := 0; n < N; n++ {
			bTransposed[n*K+k] = c[k*N+n]
		}
	}

	aTensor := C.ggml_new_f32_tensor_2d(ctx, C.int(K), C.int(M))
	bTensor := C.ggml_new_f32_tensor_2d(ctx, C.int(K), C.int(N))

	// Build graph: result = mul_mat(a, b)
	resultTensor := C.ggml_mul_mat(ctx, aTensor, bTensor)
	if resultTensor == nil {
		return nil, fmt.Errorf("ggml: ggml_mul_mat returned NULL")
	}

	graph := C.ggml_new_graph(ctx)
	C.ggml_build_forward_expand(graph, resultTensor)

	// Allocate buffers for all tensors in the graph via the graph allocator
	buft := C.ggml_backend_get_default_buffer_type(b.backend)
	alloc := C.ggml_gallocr_new(buft)
	if C.ggml_gallocr_alloc_graph(alloc, graph) == false {
		C.ggml_gallocr_free(alloc)
		return nil, fmt.Errorf("ggml: graph allocation failed")
	}

	// Set input data (after allocation, so tensors have buffers)
	C.ggml_backend_tensor_set(aTensor, unsafe.Pointer(&a[0]), 0, C.size_t(M*K*4))
	C.ggml_backend_tensor_set(bTensor, unsafe.Pointer(&bTransposed[0]), 0, C.size_t(K*N*4))

	// Compute
	status := C.ggml_backend_graph_compute(b.backend, graph)
	if status != C.GGML_STATUS_SUCCESS {
		C.ggml_gallocr_free(alloc)
		return nil, fmt.Errorf("ggml: graph compute failed (status %d)", int(status))
	}

	// Read result. GGML result has ne[0]=M, ne[1]=N → column-major [M,N].
	// Convert to row-major [M,N] by transposing.
	rawData := make([]float32, M*N)
	C.ggml_backend_tensor_get(resultTensor, unsafe.Pointer(&rawData[0]), 0, C.size_t(M*N*4))

	C.ggml_gallocr_free(alloc)

	outData := make([]float32, M*N)
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			outData[m*N+n] = rawData[m+n*M] // column-major to row-major
		}
	}
	return outData, nil
}
