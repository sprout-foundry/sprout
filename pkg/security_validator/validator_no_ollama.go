//go:build !ollama_test
// +build !ollama_test

package security_validator

import (
	"errors"
)

// loadOllamaModel is a stub for non-ollama_test builds
func loadOllamaModel(modelPath string) (LLMModel, error) {
	return nil, errors.New("ollama testing not enabled - build with -tags ollama_test")
}
