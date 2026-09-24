//go:build !darwin || !arm64 || !cgo

package localmodel

// MLXAvailable is false on non-MLX platforms and cgo-off builds.
func MLXAvailable() bool { return false }
