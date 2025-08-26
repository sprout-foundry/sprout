package llm

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/alantheprice/ledit/internal/domain/agent"
	"github.com/alantheprice/ledit/internal/domain/todo"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	pkgTypes "github.com/alantheprice/ledit/pkg/types"
)

// LLMAdapter adapts the new interfaces.LLMProvider to legacy llm.LLMProvider
type LLMAdapter struct {
	provider interfaces.LLMProvider
}

// NewLLMAdapter creates a new LLM adapter
func NewLLMAdapter(provider interfaces.LLMProvider) llm.LLMProvider {
	return &LLMAdapter{
		provider: provider,
	}
}

// GenerateResponse adapts the new interface to the legacy interface
func (a *LLMAdapter) GenerateResponse(ctx context.Context, prompt string, options map[string]interface{}) (string, error) {
	// Convert legacy options to new request options
	reqOptions := a.convertOptions(options)

	// Create messages from prompt
	messages := []types.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, _, err := a.provider.GenerateResponse(ctx, messages, reqOptions)
	return response, err
}

// GetLLMResponse adapts to legacy LLMProvider interface
func (a *LLMAdapter) GetLLMResponse(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, imagePath ...string) (string, *pkgTypes.TokenUsage, error) {
	// Convert prompts.Message to types.Message
	var convertedMessages []types.Message
	for _, msg := range messages {
		content := ""
		if str, ok := msg.Content.(string); ok {
			content = str
		} else if msg.Content != nil {
			content = fmt.Sprintf("%v", msg.Content)
		}

		convertedMessages = append(convertedMessages, types.Message{
			Role:    msg.Role,
			Content: content,
		})
	}

	// Create request options
	reqOptions := types.RequestOptions{
		Model:   modelName,
		Timeout: timeout,
	}

	// Generate response
	ctx := context.Background()
	response, metadata, err := a.provider.GenerateResponse(ctx, convertedMessages, reqOptions)
	if err != nil {
		return "", nil, err
	}

	// Convert metadata if available
	var tokenUsage *pkgTypes.TokenUsage
	if metadata != nil {
		tokenUsage = &pkgTypes.TokenUsage{
			PromptTokens:     metadata.TokenUsage.PromptTokens,
			CompletionTokens: metadata.TokenUsage.CompletionTokens,
			TotalTokens:      metadata.TokenUsage.TotalTokens,
		}
	}

	return response, tokenUsage, nil
}

// GetLLMResponseStream adapts to legacy streaming interface
func (a *LLMAdapter) GetLLMResponseStream(modelName string, messages []prompts.Message, filename string, cfg *config.Config, timeout time.Duration, writer io.Writer, imagePath ...string) (*pkgTypes.TokenUsage, error) {
	// For now, fall back to non-streaming and write to writer
	response, tokenUsage, err := a.GetLLMResponse(modelName, messages, filename, cfg, timeout, imagePath...)
	if err != nil {
		return nil, err
	}

	if _, writeErr := writer.Write([]byte(response)); writeErr != nil {
		return tokenUsage, writeErr
	}

	return tokenUsage, nil
}

// convertOptions converts legacy options map to new RequestOptions
func (a *LLMAdapter) convertOptions(options map[string]interface{}) types.RequestOptions {
	reqOptions := types.RequestOptions{}

	if temp, ok := options["temperature"].(float64); ok {
		reqOptions.Temperature = temp
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		reqOptions.MaxTokens = maxTokens
	}

	return reqOptions
}

// DomainLLMAdapter adapts interfaces.LLMProvider to domain LLM interfaces
type DomainLLMAdapter struct {
	provider interfaces.LLMProvider
}

// NewDomainLLMAdapter creates a new domain LLM adapter
func NewDomainLLMAdapter(provider interfaces.LLMProvider) *DomainLLMAdapter {
	return &DomainLLMAdapter{
		provider: provider,
	}
}

// GenerateResponse implements agent.LLMProvider interface
func (a *DomainLLMAdapter) GenerateResponse(ctx context.Context, prompt string, options map[string]interface{}) (string, error) {
	// Convert options
	reqOptions := a.convertOptions(options)

	// Create messages from prompt
	messages := []types.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, _, err := a.provider.GenerateResponse(ctx, messages, reqOptions)
	return response, err
}

// AnalyzeIntent implements agent.LLMProvider interface
func (a *DomainLLMAdapter) AnalyzeIntent(ctx context.Context, intent string, context agent.WorkspaceContext) (map[string]interface{}, error) {
	// Build analysis prompt
	prompt := fmt.Sprintf(`Analyze this user intent in the context of the workspace:

Intent: %s

Workspace Context:
- Project Type: %s
- Root Path: %s
- Summary: %s

Provide analysis as JSON with fields: type, complexity, confidence, keywords, description`,
		intent, context.ProjectType, context.RootPath, context.Summary)

	// Generate response
	response, err := a.GenerateResponse(ctx, prompt, map[string]interface{}{
		"temperature": 0.2,
		"max_tokens":  1000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to analyze intent: %w", err)
	}

	// For now, return a simple structure - in production this would parse JSON
	return map[string]interface{}{
		"raw_response": response,
		"intent":       intent,
	}, nil
}

// convertOptions converts legacy options to new RequestOptions
func (a *DomainLLMAdapter) convertOptions(options map[string]interface{}) types.RequestOptions {
	reqOptions := types.RequestOptions{}

	if temp, ok := options["temperature"].(float64); ok {
		reqOptions.Temperature = temp
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		reqOptions.MaxTokens = maxTokens
	}

	return reqOptions
}

// TodoLLMAdapter adapts interfaces.LLMProvider to todo.LLMProvider interface
type TodoLLMAdapter struct {
	provider interfaces.LLMProvider
}

// NewTodoLLMAdapter creates a new todo LLM adapter
func NewTodoLLMAdapter(provider interfaces.LLMProvider) todo.LLMProvider {
	return &TodoLLMAdapter{
		provider: provider,
	}
}

// GenerateResponse implements todo.LLMProvider interface
func (a *TodoLLMAdapter) GenerateResponse(ctx context.Context, prompt string, options map[string]interface{}) (string, error) {
	// Convert options
	reqOptions := a.convertOptions(options)

	// Create messages from prompt
	messages := []types.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, _, err := a.provider.GenerateResponse(ctx, messages, reqOptions)
	return response, err
}

// convertOptions converts legacy options to new RequestOptions
func (a *TodoLLMAdapter) convertOptions(options map[string]interface{}) types.RequestOptions {
	reqOptions := types.RequestOptions{}

	if temp, ok := options["temperature"].(float64); ok {
		reqOptions.Temperature = temp
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		reqOptions.MaxTokens = maxTokens
	}

	return reqOptions
}
