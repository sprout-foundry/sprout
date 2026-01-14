// +build test

package security_validator

import (
	"context"
	"fmt"
)

// loadLlamaModel is a stub for testing that always fails
func loadLlamaModel(modelPath string) (LLMModel, error) {
	// In test mode, we can't load the actual llama.cpp model
	// Tests will use MockLLM instead
	return nil, fmt.Errorf("llama.cpp not available in test mode (use MockLLM instead)")
}
