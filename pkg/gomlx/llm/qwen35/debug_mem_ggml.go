//go:build linux && arm64 && cgo && ggml

package qwen35

// logPrefillLayerMem is a no-op on GGML builds: MLX's allocator accounting
// (see debug_mem_darwin.go) has no GGML equivalent wired up here.
func logPrefillLayerMem(layerIdx int, kind string, seqLen int) {}
