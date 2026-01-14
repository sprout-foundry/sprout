// +build ollama_test

package security_validator

import (
	"context"
	"fmt"
	"strings"

	ollama "github.com/ollama/ollama/api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// loadOllamaModel loads an Ollama model for testing
func loadOllamaModel(modelPath string) (LLMModel, error) {
	// modelPath is actually the model name for Ollama (e.g., "qwen2.5-coder:0.5b")
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama client: %w", err)
	}

	return &ollamaWrapper{
		client: client,
		model:  modelPath,
	}, nil
}

// ollamaWrapper adapts Ollama client to implement LLMModel interface
type ollamaWrapper struct {
	client ollamaClient
	model  string
}

type ollamaClient interface {
	Chat(ctx context.Context, req *ollama.ChatRequest, fn ollama.ChatResponseFunc) error
}

func (w *ollamaWrapper) Completion(ctx context.Context, prompt string, opts ...interface{}) (string, error) {
	req := &ollama.ChatRequest{
		Model:    w.model,
		Messages: []ollama.Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Options: map[string]interface{}{
			"num_predict": 256,    // Limit to 256 tokens
			"temperature": 0.1,   // Low temperature for consistent classification
			"top_p":       0.9,
			"top_k":       40,
		},
	}

	var responseBuilder strings.Builder

	err := w.client.Chat(ctx, req, func(res ollama.ChatResponse) error {
		responseBuilder.WriteString(res.Message.Content)
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("ollama chat failed: %w", err)
	}

	return responseBuilder.String(), nil
}

// NewOllamaValidator creates a validator using Ollama (for testing)
func NewOllamaValidator(cfg *configuration.SecurityValidationConfig, logger *utils.Logger, interactive bool) (*Validator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("security validation config is nil")
	}

	if !cfg.Enabled {
		return &Validator{
			config:     cfg,
			logger:     logger,
			interactive: interactive,
			debug:      false,
		}, nil
	}

	// For Ollama, model path is the model name
	modelName := cfg.Model
	if modelName == "" {
		modelName = "qwen2.5-coder:0.5b"
	}

	// Load the Ollama model
	model, err := loadOllamaModel(modelName)
	if err != nil {
		return nil, fmt.Errorf("failed to load Ollama model: %w", err)
	}

	return &Validator{
		config:     cfg,
		model:      model,
		modelPath:  modelName, // Store model name instead of path
		logger:     logger,
		interactive: interactive,
		debug:      false,
	}, nil
}
