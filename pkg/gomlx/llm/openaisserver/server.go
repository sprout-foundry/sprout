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
}

// chatRequest mirrors the OpenAI chat-completions request body fields sprout
// sends. Unknown fields are ignored.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature *float64      `json:"temperature"`
	TopP        *float64      `json:"top_p"`
	MaxTokens   *int          `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	Role    string `json:"role"`
	Content string `json:"content"`
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
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
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
	prompt := s.model.FormatChat(msgs)

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

	if req.Stream {
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

	resp := chatResponse{
		ID:      "chatcmpl-local",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []chatChoice{{
			Index:        0,
			Message:      chatRespMsg{Role: "assistant", Content: text},
			FinishReason: "stop",
		}},
		Usage: usage{
			PromptTokens:     len(s.model.TokenizerEncode(prompt)),
			CompletionTokens: cfg.MaxTokens,
			TotalTokens:      len(s.model.TokenizerEncode(prompt)) + cfg.MaxTokens,
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

	// Stream token-by-token. GenerateText would buffer; use the onToken
	// callback variant to stream deltas as they decode.
	err := s.model.Generate(r.Context(), prompt, cfg, func(tokenID int) {
		tok := s.model.DecodeToken(tokenID)
		_ = send(streamChunk{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
			Choices: []streamChoice{{Index: 0, Delta: streamDelta{Content: tok}, FinishReason: nil}},
		})
	})
	if err != nil {
		// Send the error as a data chunk (OpenAI style) then [DONE].
		_ = send(map[string]string{"error": err.Error()})
	}

	stop := "stop"
	_ = send(streamChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
		Choices: []streamChoice{{Index: 0, Delta: streamDelta{}, FinishReason: &stop}},
	})
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) HandleModels(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(modelList{
		Object: "list",
		Data: []modelInfo{{
			ID:      s.modelName,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: "sprout-local",
		}},
	})
}

// HandleHealth reports whether the server is up and which model is loaded.
// Lightweight and lock-free so it always answers even while a generation is
// in flight.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"model":  s.modelName,
	})
}
