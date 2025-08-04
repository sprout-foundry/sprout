package llm

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/prompts"
	ollama "github.com/ollama/ollama/api"
)

func callOllamaAPI(modelName string, messages []prompts.Message, cfg *config.Config, timeout time.Duration, writer io.Writer) error {
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("could not create ollama client: %w", err)
	}

	ollamaMessages := make([]ollama.Message, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollama.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// The model name for ollama is without the "ollama:" prefix
	actualModelName := strings.TrimPrefix(modelName, "ollama:")

	req := &ollama.ChatRequest{
		Model:    actualModelName,
		Messages: ollamaMessages,
		Options: map[string]interface{}{
			"temperature": cfg.Temperature,
			"top_p":       1.0,
			"num_ctx":     100000, // Ollama's context size
			"stream":      true,   // Enable streaming for Ollama
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	respFunc := func(res ollama.ChatResponse) error {
		writer.Write([]byte(res.Message.Content))
		return nil
	}

	err = client.Chat(ctx, req, respFunc)
	if err != nil {
		return fmt.Errorf("ollama chat failed: %w", err)
	}

	return nil
}
