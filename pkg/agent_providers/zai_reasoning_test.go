package providers

import (
	"encoding/json"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestZAIParsingReasoningContent(t *testing.T) {
	// Simulate a ZAI response with reasoning content
	zaiResponse := `{
		"id": "test-id",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "glm-4.6",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The final answer",
				"reasoning_content": "This is my step-by-step reasoning process"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30,
			"estimated_cost": 0.001
		}
	}`

	// Parse the response using the same structure as ZAI provider
	var openaiResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role             string         `json:"role"`
				Content          string         `json:"content"`
				ReasoningContent string         `json:"reasoning_content,omitempty"`
				ToolCalls        []api.ToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			TotalTokens      int     `json:"total_tokens"`
			EstimatedCost    float64 `json:"estimated_cost"`
		} `json:"usage"`
	}

	if err := json.Unmarshal([]byte(zaiResponse), &openaiResp); err != nil {
		t.Fatalf("Failed to parse ZAI response: %v", err)
	}

	// Verify reasoning content was parsed correctly
	if len(openaiResp.Choices) == 0 {
		t.Fatal("No choices in response")
	}

	choice := openaiResp.Choices[0]
	if choice.Message.ReasoningContent == "" {
		t.Error("Reasoning content was not parsed from ZAI response")
	}

	expectedReasoning := "This is my step-by-step reasoning process"
	if choice.Message.ReasoningContent != expectedReasoning {
		t.Errorf("Expected reasoning content '%s', got '%s'", expectedReasoning, choice.Message.ReasoningContent)
	}

	// Test conversion to api.ChatResponse (same as ZAI provider does)
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
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				ToolCalls:        choice.Message.ToolCalls,
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

	// Verify the final response has reasoning content
	if response.Choices[0].Message.ReasoningContent == "" {
		t.Error("Reasoning content was not preserved in api.ChatResponse")
	}

	if response.Choices[0].Message.ReasoningContent != expectedReasoning {
		t.Errorf("Expected final reasoning content '%s', got '%s'", expectedReasoning, response.Choices[0].Message.ReasoningContent)
	}
}

func TestZAIParsingWithoutReasoningContent(t *testing.T) {
	// Simulate a ZAI response without reasoning content
	zaiResponse := `{
		"id": "test-id",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "glm-4.6",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "The final answer"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30,
			"estimated_cost": 0.001
		}
	}`

	// Parse the response using the same structure as ZAI provider
	var openaiResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role             string         `json:"role"`
				Content          string         `json:"content"`
				ReasoningContent string         `json:"reasoning_content,omitempty"`
				ToolCalls        []api.ToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int     `json:"prompt_tokens"`
			CompletionTokens int     `json:"completion_tokens"`
			TotalTokens      int     `json:"total_tokens"`
			EstimatedCost    float64 `json:"estimated_cost"`
		} `json:"usage"`
	}

	if err := json.Unmarshal([]byte(zaiResponse), &openaiResp); err != nil {
		t.Fatalf("Failed to parse ZAI response: %v", err)
	}

	// Verify reasoning content is empty when not provided
	choice := openaiResp.Choices[0]
	if choice.Message.ReasoningContent != "" {
		t.Errorf("Expected empty reasoning content, got '%s'", choice.Message.ReasoningContent)
	}
}
