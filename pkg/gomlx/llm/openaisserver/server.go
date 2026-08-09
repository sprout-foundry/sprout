// Package openaisserver exposes the gomlx LLM engine over an
// OpenAI-compatible HTTP API (POST /v1/chat/completions, GET /v1/models,
// GET /health) so sprout can use it as a provider via the generic provider
// machinery (like LM Studio / Ollama local endpoints).
//
// The package depends only on a small Model interface, so the HTTP contract
// (SSE framing, [DONE] marker, model discovery, max_tokens cap, error
// paths) is testable with a fake model — no MLX runtime or real weights
// required. cmd/llm_server loads a real model and calls New.
package openaisserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// Model is the inference surface the server needs. *llm.Model satisfies it;
// tests use a fake.
type Model interface {
	FormatChat(messages []llm.ChatMessage) string
	GenerateText(ctx context.Context, prompt string, genCfg llm.GenerateConfig) (string, error)
	Generate(ctx context.Context, prompt string, genCfg llm.GenerateConfig, onToken func(tokenID int)) error
	DecodeToken(id int) string
	TokenizerEncode(text string) []int

	// ContextLength returns the effective context window the model can
	// handle (prompt + generated). The server advertises it via /v1/models
	// so sprout's context-profile auto-detection (LCM for small windows)
	// works without relying on a provider-side fallback.
	ContextLength() int
}

// chatRequest mirrors the OpenAI chat-completions request body fields sprout
// sends. Unknown fields are ignored.
type chatRequest struct {
	Model       string          `json:"model"`
	Messages    []chatMessage   `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature"`
	TopP        *float64        `json:"top_p"`
	MaxTokens   *int            `json:"max_tokens"`
	Tools       []toolDef       `json:"tools,omitempty"`
}

type toolDef struct {
	Type     string       `json:"type"` // always "function"
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCalls  []toolCallChunk `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// toolCallChunk mirrors the OpenAI tool_call object in responses.
type toolCallChunk struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// chatResponse mirrors the OpenAI non-streaming response shape.
type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   usage        `json:"usage"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatRespMsg `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatRespMsg struct {
	Role      string          `json:"role"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []toolCallChunk `json:"tool_calls,omitempty"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// streamChunk mirrors the OpenAI streaming SSE chunk.
type streamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []streamChoice `json:"choices"`
}

type streamChoice struct {
	Index        int         `json:"index"`
	Delta        streamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type streamDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type modelInfo struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	Created       int64  `json:"created"`
	OwnedBy       string `json:"owned_by"`
	ContextLength int    `json:"context_length,omitempty"`
}

type modelList struct {
	Object string      `json:"object"`
	Data   []modelInfo `json:"data"`
}

// Server is the OpenAI-compatible HTTP handler.
type Server struct {
	model     Model
	modelName string
	// MaxTokensCap is the maximum max_tokens the server honors per request
	// (0 = no cap). Prevents a connection-check or runaway request from
	// generating thousands of tokens on a RAM-constrained machine.
	MaxTokensCap int
	mu           sync.Mutex // serializes generation (single GPU model, one request at a time)
}

// New creates a Server for the given model. modelName is what /v1/models and
// /health report (e.g. "qwen3_5_text-local-2560-32").
func New(model Model, modelName string, maxTokensCap int) *Server {
	return &Server{model: model, modelName: modelName, MaxTokensCap: maxTokensCap}
}

// Handler returns an http.Handler with the standard routes mounted.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.HandleChat)
	mux.HandleFunc("/v1/models", s.HandleModels)
	mux.HandleFunc("/health", s.HandleHealth)
	return mux
}

func (s *Server) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("bad request: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		http.Error(w, "messages required", http.StatusBadRequest)
		return
	}

	// Build the prompt via the chat template.
	msgs := make([]llm.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = llm.ChatMessage{Role: m.Role, Content: m.Content}
	}

	// If tools are provided, inject them into the system prompt using the
	// Qwen3.5 tool-calling template. The fine-tuned model was trained with
	// this exact format.
	toolsJSON := ""
	if len(req.Tools) > 0 {
		toolsJSON = formatToolsPrompt(req.Tools)
	}

	// Convert tool_calls and tool messages to text for the model.
	// OpenAI sends: assistant message with tool_calls, then tool result as
	// role="tool" message. We convert these to the Qwen3.5 format:
	// <tool_call>...</tool_call> and <tool_response>...</tool_response>.
	msgs = convertToolMessages(msgs, req.Messages)

	prompt := s.model.FormatChat(msgs)
	if toolsJSON != "" {
		prompt = toolsJSON + prompt
	}

	cfg := llm.DefaultGenerateConfig()
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		cfg.MaxTokens = *req.MaxTokens
	}
	if s.MaxTokensCap > 0 && cfg.MaxTokens > s.MaxTokensCap {
		cfg.MaxTokens = s.MaxTokensCap
	}
	if req.Temperature != nil {
		cfg.Temperature = float32(*req.Temperature)
	}
	if req.TopP != nil {
		cfg.TopP = float32(*req.TopP)
	}

	// When tools are provided, always use non-streaming internally so we can
	// parse the complete tool_call output. Sprout handles both streaming and
	// non-streaming responses; the tool_call XML must be complete before we
	// can parse it, so incremental streaming isn't viable.
	hasTools := len(req.Tools) > 0
	if req.Stream && !hasTools {
		s.streamChat(w, r, prompt, cfg, req)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	text, err := s.model.GenerateText(r.Context(), prompt, cfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Parse tool calls from the model output. The fine-tuned model emits
	// <tool_call><function=name><parameter=key>value</parameter></function></tool_call>
	content, toolCalls := parseToolCalls(text)
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	resp := chatResponse{
		ID:      "chatcmpl-local",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      chatRespMsg{Role: "assistant", Content: content, ToolCalls: toolCalls},
			FinishReason: finishReason,
		}},
		Usage: usage{
			PromptTokens:     len(s.model.TokenizerEncode(prompt)),
			CompletionTokens: len(s.model.TokenizerEncode(text)),
			TotalTokens:      len(s.model.TokenizerEncode(prompt)) + len(s.model.TokenizerEncode(text)),
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, prompt string, cfg llm.GenerateConfig, req chatRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	id := "chatcmpl-local"
	created := time.Now().Unix()
	send := func(v interface{}) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Initial role chunk (OpenAI sends this first).
	if err := send(streamChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
		Choices: []streamChoice{{Index: 0, Delta: streamDelta{Role: "assistant"}, FinishReason: nil}},
	}); err != nil {
		return
	}

	// Stream token-by-token. Buffer the output to detect tool calls.
	// If the model emits a <tool_call>, we convert it to the OpenAI
	// tool_calls delta format instead of streaming it as content.
	var outputBuf strings.Builder
	err := s.model.Generate(r.Context(), prompt, cfg, func(tokenID int) {
		tok := s.model.DecodeToken(tokenID)
		outputBuf.WriteString(tok)
		// Stream content as-is; tool call parsing happens post-hoc.
		// (Full streaming tool-call support would require incremental XML
		// parsing, which is complex. For now we stream raw text and let
		// the non-streaming parser handle tool calls in the final chunk.)
		_ = send(streamChunk{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
			Choices: []streamChoice{{Index: 0, Delta: streamDelta{Content: tok}, FinishReason: nil}},
		})
	})
	if err != nil {
		_ = send(map[string]string{"error": err.Error()})
	}

	// Check if the output contains tool calls
	_, toolCalls := parseToolCalls(outputBuf.String())
	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	}
	_ = send(streamChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
		Choices: []streamChoice{{Index: 0, Delta: streamDelta{}, FinishReason: &finish}},
	})
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) HandleModels(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(modelList{
		Object: "list",
		Data: []modelInfo{{
			ID:            s.modelName,
			Object:        "model",
			Created:       time.Now().Unix(),
			OwnedBy:       "sprout-local",
			ContextLength: s.model.ContextLength(),
		}},
	})
}

// HandleHealth reports whether the server is up and which model is loaded.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"model":  s.modelName,
	})
}

// formatToolsPrompt builds the Qwen3.5 tool-calling system prompt from the
// OpenAI tool definitions. The format matches the chat_template.jinja used
// during fine-tuning: tools are listed as JSON, with instructions to use
// <tool_call> XML format.
func formatToolsPrompt(tools []toolDef) string {
	var sb strings.Builder
	sb.WriteString("<|im_start|>system\n# Tools\n\nYou have access to the following functions:\n\n<tools>")
	for _, tool := range tools {
		j, _ := json.Marshal(tool)
		sb.WriteString("\n")
		sb.Write(j)
	}
	sb.WriteString("\n</tools>")
	sb.WriteString("\n\nIf you choose to call a function ONLY reply in the following format with NO suffix:\n\n")
	sb.WriteString("<tool_call>\n<function=example_function_name>\n<parameter=example_parameter_1>\nvalue_1\n</parameter>\n<parameter=example_parameter_2>\nThis is the value for the second parameter\nthat can span\nmultiple lines\n</parameter>\n</function>\n</tool_call>\n\n")
	sb.WriteString("<IMPORTANT>\nReminder:\n- Function calls MUST follow the specified format: an inner <function=...></function> block must be nested within <tool_call></tool_call> XML tags\n- Required parameters MUST be specified\n- You may provide optional reasoning for your function call in natural language BEFORE the function call, but NOT after\n- If there is no function call available, answer the question like normal with your current knowledge and do not tell the user about function calls\n</IMPORTANT>")
	sb.WriteString("<|im_end|>\n")
	return sb.String()
}

// convertToolMessages converts OpenAI tool_calls and tool result messages to
// the Qwen3.5 text format the model was trained on:
//   - assistant tool_calls → <tool_call><function=...>...</function></tool_call>
//   - tool result messages → <tool_response>...</tool_response> wrapped as user
func convertToolMessages(msgs []llm.ChatMessage, raw []chatMessage) []llm.ChatMessage {
	for i, raw := range raw {
		if raw.Role == "assistant" && len(raw.ToolCalls) > 0 {
			var sb strings.Builder
			for _, tc := range raw.ToolCalls {
				sb.WriteString("<tool_call>\n")
				sb.WriteString("<function=" + tc.Function.Name + ">\n")
				// Parse the arguments JSON and emit as <parameter> tags
				var args map[string]interface{}
				if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
					for k, v := range args {
						sb.WriteString("<parameter=" + k + ">\n")
						vBytes, _ := json.Marshal(v)
						// Trim surrounding quotes for string values
						vStr := string(vBytes)
						if len(vStr) >= 2 && vStr[0] == '"' && vStr[len(vStr)-1] == '"' {
							vStr = vStr[1 : len(vStr)-1]
						}
						sb.WriteString(vStr)
						sb.WriteString("\n</parameter>\n")
					}
				}
				sb.WriteString("</function>\n</tool_call>\n")
			}
			msgs[i].Content = sb.String()
		} else if raw.Role == "tool" {
			msgs[i].Role = "user"
			msgs[i].Content = "<tool_response>\n" + raw.Content + "\n</tool_response>"
		}
	}
	return msgs
}

// parseToolCalls extracts <tool_call> blocks from the model's text output.
// Returns the text with tool calls removed, and a slice of parsed tool calls
// in the OpenAI format.
//
// The fine-tuned model emits:
//
//	<tool_call>
//	<function=tool_name>
//	<parameter=key1>value1</parameter>
//	<parameter=key2>value2</parameter>
//	</function>
//	</tool_call>
var toolCallRe = regexp.MustCompile(`(?s)<tool_call>\s*<function=(\w+)>\s*(.*?)</function>\s*</tool_call>`)

func parseToolCalls(text string) (content string, toolCalls []toolCallChunk) {
	matches := toolCallRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}

	paramRe := regexp.MustCompile(`(?s)<parameter=(\w+)>\s*(.*?)</parameter>`)

	// Extract text before the first tool call as content
	if matches[0][0] > 0 {
		content = strings.TrimSpace(text[:matches[0][0]])
	}

	for i, m := range matches {
		funcName := text[m[2]:m[3]]
		paramsBlock := text[m[4]:m[5]]

		// Parse <parameter=key>value</parameter> blocks
		params := map[string]interface{}{}
		for _, pm := range paramRe.FindAllStringSubmatch(paramsBlock, -1) {
			key := pm[1]
			val := strings.TrimSpace(pm[2])
			// Try to parse as JSON (numbers, booleans, arrays)
			var parsed interface{}
			if err := json.Unmarshal([]byte(val), &parsed); err == nil {
				params[key] = parsed
			} else {
				params[key] = val
			}
		}

		argsJSON, _ := json.Marshal(params)
		toolCalls = append(toolCalls, toolCallChunk{
			ID:   "call_" + strconv.Itoa(i),
			Type: "function",
			Function: toolCallFunction{
				Name:      funcName,
				Arguments: string(argsJSON),
			},
		})
	}

	return content, toolCalls
}
