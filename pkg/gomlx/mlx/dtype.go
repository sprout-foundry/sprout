//go:build darwin && arm64 && cgo && mlx

package mlx

import "github.com/sprout-foundry/sprout/pkg/tensor"

// Dtype is the element type of an MLX array, aliased to tensor.Dtype so that
// mlx.Array satisfies tensor.Array without adapter wrappers.
type Dtype = tensor.Dtype
