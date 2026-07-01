//go:build !wasm

package embedding

import "context"

// StaticProviderName returns the name of the static embedding provider.
// On native builds, the static provider is not used (ONNX is preferred),
// but the function exists for API compatibility with WASM builds.
func StaticProviderName() string {
	return "static-vector-fallback"
}

// StaticEmbed is a no-op on native builds. The static provider is only
// used in WASM builds as a fallback when the ONNX bridge isn't available.
func StaticEmbed(_ context.Context, _ string) ([]float32, error) {
	return nil, nil
}
