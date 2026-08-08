package openaisserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// fakeModel is a scripted Model satisfying the server's interface. It records
// the generation config it saw (to assert max_tokens capping) and produces
// deterministic text/tokens.
type fakeModel struct {
	text      string // GenerateText result
	tokens    []int  // token IDs Generate streams
	tokenText map[int]string
	lastCfg   llm.GenerateConfig
	genErr    error
	chatOut   string // FormatChat output
	ctxLen    int    // ContextLength report
}

func (f *fakeModel) ContextLength() int {
	if f.ctxLen > 0 {
		return f.ctxLen
	}
	return 32_000
}

func (f *fakeModel) FormatChat(messages []llm.ChatMessage) string {
	if f.chatOut != "" {
		return f.chatOut
	}
	return "<|im_start|>assistant\n"
}

func (f *fakeModel) GenerateText(_ context.Context, prompt string, cfg llm.GenerateConfig) (string, error) {
	f.lastCfg = cfg
	if f.genErr != nil {
		return "", f.genErr
	}
	return f.text, nil
}

func (f *fakeModel) Generate(_ context.Context, prompt string, cfg llm.GenerateConfig, onToken func(tokenID int)) error {
	f.lastCfg = cfg
	if f.genErr != nil {
		return f.genErr
	}
	for _, id := range f.tokens {
		onToken(id)
	}
	return nil
}

func (f *fakeModel) DecodeToken(id int) string {
	if s, ok := f.tokenText[id]; ok {
		return s
	}
	return "?"
}

func (f *fakeModel) TokenizerEncode(text string) []int {
	return []int{1, 2, 3}
}

func newFake() *fakeModel {
	return &fakeModel{
		text:   "Hello from the local model",
		tokens: []int{7, 8, 9},
		tokenText: map[int]string{
			7: "Hello",
			8: " ",
			9: "world",
		},
	}
}

func newServer(t *testing.T, f *fakeModel, cap int) *httptest.Server {
	t.Helper()
	srv := New(f, "qwen3_5_text-local-2560-32", cap)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func postChat(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestChatNonStreaming checks the JSON shape: message content, finish_reason,
// model echo, and usage counts.
func TestChatNonStreaming(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 0)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var body chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID != "chatcmpl-local" {
		t.Errorf("id = %q", body.ID)
	}
	if body.Object != "chat.completion" {
		t.Errorf("object = %q", body.Object)
	}
	if body.Model != "local" {
		t.Errorf("model = %q, want request model echo", body.Model)
	}
	if len(body.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(body.Choices))
	}
	if body.Choices[0].Message.Content != "Hello from the local model" {
		t.Errorf("content = %q", body.Choices[0].Message.Content)
	}
	if body.Choices[0].Message.Role != "assistant" {
		t.Errorf("role = %q", body.Choices[0].Message.Role)
	}
	if body.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q", body.Choices[0].FinishReason)
	}
	if body.Usage.PromptTokens != 3 {
		t.Errorf("prompt_tokens = %d, want 3", body.Usage.PromptTokens)
	}
}

// TestChatMaxTokensCap verifies the server caps max_tokens to the configured
// limit, even when the request asks for more.
func TestChatMaxTokensCap(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 32)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[{"role":"user","content":"hi"}],"max_tokens":5000}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	if f.lastCfg.MaxTokens != 32 {
		t.Errorf("cfg.MaxTokens = %d, want capped 32", f.lastCfg.MaxTokens)
	}
}

// TestChatMaxTokensHonored verifies a request below the cap is not clamped.
func TestChatMaxTokensHonored(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 512)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[{"role":"user","content":"hi"}],"max_tokens":64}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)
	if f.lastCfg.MaxTokens != 64 {
		t.Errorf("cfg.MaxTokens = %d, want 64", f.lastCfg.MaxTokens)
	}
}

// TestChatEmptyMessages verifies a request with no messages is rejected.
func TestChatEmptyMessages(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 0)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestChatGenerationError verifies a model failure becomes an HTTP 500.
func TestChatGenerationError(t *testing.T) {
	f := newFake()
	f.genErr = context.DeadlineExceeded
	ts := newServer(t, f, 0)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[{"role":"user","content":"hi"}]}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

// TestStreaming verifies the SSE framing: role chunk, content deltas,
// finish_reason chunk, and the [DONE] terminator.
func TestStreaming(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 0)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	var chunks []streamChunk
	var lines []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			lines = append(lines, payload)
			continue
		}
		var c streamChunk
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			t.Fatalf("bad chunk %q: %v", payload, err)
		}
		chunks = append(chunks, c)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// [DONE] must be the last line.
	if len(lines) == 0 || lines[len(lines)-1] != "[DONE]" {
		t.Fatalf("missing [DONE] terminator; lines=%v", lines)
	}

	// First chunk carries the role.
	if len(chunks) == 0 {
		t.Fatal("no chunks")
	}
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk role = %q", chunks[0].Choices[0].Delta.Role)
	}

	// Content deltas follow the fake's token stream.
	var content strings.Builder
	for _, c := range chunks[1:] {
		content.WriteString(c.Choices[0].Delta.Content)
	}
	if got := content.String(); got != "Hello world" {
		t.Errorf("streamed content = %q, want %q", got, "Hello world")
	}

	// Last chunk has finish_reason=stop.
	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "stop" {
		t.Errorf("final finish_reason = %v, want stop", last.Choices[0].FinishReason)
	}
}

// TestStreamingGenerationError verifies a model failure mid-stream emits an
// error chunk and still terminates with [DONE].
func TestStreamingGenerationError(t *testing.T) {
	f := newFake()
	f.genErr = context.Canceled
	ts := newServer(t, f, 0)

	resp := postChat(t, ts.URL, `{"model":"local","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SSE errors are in-band)", resp.StatusCode)
	}

	var sawError, sawDone bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		switch {
		case payload == "[DONE]":
			sawDone = true
		case strings.Contains(payload, "error"):
			sawError = true
		}
	}
	if !sawError {
		t.Error("missing in-band error chunk")
	}
	if !sawDone {
		t.Error("missing [DONE] after error")
	}
}

// TestModels verifies /v1/models returns the server's model name.
func TestModels(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 0)

	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var body modelList
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != "qwen3_5_text-local-2560-32" {
		t.Errorf("data = %+v", body.Data)
	}
	if body.Data[0].ContextLength == 0 {
		t.Errorf("context_length missing from /v1/models — sprout's LCM auto-detect relies on it")
	}
}

// TestHealth verifies /health reports ok + the model name.
func TestHealth(t *testing.T) {
	f := newFake()
	ts := newServer(t, f, 0)

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q", body["status"])
	}
	if body["model"] != "qwen3_5_text-local-2560-32" {
		t.Errorf("model = %q", body["model"])
	}
}
