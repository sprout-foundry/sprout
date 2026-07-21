// Package agent_api: wire-type structs for Ollama local API communication (split from ollama_local.go)
package api

import (
	"encoding/json"
	"time"
)

// localOllamaListResponse is the JSON shape returned by Ollama's GET /api/tags.
type localOllamaListResponse struct {
	Models []localOllamaListModel `json:"models"`
}

// localOllamaListModel describes one entry in the local model list.
type localOllamaListModel struct {
	Name string `json:"name"`
}

// localOllamaShowResponse is the JSON shape returned by Ollama's POST /api/show.
type localOllamaShowResponse struct {
	ModelInfo localOllamaModelInfo `json:"model_info"`
}

// localOllamaModelInfo carries model metadata reported by Ollama.
type localOllamaModelInfo struct {
	ContextLength int `json:"context_length"`
}

// localOllamaChatRequest mirrors the JSON body POSTed to /api/chat.
type localOllamaChatRequest struct {
	Model     string               `json:"model"`
	Messages  []localOllamaMessage `json:"messages"`
	Options   map[string]any       `json:"options,omitempty"`
	Tools     []localOllamaTool    `json:"tools,omitempty"`
	Stream    *bool                `json:"stream,omitempty"`
	Format    map[string]any       `json:"format,omitempty"`
	KeepAlive string               `json:"keep_alive,omitempty"`
}

// localOllamaMessage is one entry in a chat request or response.
type localOllamaMessage struct {
	Role      string                `json:"role"`
	Content   string                `json:"content"`
	Images    [][]byte              `json:"images,omitempty"`
	ToolCalls []localOllamaToolCall `json:"tool_calls,omitempty"`
}

// localOllamaTool mirrors Ollama's tool schema.
type localOllamaTool struct {
	Type     string                  `json:"type"`
	Function localOllamaToolFunction `json:"function"`
}

// localOllamaToolFunction is the callable portion of a tool.
type localOllamaToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// localOllamaToolCall describes one tool invocation returned by the model.
type localOllamaToolCall struct {
	Function localOllamaToolCallFunction `json:"function"`
}

// localOllamaToolCallFunction carries the name + raw JSON arguments.
type localOllamaToolCallFunction struct {
	Index     int             `json:"index"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// localOllamaChatResponse is one NDJSON line of /api/chat output.
type localOllamaChatResponse struct {
	Model      string             `json:"model"`
	CreatedAt  time.Time          `json:"created_at"`
	Message    localOllamaMessage `json:"message"`
	Done       bool               `json:"done"`
	DoneReason string             `json:"done_reason"`
	Metrics    localOllamaMetrics `json:"metrics,omitempty"`
}

// localOllamaMetrics carries the model's reported token counts.
type localOllamaMetrics struct {
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}
