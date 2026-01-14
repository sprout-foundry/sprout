package security_validator

import (
	"context"
)

// LLMModel defines the interface for LLM inference
// This allows us to mock the model in tests without requiring llama.cpp to be installed
type LLMModel interface {
	Completion(ctx context.Context, prompt string, opts ...interface{}) (string, error)
}

// Adapt llama.LLama to our interface (only used in production)
// This is done in validator.go when we create the real model
