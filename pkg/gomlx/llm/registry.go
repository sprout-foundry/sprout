//go:build darwin && arm64 && cgo

package llm

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// ArchitectureFactory creates an Architecture instance from a ModelConfig and
// a tensor.Backend. Factories are registered at init time by each architecture
// package.
type ArchitectureFactory func(cfg ModelConfig, backend tensor.Backend) (Architecture, error)

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
func createArchitecture(cfg ModelConfig, backend tensor.Backend) (Architecture, error) {
	factory, ok := architectureFactories[cfg.Arch]
	if !ok {
		return nil, fmt.Errorf("llm: unsupported architecture %q (registered: %v)",
			cfg.Arch, registeredArchitectures())
	}
	return factory(cfg, backend)
}

// ArchFactory returns the registered factory for a model type. Exported for
// tools that build an Architecture directly (e.g. layer-parity debugging).
func ArchFactory(modelType string) (ArchitectureFactory, error) {
	factory, ok := architectureFactories[modelType]
	if !ok {
		return nil, fmt.Errorf("llm: unsupported architecture %q (registered: %v)",
			modelType, registeredArchitectures())
	}
	return factory, nil
}

func registeredArchitectures() []string {
	types := make([]string, 0, len(architectureFactories))
	for k := range architectureFactories {
		types = append(types, k)
	}
	return types
}
