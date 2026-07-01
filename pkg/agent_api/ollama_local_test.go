package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// ollamaTestServer wraps an httptest.Server with hooks for capturing chat
// requests and serving canned list / chat responses.
type ollamaTestServer struct {
	srv         *httptest.Server
	listHandler http.HandlerFunc
	chatHandler http.HandlerFunc

	mu            sync.Mutex
	lastChatBody  *localOllamaChatRequest
	chatCallCount int32
}

func newOllamaTestServer(t *testing.T) *ollamaTestServer {
	ts := &ollamaTestServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		if ts.listHandler != nil {
			ts.listHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	})
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&ts.chatCallCount, 1)
		var body localOllamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		ts.mu.Lock()
		ts.lastChatBody = &body
		ts.mu.Unlock()
		if ts.chatHandler != nil {
			ts.chatHandler(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	ts.srv = httptest.NewServer(mux)
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *ollamaTestServer) factory() ollamaClientFactory {
	return func() (ollamaClient, error) {
		return newHTTPClientAt(ts.srv.URL), nil
	}
}

func (ts *ollamaTestServer) chatCalls() int32 {
	return atomic.LoadInt32(&ts.chatCallCount)
}

func (ts *ollamaTestServer) lastBody() *localOllamaChatRequest {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastChatBody
}

func serveJSONList(t *testing.T, models ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := localOllamaListResponse{}
		for _, m := range models {
			out.Models = append(out.Models, localOllamaListModel{Name: m})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(out))
	}
}

// serveSingleChat writes a single (non-streaming) JSON ChatResponse.
func serveSingleChat(t *testing.T, resp localOllamaChatResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}
}

// serveNDJSONChat writes a sequence of newline-delimited ChatResponse chunks.
func serveNDJSONChat(t *testing.T, chunks ...localOllamaChatResponse) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			line, err := json.Marshal(c)
			require.NoError(t, err)
			if _, err := w.Write(line); err != nil {
				return
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func TestOllamaLocalClientSetModelSwitchesModel(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a", "model-b")

	var factoryCalls int32
	factory := func() (ollamaClient, error) {
		atomic.AddInt32(&factoryCalls, 1)
		return newHTTPClientAt(ts.srv.URL), nil
	}

	client, err := newOllamaLocalClientWithFactory("model-a", factory)
	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&factoryCalls))

	err = client.SetModel("model-b")
	require.NoError(t, err)
	require.Equal(t, "model-b", client.GetModel())
	require.Equal(t, int32(2), atomic.LoadInt32(&factoryCalls), "expected factory invocation for SetModel")

	// Setting the same model should be a no-op and avoid calling the factory
	err = client.SetModel("model-b")
	require.NoError(t, err)
	require.Equal(t, int32(2), atomic.LoadInt32(&factoryCalls))
}

func TestOllamaLocalClientSetModelUnknownModel(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")

	factory := func() (ollamaClient, error) { return newHTTPClientAt(ts.srv.URL), nil }

	client, err := newOllamaLocalClientWithFactory("model-a", factory)
	require.NoError(t, err)

	err = client.SetModel("model-missing")
	// With fallback, SetModel should not error and should use fallback model
	require.NoError(t, err)
	require.Equal(t, "model-a", client.GetModel())
}

func TestOllamaLocalClientSetModelEmptyName(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")

	factory := func() (ollamaClient, error) { return newHTTPClientAt(ts.srv.URL), nil }

	client, err := newOllamaLocalClientWithFactory("model-a", factory)
	require.NoError(t, err)

	err = client.SetModel("   ")
	require.Error(t, err)
	require.Equal(t, "model-a", client.GetModel())
}

func TestOllamaLocalClientIncludesStructuredTools(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")
	ts.chatHandler = serveSingleChat(t, localOllamaChatResponse{
		Model:   "model-a",
		Message: localOllamaMessage{Role: "assistant", Content: ""},
		Done:    true,
	})

	client, err := newOllamaLocalClientWithFactory("model-a", ts.factory())
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

	_, err = client.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools, "", false)
	require.NoError(t, err)

	require.Equal(t, int32(1), ts.chatCalls())
	body := ts.lastBody()
	require.NotNil(t, body)
	require.Len(t, body.Tools, 1)
	require.Len(t, body.Messages, 1)
	require.Equal(t, "user", body.Messages[0].Role)
	require.Equal(t, "hi", body.Messages[0].Content)

	reqTool := body.Tools[0]
	require.Equal(t, "function", reqTool.Type)
	require.Equal(t, "shell_command", reqTool.Function.Name)
	require.Equal(t, "Execute shell commands", reqTool.Function.Description)

	var paramsMap map[string]any
	require.NoError(t, json.Unmarshal(reqTool.Function.Parameters, &paramsMap))
	require.Equal(t, "object", paramsMap["type"])

	required, ok := paramsMap["required"].([]any)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"command"}, required)

	props, ok := paramsMap["properties"].(map[string]any)
	require.True(t, ok)
	cmd, ok := props["command"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "string", cmd["type"])
	require.Equal(t, "Shell command to execute", cmd["description"])
}

func TestOllamaLocalClientStreamingEmitsChunks(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")
	ts.chatHandler = serveNDJSONChat(t,
		localOllamaChatResponse{
			Model:   "model-a",
			Message: localOllamaMessage{Role: "assistant", Content: "chunk one "},
		},
		localOllamaChatResponse{
			Model:   "model-a",
			Message: localOllamaMessage{Role: "assistant", Content: "chunk two"},
			Done:    true, DoneReason: "stop",
			Metrics: localOllamaMetrics{PromptEvalCount: 12, EvalCount: 8},
		},
	)

	client, err := newOllamaLocalClientWithFactory("model-a", ts.factory())
	require.NoError(t, err)

	var chunks []string
	resp, err := client.SendChatRequestStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "", false, func(content string, contentType string) {
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

	body := ts.lastBody()
	require.NotNil(t, body)
	require.NotNil(t, body.Stream)
	require.True(t, *body.Stream)
	streamOpt, ok := body.Options["stream"].(bool)
	require.True(t, ok)
	require.True(t, streamOpt)
}

func TestOllamaLocalClientCapturesToolCalls(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")

	toolArgsJSON := `{"args": ["-la"], "command": "ls"}`
	ts.chatHandler = serveSingleChat(t, localOllamaChatResponse{
		Message: localOllamaMessage{
			ToolCalls: []localOllamaToolCall{
				{Function: localOllamaToolCallFunction{
					Name:      "shell_command",
					Arguments: json.RawMessage(toolArgsJSON),
				}},
			},
		},
		Done:       true,
		DoneReason: "tool_calls",
	})

	client, err := newOllamaLocalClientWithFactory("model-a", ts.factory())
	require.NoError(t, err)

	resp, err := client.SendChatRequest(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "", false)
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	require.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	call := resp.Choices[0].Message.ToolCalls[0]
	require.Equal(t, "function", call.Type)
	require.Equal(t, "shell_command", call.Function.Name)
	require.JSONEq(t, toolArgsJSON, call.Function.Arguments)

	body := ts.lastBody()
	require.NotNil(t, body)
	streamOpt, ok := body.Options["stream"].(bool)
	require.True(t, ok)
	require.False(t, streamOpt)
}

func TestOllamaLocalClientStreamingToolCalls(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")

	toolArgsJSON := `{"args": ["-la"], "command": "ls"}`
	ts.chatHandler = serveNDJSONChat(t,
		localOllamaChatResponse{
			Message: localOllamaMessage{
				ToolCalls: []localOllamaToolCall{
					{Function: localOllamaToolCallFunction{
						Name:      "shell_command",
						Arguments: json.RawMessage(toolArgsJSON),
					}},
				},
			},
			Done:       true,
			DoneReason: "tool_calls",
		},
	)

	client, err := newOllamaLocalClientWithFactory("model-a", ts.factory())
	require.NoError(t, err)

	var chunks []string
	resp, err := client.SendChatRequestStream(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "", false, func(content string, contentType string) {
		chunks = append(chunks, content)
	})
	require.NoError(t, err)
	require.Len(t, resp.Choices, 1)
	require.Len(t, resp.Choices[0].Message.ToolCalls, 1)
	require.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
	require.JSONEq(t, toolArgsJSON, resp.Choices[0].Message.ToolCalls[0].Function.Arguments)
	require.Empty(t, chunks)

	body := ts.lastBody()
	require.NotNil(t, body)
	require.NotNil(t, body.Stream)
	require.True(t, *body.Stream)
}

// TestOllamaLocalClientConnects verifies the HTTP client actually talks to a
// running httptest.Server (round-trip sanity check).
func TestOllamaLocalClientConnects(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "qwen2.5:7b")

	c, err := ts.factory()()
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resp, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, resp.Models, 1)
	require.Equal(t, "qwen2.5:7b", resp.Models[0].Name)
}

// TestOllamaLocalClientChatStreamRequest exercises the streaming HTTP path
// directly through the HTTP client (rather than via OllamaLocalClient).
func TestOllamaLocalClientChatStreamRequest(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")
	ts.chatHandler = serveNDJSONChat(t,
		localOllamaChatResponse{
			Model:   "model-a",
			Message: localOllamaMessage{Role: "assistant", Content: "hi"},
			Done:    true,
		},
	)

	c, err := ts.factory()()
	require.NoError(t, err)

	stream := true
	req := &localOllamaChatRequest{
		Model:  "model-a",
		Stream: &stream,
		Messages: []localOllamaMessage{
			{Role: "user", Content: "hello"},
		},
	}

	var received int
	err = c.Chat(context.Background(), req, func(res *localOllamaChatResponse) error {
		received++
		require.Equal(t, "model-a", res.Model)
		return nil
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, received, 1)
}

// TestOllamaLocalClientChatNonStreamRequest verifies the single-shot HTTP
// decode path (stream=false returns one ChatResponse).
func TestOllamaLocalClientChatNonStreamRequest(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")
	ts.chatHandler = serveSingleChat(t, localOllamaChatResponse{
		Message: localOllamaMessage{Role: "assistant", Content: "full response"},
		Done:    true,
	})

	c, err := ts.factory()()
	require.NoError(t, err)

	stream := false
	req := &localOllamaChatRequest{
		Model:  "model-a",
		Stream: &stream,
		Messages: []localOllamaMessage{
			{Role: "user", Content: "hello"},
		},
	}

	var got string
	err = c.Chat(context.Background(), req, func(res *localOllamaChatResponse) error {
		got = res.Message.Content
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "full response", got)
}

// TestOllamaLocalClientListHTTPError verifies that a non-2xx status from
// /api/tags surfaces as an error.
func TestOllamaLocalClientListHTTPError(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "ollama not running", http.StatusInternalServerError)
	}

	c, err := ts.factory()()
	require.NoError(t, err)

	_, err = c.List(context.Background())
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "ollama")
}

// TestOllamaLocalClientChatHTTPError verifies that a non-2xx status from
// /api/chat surfaces as an error.
func TestOllamaLocalClientChatHTTPError(t *testing.T) {
	ts := newOllamaTestServer(t)
	ts.listHandler = serveJSONList(t, "model-a")
	ts.chatHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}

	c, err := ts.factory()()
	require.NoError(t, err)

	stream := false
	req := &localOllamaChatRequest{
		Model:    "model-a",
		Stream:   &stream,
		Messages: []localOllamaMessage{{Role: "user", Content: "x"}},
	}
	err = c.Chat(context.Background(), req, nil)
	require.Error(t, err)
}

// silence unused imports if a future change removes a use
var _ = fmt.Sprintf