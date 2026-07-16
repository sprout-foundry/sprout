// Package agent_api: Ollama streaming response helpers, converter functions, and streaming API (split from ollama_local.go)
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SendChatRequestStream streams responses from local Ollama as they arrive
func (c *OllamaLocalClient) SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
	client, err := c.newClient()
	if err != nil {
		return nil, agenterrors.NewConfig("could not create ollama client", err)
	}

	req, totalTokens := c.buildChatRequest(messages, tools, reasoning, true)

	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	builder := NewStreamingResponseBuilder(callback)
	var lastMetrics localOllamaMetrics
	var lastDoneReason string

	startTime := time.Now()

	err = client.Chat(ctx, req, func(res *localOllamaChatResponse) error {
		chunk := convertOllamaResponseToStreamingChunk(res)
		if err := builder.ProcessChunk(chunk); err != nil {
			return agenterrors.NewProviderError("failed to process ollama chat chunk", err, "ollama", c.model)
		}

		if res.DoneReason != "" {
			lastDoneReason = res.DoneReason
		}

		lastMetrics = res.Metrics
		return nil
	})
	if err != nil {
		return nil, agenterrors.NewProviderError("ollama chat failed", err, "ollama", c.model)
	}

	response := builder.GetResponse()
	if response == nil {
		response = &ChatResponse{}
	}

	if response.ID == "" {
		response.ID = "ollama-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if response.Object == "" {
		response.Object = "chat.completion"
	}
	if response.Created == 0 {
		response.Created = time.Now().Unix()
	}
	response.Model = c.model

	if len(response.Choices) == 0 {
		response.Choices = []Choice{{}}
	}

	choice := &response.Choices[0]
	if choice.Message.Role == "" {
		choice.Message.Role = "assistant"
	}
	if choice.FinishReason == "" {
		if lastDoneReason != "" {
			choice.FinishReason = lastDoneReason
		} else {
			choice.FinishReason = "stop"
		}
	}

	promptTokens := totalTokens
	if lastMetrics.PromptEvalCount > 0 {
		promptTokens = lastMetrics.PromptEvalCount
	}

	completionTokens := EstimateTokens(choice.Message.Content)
	if lastMetrics.EvalCount > 0 {
		completionTokens = lastMetrics.EvalCount
	}

	response.Usage.PromptTokens = promptTokens
	response.Usage.CompletionTokens = completionTokens
	response.Usage.TotalTokens = promptTokens + completionTokens
	response.Usage.EstimatedCost = 0

	if c.GetTracker() != nil && completionTokens > 0 {
		c.GetTracker().RecordRequest(time.Since(startTime), completionTokens)
	}

	return response, nil
}

func convertToolsToOllamaTools(tools []Tool) []localOllamaTool {
	if len(tools) == 0 {
		return nil
	}

	result := make([]localOllamaTool, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Type) == "" {
			continue
		}

		ollamaTool := localOllamaTool{
			Type: tool.Type,
			Function: localOllamaToolFunction{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
			},
		}

		params := json.RawMessage(`{"type":"object"}`)
		if tool.Function.Parameters != nil {
			if raw, err := json.Marshal(tool.Function.Parameters); err == nil {
				params = raw
			}
		}

		ollamaTool.Function.Parameters = params
		result = append(result, ollamaTool)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func convertOllamaResponseToStreamingChunk(res *localOllamaChatResponse) *StreamingChatResponse {
	chunk := &StreamingChatResponse{
		ID:    res.Model,
		Model: res.Model,
	}

	if !res.CreatedAt.IsZero() {
		chunk.Created = res.CreatedAt.Unix()
	}

	delta := StreamingDelta{Role: res.Message.Role}

	if len(res.Message.ToolCalls) == 0 {
		trimmed := strings.TrimSpace(res.Message.Content)
		if trimmed != "" {
			delta.Content = res.Message.Content
		}
	}

	if len(res.Message.ToolCalls) > 0 {
		delta.ToolCalls = make([]StreamingToolCall, 0, len(res.Message.ToolCalls))
		for _, call := range res.Message.ToolCalls {
			arguments := ""
			if len(call.Function.Arguments) > 0 {
				arguments = string(call.Function.Arguments)
			}

			delta.ToolCalls = append(delta.ToolCalls, StreamingToolCall{
				Index: call.Function.Index,
				Function: &StreamingToolCallFunction{
					Name:      call.Function.Name,
					Arguments: arguments,
				},
			})
		}
	}

	choice := StreamingChoice{
		Index: 0,
		Delta: delta,
	}

	if res.DoneReason != "" {
		reason := res.DoneReason
		choice.FinishReason = &reason
	} else if res.Done {
		reason := "stop"
		choice.FinishReason = &reason
	}

	chunk.Choices = []StreamingChoice{choice}
	return chunk
}

func convertOllamaToolCalls(calls []localOllamaToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}

	result := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		arguments := ""
		if len(call.Function.Arguments) > 0 {
			arguments = string(call.Function.Arguments)
		}

		toolCall := ToolCall{Type: "function"}
		toolCall.Function.Name = strings.Split(call.Function.Name, "<|channel|>")[0]
		toolCall.Function.Arguments = arguments
		result = append(result, toolCall)
	}

	return result
}
