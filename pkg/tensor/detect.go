package tensor

import "runtime"

// DetectBackend returns the best available tensor backend for this machine,
// or nil if no GPU backend is available.
//
// Detection order:
//  1. metal — Apple Silicon (darwin/arm64) with MLX build tag
//  2. Future: cuda, rocm, vulkan via GGML
//  3. nil — no backend (caller falls back to cloud providers)
func DetectBackend() Backend {
	// The metal backend is registered via init() in pkg/gomlx/mlx/backend.go
	// (build tag: darwin && arm64 && cgo && mlx). On other platforms the
	// registry is empty and this returns nil.
	for _, b := range registeredBackends {
		if b.Available() {
			return b
		}
	}
	return nil
}

// registeredBackends is populated by init() in each backend package.
var registeredBackends []Backend

// RegisterBackend adds a backend to the detection list. Called by init()
// in metal/cuda/rocm/vulkan packages.
func RegisterBackend(b Backend) {
	registeredBackends = append(registeredBackends, b)
}

// PlatformSupported reports whether the current platform could support any
// GPU backend at all. Used by the UI to decide whether to show "Local (Offline)".
func PlatformSupported() bool {
	switch runtime.GOOS {
	case "darwin":
		return runtime.GOARCH == "arm64"
	case "linux", "windows":
		return true // could have CUDA/ROCm/Vulkan
	default:
		return false
	}
}
