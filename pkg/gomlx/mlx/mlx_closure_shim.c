//go:build darwin && arm64 && cgo

// C shim for MLX closures and compilation. cgo cannot bind C functions that
// take function-pointer arguments (mlx_closure_new_func_payload) or
// pointer-to-array arguments (mlx_vector_array_new_data), so all of those
// calls live here behind simple C entry points. cgo compiles package .c
// files after generating _cgo_export.h, so this file can reference the
// exported Go callback mlxGoClosureApply.
#include "_cgo_export.h"
#include <mlx/c/closure.h>
#include <mlx/c/compile.h>
#include <mlx/c/vector.h>
#include <stdlib.h>

// mlx_go_closure_body is the closure body registered with mlx-c. MLX calls
// it with input arrays, an output vector to populate, and the opaque payload
// we passed at closure creation. The payload is a C-malloc'd copy of an int
// registry ID (never a Go pointer), so the Go callback can look up the real
// closure implementation.
static int mlx_go_closure_body(
    mlx_vector_array* res,
    const mlx_vector_array input,
    void* payload) {
  return mlxGoClosureApply(res, input, payload);
}

// Create an mlx closure whose body calls the Go callback. id is the registry
// ID; the payload is a malloc'd copy that MLX frees via the dtor.
mlx_closure mlx_go_closure_create(int id) {
  int* payload = (int*)malloc(sizeof(int));
  if (payload == NULL) {
    return (mlx_closure){NULL};
  }
  *payload = id;
  return mlx_closure_new_func_payload(
      mlx_go_closure_body, payload, free);
}

// Build an mlx_vector_array from a C array of mlx_array handles.
mlx_vector_array mlx_go_vector_array_new(const mlx_array* data, size_t size) {
  return mlx_vector_array_new_data(data, size);
}
