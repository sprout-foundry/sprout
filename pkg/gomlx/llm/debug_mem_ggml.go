//go:build linux && arm64 && cgo && ggml

package llm

// logGenMem is a no-op on GGML builds — see debug_mem_darwin.go.
func logGenMem(tag string) {}
