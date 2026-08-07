//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// ArchitectureFactory creates an Architecture instance from a ModelConfig.
// Factories are registered at init time by each architecture package.
type ArchitectureFactory func(cfg ModelConfig) (Architecture, error)

// architectureFactories maps model_type → factory. Each architecture package
// (e.g. qwen3) registers its factory in an init() function.
var architectureFactories = map[string]ArchitectureFactory{}

// RegisterArchitecture registers a factory for a model_type.
// Called by architecture packages in their init() functions. Panics on
// duplicate registration to catch development errors early.
func RegisterArchitecture(modelType string, factory ArchitectureFactory) {
	if _, exists := architectureFactories[modelType]; exists {
		panic(fmt.Sprintf("llm: architecture %q already registered", modelType))
	}
	architectureFactories[modelType] = factory
}

// createArchitecture looks up the factory for cfg.Arch and creates an instance.
func createArchitecture(cfg ModelConfig) (Architecture, error) {
	factory, ok := architectureFactories[cfg.Arch]
	if !ok {
		return nil, fmt.Errorf("llm: unsupported architecture %q (registered: %v)",
			cfg.Arch, registeredArchitectures())
	}
	return factory(cfg)
}

func registeredArchitectures() []string {
	types := make([]string, 0, len(architectureFactories))
	for k := range architectureFactories {
		types = append(types, k)
	}
	return types
}

// _ ensures the mlx package is referenced even if the interface doesn't use
// it directly (it does via ForwardPrefill/ForwardDecode signatures).
var _ = mlx.Available
