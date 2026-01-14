// +build !test,!ollama_test

package security_validator

import (
	"context"

	"github.com/go-skynet/go-llama.cpp"
)

// loadLlamaModel loads a llama.cpp model with the given path
func loadLlamaModel(modelPath string) (LLMModel, error) {
	llamaModel, err := llama.New(modelPath, llama.EnableF16Memory)
	if err != nil {
		return nil, err
	}

	return &llamaWrapper{model: llamaModel}, nil
}

// llamaWrapper adapts llama.LLama to implement LLMModel interface
type llamaWrapper struct {
	model *llama.LLama
}

func (w *llamaWrapper) Completion(ctx context.Context, prompt string, opts ...interface{}) (string, error) {
	// Extract options from the variadic args and pass them to llama
	// For now, we'll use a simplified version that just calls the model
	return w.model.Completion(prompt)
}
