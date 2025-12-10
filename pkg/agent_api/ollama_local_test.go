package api

import (
	"context"
	"testing"

	ollama "github.com/ollama/ollama/api"
	"github.com/stretchr/testify/require"
)

type stubOllamaClient struct {
	listResp      *ollama.ListResponse
	listErr       error
	chatErr       error
	chatResponses []ollama.ChatResponse
	chatRequest   *ollama.ChatRequest
}

func (s *stubOllamaClient) List(ctx context.Context) (*ollama.ListResponse, error) {
	return s.listResp, s.listErr
}

func (s *stubOllamaClient) Chat(ctx context.Context, req *ollama.ChatRequest, fn ollama.ChatResponseFunc) error {
	s.chatRequest = req
	if s.chatErr != nil {
		return s.chatErr
	}
	if fn != nil {
		for _, resp := range s.chatResponses {
			if err := fn(resp); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestOllamaLocalClientSetModelSwitchesModel(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{
			{Name: "model-a"},
			{Name: "model-b"},
		},
	}

	callCount := 0
	factory := func() (ollamaClient, error) {
		callCount++
		return &stubOllamaClient{listResp: available}, nil
	}

	client, err := newOllamaLocalClientWithFactory("model-a", factory)
	require.NoError(t, err)
	require.Equal(t, 1, callCount)

	err = client.SetModel("model-b")
	require.NoError(t, err)
	require.Equal(t, "model-b", client.GetModel())
	require.Equal(t, 2, callCount, "expected client factory to be invoked for SetModel")

	// Setting the same model should be a no-op and avoid calling the factory
	err = client.SetModel("model-b")
	require.NoError(t, err)
	require.Equal(t, 2, callCount)
}

func TestOllamaLocalClientSetModelUnknownModel(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{
			{Name: "model-a"},
		},
	}

	factory := func() (ollamaClient, error) {
		return &stubOllamaClient{listResp: available}, nil
	}

	client, err := newOllamaLocalClientWithFactory("model-a", factory)
	require.NoError(t, err)

	err = client.SetModel("model-missing")
	// With fallback, SetModel should not error and should use fallback model
	require.NoError(t, err)
	require.Equal(t, "model-a", client.GetModel())
}

func TestOllamaLocalClientSetModelEmptyName(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{{Name: "model-a"}},
	}

	factory := func() (ollamaClient, error) {
		return &stubOllamaClient{listResp: available}, nil
	}

	client, err := newOllamaLocalClientWithFactory("model-a", factory)
	require.NoError(t, err)

	err = client.SetModel("   ")
	require.Error(t, err)
	require.Equal(t, "model-a", client.GetModel())
}

func TestOllamaLocalClientIncludesStructuredTools(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{{Name: "model-a"}},
	}

	stub := &stubOllamaClient{listResp: available}
	client, err := newOllamaLocalClientWithFactory("model-a", func() (ollamaClient, error) {
		return stub, nil
	})
	require.NoError(t, err)

	tools := []Tool{{
		Type: "function",
		Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{
			Name:        "shell_command",
			Description: "Execute shell commands",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Shell command to execute",
					},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			},
		},
	}}

	_, err = client.SendChatRequest([]Message{{Role: "user", Content: "hi"}}, tools, "")
	require.NoError(t, err)
	require.NotNil(t, stub.chatRequest)
	require.Len(t, stub.chatRequest.Tools, 1)
	require.Len(t, stub.chatRequest.Messages, 1)
	require.Equal(t, "user", stub.chatRequest.Messages[0].Role)
	require.Equal(t, "hi", stub.chatRequest.Messages[0].Content)

	reqTool := stub.chatRequest.Tools[0]
	require.Equal(t, "function", reqTool.Type)
	require.Equal(t, "shell_command", reqTool.Function.Name)
	require.Equal(t, "Execute shell commands", reqTool.Function.Description)
	require.Equal(t, "object", reqTool.Function.Parameters.Type)
	require.ElementsMatch(t, []string{"command"}, reqTool.Function.Parameters.Required)

	commandProp, ok := reqTool.Function.Parameters.Properties["command"]
	require.True(t, ok)
	require.Contains(t, commandProp.Type, "string")
	require.Equal(t, "Shell command to execute", commandProp.Description)
}

func TestOllamaLocalClientStreamingEmitsChunks(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{{Name: "model-a"}},
	}

	stub := &stubOllamaClient{
		listResp: available,
		chatResponses: []ollama.ChatResponse{
			{
				Model: "model-a",
				Message: ollama.Message{
					Role:    "assistant",
					Content: "chunk one ",
				},
			},
			{
				Model: "model-a",
				Message: ollama.Message{
					Role:    "assistant",
					Content: "chunk two",
				},
				Done:       true,
				DoneReason: "stop",
				Metrics: ollama.Metrics{
					PromptEvalCount: 12,
					EvalCount:       8,
				},
			},
		},
	}

	client, err := newOllamaLocalClientWithFactory("model-a", func() (ollamaClient, error) {
		return stub, nil
	})
	require.NoError(t, err)

	var chunks []string
	resp, err := client.SendChatRequestStream([]Message{{Role: "user", Content: "hi"}}, nil, "", func(content string) {
		chunks = append(chunks, content)
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Choices, 1)
	require.Equal(t, "chunk one chunk two", resp.Choices[0].Message.Content)
	require.Equal(t, []string{"chunk one ", "chunk two"}, chunks)
	require.Equal(t, "stop", resp.Choices[0].FinishReason)
	require.Equal(t, 12, resp.Usage.PromptTokens)
	require.Equal(t, 8, resp.Usage.CompletionTokens)
	require.Equal(t, 20, resp.Usage.TotalTokens)
	require.NotNil(t, stub.chatRequest)
	require.NotNil(t, stub.chatRequest.Stream)
	require.True(t, *stub.chatRequest.Stream)
	streamOpt, ok := stub.chatRequest.Options["stream"].(bool)
	require.True(t, ok)
	require.True(t, streamOpt)
}

func TestOllamaLocalClientCapturesToolCalls(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{{Name: "model-a"}},
	}

	toolArgs := ollama.ToolCallFunctionArguments{"command": "ls", "args": []any{"-la"}}
	stub := &stubOllamaClient{
		listResp: available,
		chatResponses: []ollama.ChatResponse{
			{
				Message: ollama.Message{
					ToolCalls: []ollama.ToolCall{
						{Function: ollama.ToolCallFunction{Name: "shell_command", Arguments: toolArgs}},
					},
				},
				DoneReason: "tool_calls",
			},
		},
	}

	client, err := newOllamaLocalClientWithFactory("model-a", func() (ollamaClient, error) {
		return stub, nil
	})
	require.NoError(t, err)

	resp, err := client.SendChatRequest([]Message{{Role: "user", Content: "hi"}}, nil, "")
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	require.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	call := resp.Choices[0].Message.ToolCalls[0]
	require.Equal(t, "function", call.Type)
	require.Equal(t, "shell_command", call.Function.Name)
	require.JSONEq(t, `{"args": ["-la"], "command": "ls"}`, call.Function.Arguments)
	require.NotNil(t, stub.chatRequest)
	streamOpt, ok := stub.chatRequest.Options["stream"].(bool)
	require.True(t, ok)
	require.False(t, streamOpt)
}

func TestOllamaLocalClientStreamingToolCalls(t *testing.T) {
	available := &ollama.ListResponse{
		Models: []ollama.ListModelResponse{{Name: "model-a"}},
	}

	toolArgs := ollama.ToolCallFunctionArguments{"command": "ls", "args": []any{"-la"}}
	stub := &stubOllamaClient{
		listResp: available,
		chatResponses: []ollama.ChatResponse{
			{
				Message: ollama.Message{
					ToolCalls: []ollama.ToolCall{
						{Function: ollama.ToolCallFunction{Name: "shell_command", Arguments: toolArgs}},
					},
				},
				DoneReason: "tool_calls",
				Done:       true,
			},
		},
	}

	client, err := newOllamaLocalClientWithFactory("model-a", func() (ollamaClient, error) {
		return stub, nil
	})
	require.NoError(t, err)

	var chunks []string
	resp, err := client.SendChatRequestStream([]Message{{Role: "user", Content: "hi"}}, nil, "", func(content string) {
		chunks = append(chunks, content)
	})
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	require.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
	require.JSONEq(t, `{"args": ["-la"], "command": "ls"}`, resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	require.Empty(t, chunks)
	require.NotNil(t, stub.chatRequest)
	require.NotNil(t, stub.chatRequest.Stream)
	require.True(t, *stub.chatRequest.Stream)
}
