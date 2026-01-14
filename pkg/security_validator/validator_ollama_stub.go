// +build ollama_test

package security_validator

import (
	"errors"
)

// loadLlamaModel is a stub for ollama_test builds (will use ollama instead)
func loadLlamaModel(modelPath string) (LLMModel, error) {
	return nil, errors.New("llama.cpp not available in ollama_test build - using Ollama instead")
}
