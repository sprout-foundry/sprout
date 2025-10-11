package providers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ZAIProvider implements the OpenAI-compatible Z.AI Coding Plan API
type ZAIProvider struct {
	httpClient      *http.Client
	streamingClient *http.Client
	apiToken        string
	debug           bool
	model           string
	models          []api.ModelInfo
	modelsCached    bool
}

// NewZAIProvider creates a new Z.AI provider instance
func NewZAIProvider() (*ZAIProvider, error) {
	token := os.Getenv("ZAI_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("ZAI_API_KEY environment variable not set")
	}

	timeout := 320 * time.Second

	return &ZAIProvider{
		httpClient:      &http.Client{Timeout: timeout},
		streamingClient: &http.Client{Timeout: 900 * time.Second},
		apiToken:        token,
		debug:           false,
		model:           "GLM-4.6",
	}, nil
}

// NewZAIProviderWithModel creates a Z.AI provider with a specific model
func NewZAIProviderWithModel(model string) (*ZAIProvider, error) {
	p, err := NewZAIProvider()
	if err != nil {
		return nil, err
	}
	if model != "" {
		p.model = model
	}
	return p, nil
}

// SendChatRequest sends a chat completion request to Z.AI
func (p *ZAIProvider) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Z.AI follows OpenAI message format
	openaiMessages := BuildOpenAIChatMessages(messages, MessageConversionOptions{})

	// Estimate tokens and compute max_tokens conservatively
	contextLimit, err := p.GetModelContextLimit()
	if err != nil {
		contextLimit = 128000
	}
	maxTokens := CalculateMaxTokens(contextLimit, messages, tools)

	requestBody := map[string]interface{}{
		"model":       p.model,
		"messages":    openaiMessages,
		"max_tokens":  maxTokens,
		"temperature": 0.1, // Lower temperature for GLM models for more consistent coding output
	}

	if openAITools := BuildOpenAIToolsPayload(tools); openAITools != nil {
		requestBody["tools"] = openAITools
		requestBody["tool_choice"] = "auto"
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := "https://api.z.ai/api/coding/paas/v4/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiToken)

	if p.debug {
		fmt.Printf("🔍 Z.AI Request URL: %s\n", url)
		fmt.Printf("🔍 Z.AI Request Body: %s\n", string(body))
	}

	return p.sendRequestWithRetry(req, body)
}

// SendChatRequestStream sends a streaming chat request
func (p *ZAIProvider) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	url := "https://api.z.ai/api/coding/paas/v4/chat/completions"

	openaiMessages := BuildOpenAIStreamingMessages(messages, MessageConversionOptions{})
	reqBody := map[string]interface{}{
		"model":       p.model,
		"messages":    openaiMessages,
		"temperature": 0.1, // Lower temperature for GLM models for more consistent coding output
		"stream":      true,
	}
	if openAITools := BuildOpenAIToolsPayload(tools); openAITools != nil {
		reqBody["tools"] = openAITools
		reqBody["tool_choice"] = "auto"
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Accept", "text/event-stream")

	if p.debug {
		fmt.Printf("🔍 Z.AI Streaming Request URL: %s\n", url)
		fmt.Printf("🔍 Z.AI Streaming Request Body: %s\n", string(body))
	}

	resp, err := p.streamingClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Z.AI API error (status %d): %s", resp.StatusCode, string(b))
	}

	// Parse SSE stream with timeout protection
	scanner := bufio.NewScanner(resp.Body)
	var content strings.Builder
	var reasoningContent strings.Builder
	var toolCalls []api.ToolCall
	var toolCallsMap = make(map[string]*api.ToolCall)
	var finishReason string
	var usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		EstimatedCost    float64 `json:"estimated_cost"`
	}

	// Add a timeout channel to prevent infinite blocking
	timeoutChan := time.After(120 * time.Second) // 2 minutes max streaming time

	for scanner.Scan() {
		select {
		case <-timeoutChan:
			return nil, fmt.Errorf("ZAI streaming timeout after 2 minutes")
		default:
			// Continue processing
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				if p.debug {
					fmt.Printf("Failed to parse stream chunk: %v\n", err)
				}
				continue
			}
			// Extract content delta
			if choices, ok := chunk["choices"].([]interface{}); ok && len(choices) > 0 {
				if choice, ok := choices[0].(map[string]interface{}); ok {
					if delta, ok := choice["delta"].(map[string]interface{}); ok {
						if c, ok := delta["content"].(string); ok && c != "" {
							content.WriteString(c)
							if callback != nil {
								callback(c)
							}
						}
						// Track reasoning content if present
						if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
							reasoningContent.WriteString(rc)
							// Send reasoning content through callback with grey formatting
							if callback != nil {
								// Format reasoning content in grey/dim text to indicate thinking
								formattedReasoning := fmt.Sprintf("\033[2m%s\033[0m", rc)
								callback(formattedReasoning)
							}
						}
						// Track tool calls if present
						if toolCallsData, ok := delta["tool_calls"].([]interface{}); ok {
							for _, tc := range toolCallsData {
								if tcMap, ok := tc.(map[string]interface{}); ok {
									idx := intFrom(tcMap["index"]) // may be 0
									// Ensure capacity
									for len(toolCalls) <= idx {
										toolCalls = append(toolCalls, api.ToolCall{})
									}
									// Get or create entry
									var id string
									if idv, ok := tcMap["id"].(string); ok {
										id = idv
									} else {
										id = fmt.Sprintf("call_%d", idx)
									}
									entry, exists := toolCallsMap[id]
									if !exists {
										toolCalls[idx] = api.ToolCall{ID: id, Type: "function"}
										toolCallsMap[id] = &toolCalls[idx]
										entry = &toolCalls[idx]
									}
									if fdata, ok := tcMap["function"].(map[string]interface{}); ok {
										if name, ok := fdata["name"].(string); ok {
											entry.Function.Name = name
										}
										if args, ok := fdata["arguments"].(string); ok {
											entry.Function.Arguments = args
										}
									}
								}
							}
						}
					}
					if fr, ok := choice["finish_reason"].(string); ok {
						finishReason = fr
					}
				}
			}
			// Extract usage if present in chunk
			if usageData, ok := chunk["usage"].(map[string]interface{}); ok {
				if pt, ok := usageData["prompt_tokens"].(float64); ok {
					usage.PromptTokens = int(pt)
				}
				if ct, ok := usageData["completion_tokens"].(float64); ok {
					usage.CompletionTokens = int(ct)
				}
				if tt, ok := usageData["total_tokens"].(float64); ok {
					usage.TotalTokens = int(tt)
				}
				if cost, ok := usageData["estimated_cost"].(float64); ok {
					usage.EstimatedCost = cost
				}
			}
		}
	}

	// Check for scanner errors with more detailed error reporting
	if err := scanner.Err(); err != nil {
		if p.debug {
			fmt.Printf("ZAI streaming scanner error: %v\n", err)
		}
		return nil, fmt.Errorf("ZAI stream read error: %w", err)
	}

	// Check if we received any content at all
	if content.Len() == 0 && len(toolCalls) == 0 {
		return nil, fmt.Errorf("ZAI streaming completed with no content or tool calls")
	}

	response := &api.ChatResponse{
		ID:      "",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   p.model,
		Choices: []api.Choice{{
			Index: 0,
			Message: struct {
				Role             string          `json:"role"`
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content,omitempty"`
				Images           []api.ImageData `json:"images,omitempty"`
				ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             "assistant",
				Content:          content.String(),
				ReasoningContent: reasoningContent.String(),
				ToolCalls:        toolCalls,
			},
			FinishReason: finishReason,
		}},
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			EstimatedCost:    usage.EstimatedCost,
		},
	}
	return response, nil
}

// sendRequestWithRetry is shared by non-streaming path
func (p *ZAIProvider) sendRequestWithRetry(req *http.Request, reqBody []byte) (*api.ChatResponse, error) {
	// Simple single-attempt request (can be extended with retry/backoff if needed)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if p.debug {
		fmt.Printf("🔍 Z.AI Response: %s\n", string(body))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Z.AI API error (status %d): %s", resp.StatusCode, string(body))
	}

	// The response is OpenAI-compatible
	var openaiResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string         `json:"role"`
				Content   string         `json:"content"`
				ToolCalls []api.ToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			TotalTokens      int     `json:"total_tokens"`
			EstimatedCost    float64 `json:"estimated_cost"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if openaiResp.Error != nil {
		return nil, fmt.Errorf("Z.AI API error: %s", openaiResp.Error.Message)
	}
	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := openaiResp.Choices[0]
	response := &api.ChatResponse{
		ID:      openaiResp.ID,
		Object:  openaiResp.Object,
		Created: openaiResp.Created,
		Model:   openaiResp.Model,
		Choices: []api.Choice{{
			Index: choice.Index,
			Message: struct {
				Role             string          `json:"role"`
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content,omitempty"`
				Images           []api.ImageData `json:"images,omitempty"`
				ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:      choice.Message.Role,
				Content:   choice.Message.Content,
				ToolCalls: choice.Message.ToolCalls,
			},
			FinishReason: choice.FinishReason,
		}},
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			TotalTokens:      openaiResp.Usage.TotalTokens,
			EstimatedCost:    openaiResp.Usage.EstimatedCost,
		},
	}
	return response, nil
}

// CheckConnection verifies connectivity by sending a minimal request
func (p *ZAIProvider) CheckConnection() error {
	// Perform a minimal POST to chat/completions with an empty body.
	// Any HTTP response proves reachability. 401 signals bad/absent key.
	client := &http.Client{Timeout: 15 * time.Second}
	url := "https://api.z.ai/api/coding/paas/v4/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewBufferString("{}"))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Z.AI API not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid Z.AI API key")
	}
	// For other statuses (200/400/404/405/422/5xx), consider connectivity OK
	return nil
}

func (p *ZAIProvider) SetDebug(debug bool)         { p.debug = debug }
func (p *ZAIProvider) SetModel(model string) error { p.model = model; return nil }
func (p *ZAIProvider) GetModel() string            { return p.model }
func (p *ZAIProvider) GetProvider() string         { return "zai" }

func (p *ZAIProvider) GetModelContextLimit() (int, error) {
	// GLM 4.x commonly supports large context; use 128k as conservative default
	return 128000, nil
}

func (p *ZAIProvider) SupportsVision() bool   { return false }
func (p *ZAIProvider) GetVisionModel() string { return "" }
func (p *ZAIProvider) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	// Fallback to regular request
	return p.SendChatRequest(messages, tools, reasoning)
}

// ListModels returns a static list of common GLM Coding Plan models
func (p *ZAIProvider) ListModels() ([]api.ModelInfo, error) {
	if p.modelsCached && len(p.models) > 0 {
		return p.models, nil
	}
	p.models = []api.ModelInfo{
		{ID: "GLM-4.6", Name: "GLM-4.6", ContextLength: 128000},
		{ID: "GLM-4.5", Name: "GLM-4.5", ContextLength: 128000},
		{ID: "GLM-4.5-air", Name: "GLM-4.5-air", ContextLength: 128000},
	}
	p.modelsCached = true
	return p.models, nil
}

// Estimate cost is not documented; return 0.0 for now
func (p *ZAIProvider) estimateCost(promptTokens, completionTokens int) float64 {
	_ = math.Min(0, 0) // keep math import used
	return 0.0
}

// Utility to coerce number to int safely
func intFrom(v interface{}) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}
