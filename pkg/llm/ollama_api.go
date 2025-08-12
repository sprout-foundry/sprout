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

func callOllamaAPI(modelName string, messages []prompts.Message, cfg *config.Config, timeout time.Duration, writer io.Writer) (*TokenUsage, error) {
	client, err := ollama.ClientFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("could not create ollama client: %w", err)
	}

	ollamaMessages := make([]ollama.Message, len(messages))
	for i, msg := range messages {
		ollamaMessages[i] = ollama.Message{
			Role:    msg.Role,
			Content: GetMessageText(msg.Content), // Use helper function
		}
	}

	// The model name for ollama is without the "ollama:" prefix
	actualModelName := strings.TrimPrefix(modelName, "ollama:")

	// Calculate total token count for all messages
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += EstimateTokens(GetMessageText(msg.Content)) // Use helper function
	}

	// Set num_ctx to be slightly larger than the total token count to provide buffer
	// but with a minimum value to ensure adequate context
	numCtx := totalTokens + 1000
	if numCtx < 4096 {
		numCtx = 4096 // Minimum context size
	}

	req := &ollama.ChatRequest{
		Model:    actualModelName,
		Messages: ollamaMessages,
		Options: map[string]interface{}{
			"temperature":    0.1,                                  // Very low for consistency
			"top_p":          0.9,                                  // Focus on high-probability tokens
			"num_ctx":        numCtx,                               // Dynamically calculated context size
			"num_predict":    4096,                                 // Limit output length
			"repeat_penalty": 1.1,                                  // Discourage repetition
			"stop":           []string{"\n\n\n", "```\n\n", "END"}, // Stop sequences
			"stream":         true,                                 // Enable streaming for Ollama
		},
	}

	// For Ollama, we'll estimate token usage since it doesn't provide detailed usage data
	estimatedUsage := &TokenUsage{
		PromptTokens:     totalTokens,
		CompletionTokens: totalTokens / 3, // Rough estimate
		TotalTokens:      totalTokens + (totalTokens / 3),
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	respFunc := func(res ollama.ChatResponse) error {
		writer.Write([]byte(res.Message.Content))
		return nil
	}

	err = client.Chat(ctx, req, respFunc)
	if err != nil {
		return estimatedUsage, fmt.Errorf("ollama chat failed: %w", err)
	}

	return estimatedUsage, nil
}
