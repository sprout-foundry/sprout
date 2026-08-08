//go:build darwin && arm64 && cgo && mlx

// Command llm_server exposes the local gomlx LLM engine over an
// OpenAI-compatible HTTP API (POST /v1/chat/completions, GET /v1/models),
// so sprout can use it as a provider via the generic provider machinery
// (like LM Studio / Ollama local endpoints).
//
// Usage:
//
//	GO_QUANTIZE=4 go run -tags mlx ./cmd/llm_server -model ~/.cache/sprout/models/qwen3-1.7b -port 8080
//
// Then configure sprout with a provider whose endpoint is
// http://127.0.0.1:8080/v1/chat/completions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen35"
)

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

type server struct {
	model     *llm.Model
	modelName string
	mu        sync.Mutex // serializes generation (single GPU model, one request at a time)
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
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

	// Build the prompt via the Qwen3 chat template.
	msgs := make([]llm.ChatMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = llm.ChatMessage{Role: m.Role, Content: m.Content}
	}
	prompt := s.model.FormatChat(msgs)

	cfg := llm.DefaultGenerateConfig()
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		cfg.MaxTokens = *req.MaxTokens
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

func (s *server) streamChat(w http.ResponseWriter, r *http.Request, prompt string, cfg llm.GenerateConfig, req chatRequest) {
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

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
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

func main() {
	modelDir := flag.String("model", "", "path to the model directory (default: ~/.cache/sprout/models/qwen3-1.7b)")
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	dir := *modelDir
	if dir == "" {
		dir = os.Getenv("HOME") + "/.cache/sprout/models/qwen3-1.7b"
	}

	log.Printf("loading model from %s ...", dir)
	model, err := llm.NewModel(dir)
	if err != nil {
		log.Fatalf("load model: %v", err)
	}
	defer model.Close()

	name := "local"
	if cfg := model.Config(); cfg.Arch != "" {
		name = fmt.Sprintf("%s-local-%d-%d", cfg.Arch, cfg.HiddenSize, cfg.NumLayers)
	}

	srv := &server{model: model, modelName: name}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", srv.handleChat)
	mux.HandleFunc("/v1/models", srv.handleModels)

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	httpSrv := &http.Server{Addr: addr, Handler: mux}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Println("shutting down...")
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	log.Printf("llm server listening on http://%s (model %s)", addr, name)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}
