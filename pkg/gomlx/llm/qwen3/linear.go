//go:build darwin && arm64 && cgo && mlx

package qwen3

import (
	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// linear is a compatibility alias for the shared llm.Linear projection
// weight (full-precision or quantized). Keeping the local name avoids
// touching every call site in this package.
type linear = llm.Linear

func loadLinear(sf *llm.SafetensorsFile, name string, s *mlx.Stream, quant *llm.QuantConfig) (*linear, error) {
	return llm.LoadLinear(sf, name, s, quant)
}
