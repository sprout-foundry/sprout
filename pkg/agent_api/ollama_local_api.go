// Package agent_api: Ollama local client synchronous API methods (split from ollama_local.go)
package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SendChatRequest sends a chat request to local Ollama
func (c *OllamaLocalClient) SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	req, totalTokens := c.buildChatRequest(messages, tools, reasoning, false)

	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	var responseContent strings.Builder
	var toolCalls []ToolCall
	var lastDoneReason string
	var lastMetrics localOllamaMetrics
	respFunc := func(res *localOllamaChatResponse) error {
		if len(res.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, convertOllamaToolCalls(res.Message.ToolCalls)...)
		} else if trimmed := strings.TrimSpace(res.Message.Content); trimmed != "" {
			responseContent.WriteString(res.Message.Content)
		}

		if res.DoneReason != "" {
			lastDoneReason = res.DoneReason
		}

		lastMetrics = res.Metrics

		return nil
	}

	startTime := time.Now()

	err = client.Chat(ctx, req, respFunc)
	if err != nil {
		return nil, agenterrors.NewProviderError("ollama chat failed", err, "ollama", c.model)
	}

	duration := time.Since(startTime)

	finishReason := lastDoneReason
	if finishReason == "" {
		finishReason = "stop"
	}

	response := &ChatResponse{
		ID:      "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   c.model,
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:    "assistant",
				Content: responseContent.String(),
			},
			FinishReason: finishReason,
		}},
	}

	promptTokens := totalTokens
	if lastMetrics.PromptEvalCount > 0 {
		promptTokens = lastMetrics.PromptEvalCount
	}

	completionTokens := EstimateTokens(responseContent.String())
	if lastMetrics.EvalCount > 0 {
		completionTokens = lastMetrics.EvalCount
	}

	response.Usage.PromptTokens = promptTokens
	response.Usage.CompletionTokens = completionTokens
	response.Usage.TotalTokens = promptTokens + completionTokens
	response.Usage.EstimatedCost = 0

	if len(toolCalls) > 0 {
		response.Choices[0].Message.ToolCalls = toolCalls
	}

	if c.GetTracker() != nil && completionTokens > 0 {
		c.GetTracker().RecordRequest(duration, completionTokens)
	}

	return response, nil
}

// SetDebug enables or disables debug mode
func (c *OllamaLocalClient) SetDebug(debug bool) {
	c.debug = debug
}

// GetModel returns the current model
func (c *OllamaLocalClient) GetModel() string {
	return c.model
}

// GetProvider returns the provider name
func (c *OllamaLocalClient) GetProvider() string {
	return "ollama-local"
}

// CheckConnection verifies local Ollama is accessible
func (c *OllamaLocalClient) CheckConnection() error {
	client, err := c.newClient()
	if err != nil {
		return agenterrors.NewConfig("could not create ollama client", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.List(ctx)
	if err != nil {
		return agenterrors.NewNetwork("ollama connection check failed", err)
	}
	return nil
}

// GetModelContextLimit returns the context limit for the model.
//
// Resolution layers (most-specific to least):
//  1. Cached list-models entry whose context Ollama reported via /api/show.
//  2. Fresh /api/show lookup for the current model (2s timeout; log on failure
//     so a misconfigured Ollama server doesn't silently degrade to the wrong
//     default context length).
//  3. Static DefaultContextLimit from config (set at construction).
//  4. Hardcoded 32000 fallback.
//
// Replaces the previous substring match on "qwen3-coder"/"gpt-oss", which
// missed most models and silently returned 32k for everything else.
func (c *OllamaLocalClient) GetModelContextLimit() (int, error) {
	if contextLength, ok := c.GetCachedModel(c.model); ok {
		return contextLength, nil
	}

	if c.model != "" {
		if client, err := c.newClient(); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			showResp, showErr := client.Show(ctx, c.model)
			cancel()
			if showErr == nil && showResp != nil && showResp.ModelInfo.ContextLength > 0 {
				return showResp.ModelInfo.ContextLength, nil
			}
			if showErr != nil {
				fmt.Fprintf(os.Stderr, "[~] Failed to fetch Ollama model context for %s: %v\n", c.model, showErr)
			}
		}
	}

	if c.config.DefaultContextLimit > 0 {
		return c.config.DefaultContextLimit, nil
	}
	return defaultOllamaContextLimit, nil
}

// GetCachedModel returns a cached context length when the model list is fresh.
// Matches by ID or Name; callers typically pass the Ollama tag (e.g.
// "llama3:8b") which ListModels stores in both fields for symmetry with
// lmStudioListModelsWrapper.
func (c *OllamaLocalClient) GetCachedModel(name string) (int, bool) {
	if name == "" {
		return 0, false
	}

	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	if c.cachedAt.IsZero() || time.Since(c.cachedAt) >= ollamaModelsCacheTTL {
		return 0, false
	}

	for _, model := range c.cachedModels {
		if (model.ID == name || model.Name == name) && model.ContextLength > 0 {
			return model.ContextLength, true
		}
	}
	return 0, false
}

// SetModel updates the active model after validating it exists locally
func (c *OllamaLocalClient) SetModel(model string) error {
	if strings.TrimSpace(model) == "" {
		return errors.New("model name cannot be empty")
	}

	if model == c.model {
		return nil
	}

	client, err := c.newClient()
	if err != nil {
		return agenterrors.NewConfig("could not create ollama client", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := client.List(ctx)
	if err != nil {
		return agenterrors.NewNetwork("failed to list local models", err)
	}

	availableModels := make([]string, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		availableModels = append(availableModels, m.Name)
		if m.Name == model {
			c.model = model
			return nil
		}
	}

	if len(listResp.Models) > 0 {
		bracketWarn(os.Stderr, fmt.Sprintf("Model '%s' not found locally. Available models: %v", model, availableModels))
		fmt.Fprintf(os.Stderr, "[~] Falling back to first available model: %s\n", listResp.Models[0].Name)
		c.model = listResp.Models[0].Name
		return nil
	}

	return agenterrors.NewProviderError(fmt.Sprintf("model %s not found locally and no other models available. Available models: %s", model, availableModels), nil, "ollama", "")
}

// ListModels returns available local models.
func (c *OllamaLocalClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if models, ok := c.getCachedModels(); ok {
		return models, nil
	}

	client, err := c.newClient()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	listResp, err := client.List(listCtx)
	if err != nil {
		return nil, agenterrors.NewNetwork("failed to list local models", err)
	}

	models := make([]ModelInfo, 0, len(listResp.Models))
	for _, m := range listResp.Models {
		modelInfo := ModelInfo{
			ID:       m.Name,
			Name:     m.Name,
			Provider: "ollama-local",
		}

		showCtx, showCancel := context.WithTimeout(listCtx, 2*time.Second)
		showResp, showErr := client.Show(showCtx, m.Name)
		showCancel()
		if showErr != nil {
			fmt.Fprintf(os.Stderr, "[~] Failed to fetch Ollama model details for %s: %v\n", m.Name, showErr)
		} else if showResp != nil {
			modelInfo.ContextLength = showResp.ModelInfo.ContextLength
		}
		models = append(models, modelInfo)
	}

	c.cacheMu.Lock()
	c.cachedModels = append([]ModelInfo(nil), models...)
	c.cachedAt = time.Now()
	c.cacheMu.Unlock()

	return models, nil
}

func (c *OllamaLocalClient) getCachedModels() ([]ModelInfo, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	if len(c.cachedModels) == 0 || c.cachedAt.IsZero() || time.Since(c.cachedAt) >= ollamaModelsCacheTTL {
		return nil, false
	}
	return append([]ModelInfo(nil), c.cachedModels...), true
}

// SupportsVision returns true for OCR-capable models
func (c *OllamaLocalClient) SupportsVision() bool {
	modelLower := strings.ToLower(c.model)
	return strings.Contains(modelLower, "ocr") ||
		strings.Contains(modelLower, "vision") ||
		strings.Contains(modelLower, "llama3.2")
}

// VisionCapabilities returns the local-Ollama vision limits used by
// llama3.2-vision / glm-ocr family models.
//
// Conservative defaults reflect the documented Ollama API constraints:
// the older base64 payload cap is ~3.5MB (we use 5MB), llama3.2-vision
// works best at 1024px on the longest side, and we cap to a handful of
// images per request since local context windows are tight. Detail tiers
// are intentionally left nil — Ollama picks automatically. The returned
// values are static (don't depend on c.model) so the table is safe to
// share across clients; per-model overrides can be added later.
// SP-103-D3 / AUDIT-GAP-2.
func (c *OllamaLocalClient) VisionCapabilities() VisionCapabilities {
	return VisionCapabilities{
		MaxImageBytes:     5_000_000,
		MaxImageCount:     5,
		MaxImageDimension: 1024,
		DetailTiers:       nil,
	}
}

// SupportsConversationalVision returns true only for multimodal chat models.
// OCR-only models (e.g. glm-ocr) accept images but produce extraction output
// that doesn't help free-form conversational turns — the tool path
// (analyze_image_content) is the right channel for them. Inline embedding
// is only useful for chat models like llama3.2-vision.
func (c *OllamaLocalClient) SupportsConversationalVision() bool {
	modelLower := strings.ToLower(c.model)
	if strings.Contains(modelLower, "ocr") {
		return false
	}
	return strings.Contains(modelLower, "vision") ||
		strings.Contains(modelLower, "llama3.2")
}

// GetVisionModel returns empty string as vision is not supported
func (c *OllamaLocalClient) GetVisionModel() string {
	return ""
}

// SendVisionRequest handles vision/OCR requests for Ollama
// Delegates to SendChatRequest since the image handling is done in buildChatRequest
func (c *OllamaLocalClient) SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	return c.SendChatRequest(ctx, messages, tools, reasoning, disableThinking)
}
