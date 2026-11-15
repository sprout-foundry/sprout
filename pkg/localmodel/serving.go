//go:build darwin && arm64 && cgo

package localmodel

import (
	"fmt"

	"github.com/sprout-foundry/sinter/llm"
)

// LoadServingModel loads the model in dir to serve it over HTTP (`sprout gpu`
// with the sinter engine), with the same memory limits and corrupt-install
// guard the in-process provider applies. The caller closes it.
func LoadServingModel(dir string) (*llm.Model, error) {
	if !MLXAvailable() {
		return nil, fmt.Errorf("the MLX runtime is not loaded: install it with `brew install mlx-c`, or let `sprout gpu link --sinter` download it, then start again")
	}
	if err := llm.ApplyMemoryLimits(); err != nil {
		return nil, fmt.Errorf("apply memory limits: %w", err)
	}
	if !validModelConfig(dir) {
		return nil, fmt.Errorf("refusing to load %s: config.json is missing, empty, or invalid — re-download the model", dir)
	}
	m, err := llm.NewModel(dir)
	if err != nil {
		return nil, fmt.Errorf("load model from %s: %w", dir, err)
	}
	return m, nil
}
