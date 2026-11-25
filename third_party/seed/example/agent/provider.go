// provider provides a minimal OpenAI-compatible HTTP provider.
//
// REAL IMPLEMENTATION NOTES:
//
//   - Validate endpoints against a whitelist or require HTTPS (no file:// gopher://)
//   - Redact API keys in error messages and logs
//   - Implement proper token estimation (not char-count / 4)
//   - Support streaming via ChatStream() with Server-Sent Events parsing
//   - Handle retry on 429/5xx with exponential backoff
//   - Enforce per-request token budgets to avoid runaway API costs
//   - Add request/response logging for auditing
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/seed/core"
)

// openAIProv is a minimal OpenAI-compatible provider.
type openAIProv struct {
	ep  string // endpoint base URL (e.g. https://api.openai.com/v1)
	key string // API key from environment
	// REAL: These should be validated on construction — cap contextSize,
	// reject empty model names, etc.
	model     string
	ctxsz     int
	hasVision bool
	cli       *http.Client
}

func newProvider(params map[string]interface{}) (*openAIProv, error) {
	raw, ok := params["provider"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing provider object")
	}

	ep := sVal(raw, "endpoint")
	if ep == "" {
		ep = "https://api.openai.com/v1"
	}

	// REAL: Only allow a whitelist of env var names (e.g. OPENAI_API_KEY,
	// ANTHROPIC_API_KEY) — don't let arbitrary env vars be read.
	env := sVal(raw, "apiKeyEnv")
	if env == "" {
		env = "OPENAI_API_KEY"
	}

	key := os.Getenv(env)
	if key == "" {
		return nil, fmt.Errorf("env %s not set", env)
	}

	model := sVal(raw, "model")
	if model == "" {
		model = "gpt-4o"
	}

	// REAL: Cap contextSize to prevent unreasonably large values.
	ctxsz := iVal(raw, "contextSize")
	if ctxsz == 0 {
		ctxsz = 128000
	}

	// Check for vision capability in the capabilities list.
	hv := false
	if caps, ok := raw["capabilities"]; ok {
		if arr, ok := caps.([]interface{}); ok {
			for _, c := range arr {
				if s, ok := c.(string); ok && s == "vision" {
					hv = true
				}
			}
		}
	}

	return &openAIProv{
		ep:        ep,
		key:       key,
		model:     model,
		ctxsz:     ctxsz,
		hasVision: hv,
		cli:       &http.Client{Timeout: 180 * time.Second},
	}, nil
}

// --- Wire types for OpenAI API ---

type wReq struct {
	Model     string  `json:"model"`
	Messages  []wMsg  `json:"messages"`
	Tools     []wTool `json:"tools,omitempty"`
	MaxTokens int     `json:"max_tokens,omitempty"`
}

type wMsg struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content,omitempty"`
	TCID    string      `json:"tool_call_id,omitempty"`
	TCs     []wTC       `json:"tool_calls,omitempty"`
}

type wTC struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Fn   wFn    `json:"function"`
}

type wFn struct {
	Name string `json:"name"`
	Args string `json:"arguments"`
}

type wTool struct {
	Type string   `json:"type"`
	Fn   wToolDef `json:"function"`
}

type wToolDef struct {
	Name   string      `json:"name"`
	Desc   string      `json:"description"`
	Params interface{} `json:"parameters"`
}

type wResp struct {
	Model   string `json:"model"`
	Choices []wCh  `json:"choices"`
	Usage   wUsage `json:"usage"`
}

type wCh struct {
	Msg    wMsg   `json:"message"`
	Finish string `json:"finish_reason"`
}

type wUsage struct {
	Prompt int `json:"prompt_tokens"`
	Comp   int `json:"completion_tokens"`
	Total  int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Provider interface
// ---------------------------------------------------------------------------

func (p *openAIProv) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	w := p.build(req)
	body, _ := json.Marshal(w)

	u := strings.TrimRight(p.ep, "/") + "/chat/completions"
	hreq, _ := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+p.key)

	resp, err := p.cli.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// REAL: Redact any potential secret in the response snippet.
		snip := string(rb)
		if len(snip) > 500 {
			snip = snip[:500]
		}
		// REAL: Map status codes to typed errors (TransientError, etc.)
		// so the retry layer can distinguish retryable vs. fatal.
		switch {
		case resp.StatusCode == 401:
			return nil, fmt.Errorf("auth error: HTTP %d", resp.StatusCode)
		case resp.StatusCode == 429:
			return nil, fmt.Errorf("rate limit: HTTP %d", resp.StatusCode)
		case resp.StatusCode >= 500:
			return nil, fmt.Errorf("server error: HTTP %d", resp.StatusCode)
		default:
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snip)
		}
	}

	var wr wResp
	if err := json.Unmarshal(rb, &wr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return p.parse(&wr), nil
}

func (p *openAIProv) ChatStream(_ context.Context, _ *core.ChatRequest, _ core.StreamHandler) error {
	// REAL: Implement streaming by opening an SSE connection to the
	// /chat/completions endpoint with stream=true, parsing "data:" lines,
	// and calling handler.OnContent() / handler.OnDone() for each chunk.
	return fmt.Errorf("streaming not supported in this example")
}

func (p *openAIProv) Info() core.ProviderInfo {
	return core.ProviderInfo{
		Model:       p.model,
		ContextSize: p.ctxsz,
		HasVision:   p.hasVision,
	}
}

func (p *openAIProv) EstimateTokens(req *core.ChatRequest) int {
	// REAL: Use a proper tokenizer (tiktoken) for accurate estimates.
	// This rough approximation is only illustrative.
	t := 0
	for _, m := range req.Messages {
		t += len(m.Content)
	}
	return t / 4
}

// ---------------------------------------------------------------------------
// Wire conversion
// ---------------------------------------------------------------------------

func (p *openAIProv) build(req *core.ChatRequest) wReq {
	model := req.Model
	if model == "" {
		model = p.model
	}

	msgs := make([]wMsg, 0, len(req.Messages))
	for _, msg := range req.Messages {
		wm := wMsg{Role: msg.Role, TCID: msg.ToolCallID}

		if len(msg.Images) > 0 && p.hasVision {
			parts := make([]interface{}, 0, 1+len(msg.Images))
			parts = append(parts, map[string]interface{}{"type": "text", "text": msg.Content})
			for _, img := range msg.Images {
				if img.URL != "" {
					parts = append(parts, map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": img.URL},
					})
				} else if img.Base64 != "" {
					mime := img.Type
					if mime == "" {
						mime = "image/png"
					}
					parts = append(parts, map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:" + mime + ";base64," + img.Base64,
						},
					})
				}
			}
			wm.Content = parts
		} else {
			wm.Content = msg.Content
		}

		for _, tc := range msg.ToolCalls {
			wm.TCs = append(wm.TCs, wTC{
				ID:   tc.ID,
				Type: "function",
				Fn:   wFn{Name: tc.Function.Name, Args: tc.Function.Arguments},
			})
		}
		msgs = append(msgs, wm)
	}

	var tools []wTool
	for _, t := range req.Tools {
		tools = append(tools, wTool{
			Type: "function",
			Fn: wToolDef{
				Name:   t.Function.Name,
				Desc:   t.Function.Description,
				Params: t.Function.Parameters,
			},
		})
	}

	return wReq{Model: model, Messages: msgs, Tools: tools, MaxTokens: req.MaxTokens}
}

func (p *openAIProv) parse(wr *wResp) *core.ChatResponse {
	cr := &core.ChatResponse{
		Model: wr.Model,
		Usage: core.ChatUsage{
			PromptTokens:     wr.Usage.Prompt,
			CompletionTokens: wr.Usage.Comp,
			TotalTokens:      wr.Usage.Total,
		},
	}

	if len(wr.Choices) > 0 {
		ch := wr.Choices[0]
		msg := core.Message{Role: ch.Msg.Role}
		if s, ok := ch.Msg.Content.(string); ok {
			msg.Content = s
		}
		for _, tc := range ch.Msg.TCs {
			msg.ToolCalls = append(msg.ToolCalls, core.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: core.ToolCallFunction{
					Name:      tc.Fn.Name,
					Arguments: tc.Fn.Args,
				},
			})
		}
		cr.Choices = []core.ChatChoice{{Message: msg, FinishReason: ch.Finish}}
	}
	return cr
}
