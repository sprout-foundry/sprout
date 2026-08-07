//go:build darwin && arm64 && cgo && mlx

// C shim for installing the Go error handler. cgo compiles package .c files
// after generating _cgo_export.h, so this file can safely reference the
// exported mlxGoErrorHandler while the inline C preamble cannot.
#include "_cgo_export.h"

void mlx_go_error_handler_shim(const char* msg, void* data) {
	mlxGoErrorHandler((char*)msg, data);
}
