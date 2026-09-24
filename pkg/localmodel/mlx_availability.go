//go:build darwin && arm64 && cgo

package localmodel

import "github.com/sprout-foundry/sinter/mlx"

// MLXAvailable reports whether the MLX C library was found at runtime.
// MLX is optional: sprout works without it, only the local LLM backend
// needs it (brew install mlx-c).
func MLXAvailable() bool { return mlx.Available() }
