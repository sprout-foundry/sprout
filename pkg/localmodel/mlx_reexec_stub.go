//go:build !darwin || !arm64 || !cgo

package localmodel

import "errors"

// RuntimeSupported is false where MLX is unsupported.
func RuntimeSupported() bool { return false }

// MaybeReexecWithBundledRuntime is a no-op where MLX is unsupported.
func MaybeReexecWithBundledRuntime() {}

// RestartWithRuntime is unsupported where MLX is unsupported.
func RestartWithRuntime() error {
	return errors.New("local models need an Apple Silicon Mac")
}
