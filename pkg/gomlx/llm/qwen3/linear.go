//go:build arm64 && cgo && (darwin || (linux && ggml))

package qwen3

import (
	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// linear is a compatibility alias for the shared llm.Linear projection
// weight (full-precision or quantized). Keeping the local name avoids
// touching every call site in this package.
type linear = llm.Linear

func loadLinear(sf *llm.SafetensorsFile, name string, b tensor.Backend, s tensor.Stream, quant *llm.QuantConfig) (*linear, error) {
	return llm.LoadLinear(sf, name, b, s, quant)
}
