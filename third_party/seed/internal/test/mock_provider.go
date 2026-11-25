package test

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/seed/core"
)

// MockProvider is a scriptable mock implementation of core.Provider.
// Configure it with a sequence of responses via AddResponse or SetResponses.
// It records every Chat call for assertion.
type MockProvider struct {
	mu         sync.Mutex
	responses  []*core.ChatResponse
	errors     []error
	respIndex  int
	info       core.ProviderInfo
	tokenCount int

	// blockUntil, when non-nil, causes the next Chat call to block
	// until the channel is closed (useful for cancellation tests).
	blockUntil chan struct{}

	// blockOnCall, when set, blocks on the Nth call (1-indexed).
	// blockOnCh is the channel to wait on; close it to unblock.
	blockOnCall int
	blockOnCh   chan struct{}

	// Recorded calls for assertion
	Calls []*core.ChatRequest

	// Stream configuration
	streaming      bool // when true, ChatStream delivers chunks via StreamHandler
	streamDelay    time.Duration
	streamChunks   [][]string // pre-configured chunk sequences per call index
	streamChunkIdx int
}

// NewMockProvider creates a MockProvider with default info.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		info: core.ProviderInfo{
			Model:       "mock-model",
			ContextSize: 128000,
			HasVision:   false,
		},
		tokenCount: 100,
	}
}

// WithInfo sets the provider info.
func (m *MockProvider) WithInfo(info core.ProviderInfo) *MockProvider {
	m.info = info
	return m
}

// WithTokenEstimate sets the token estimate returned by EstimateTokens.
func (m *MockProvider) WithTokenEstimate(count int) *MockProvider {
	m.tokenCount = count
	return m
}

// BlockUntil causes the next Chat call to block until the returned channel
// is closed. Use this in cancellation tests: cancel the channel to unblock
// the provider so the context cancellation is observed.
func (m *MockProvider) BlockUntil() chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan struct{})
	m.blockUntil = ch
	return ch
}

// BlockOnCallN causes the Nth Chat call (1-indexed) to block until the
// returned channel is closed. This is useful for cancellation tests where
// you want the first N-1 calls to succeed normally.
func (m *MockProvider) BlockOnCallN(n int) chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan struct{})
	m.blockOnCall = n
	m.blockOnCh = ch
	return ch
}

// AddResponse appends a response to the sequence.
// The next call to Chat will return this response.
func (m *MockProvider) AddResponse(resp *core.ChatResponse) *MockProvider {
	m.responses = append(m.responses, resp)
	m.errors = append(m.errors, nil)
	return m
}

// AddError appends an error to the sequence.
// The next call to Chat will return this error.
func (m *MockProvider) AddError(err error) *MockProvider {
	m.responses = append(m.responses, nil)
	m.errors = append(m.errors, err)
	return m
}

// SetResponses replaces the entire response sequence.
func (m *MockProvider) SetResponses(resps []*core.ChatResponse) *MockProvider {
	m.responses = resps
	m.errors = make([]error, len(resps))
	return m
}

// AddToolCallResponse adds a response that includes tool calls.
func (m *MockProvider) AddToolCallResponse(content string, toolCalls ...core.ToolCall) *MockProvider {
	m.AddResponse(&core.ChatResponse{
		Choices: []core.ChatChoice{{
			Message: core.Message{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls,
			},
		}},
		Usage: core.ChatUsage{
			PromptTokens:     50,
			CompletionTokens: 30,
			TotalTokens:      80,
		},
	})
	return m
}

// AddTextResponse adds a simple text response (no tool calls).
func (m *MockProvider) AddTextResponse(content string) *MockProvider {
	m.AddResponse(&core.ChatResponse{
		Choices: []core.ChatChoice{{
			Message: core.Message{
				Role:    "assistant",
				Content: content,
			},
		}},
		Usage: core.ChatUsage{
			PromptTokens:     20,
			CompletionTokens: 15,
			TotalTokens:      35,
		},
	})
	return m
}

// AddTextResponseWithFinish adds a simple text response with a custom finish reason.
// Useful for testing finish-reason-specific behavior (e.g., "length", "stop", "content_filter").
func (m *MockProvider) AddTextResponseWithFinish(content, finishReason string) *MockProvider {
	m.AddResponse(&core.ChatResponse{
		Choices: []core.ChatChoice{{
			Message: core.Message{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: finishReason,
		}},
		Usage: core.ChatUsage{
			PromptTokens:     20,
			CompletionTokens: 15,
			TotalTokens:      35,
		},
	})
	return m
}

// AddMalformedResponse adds a response where tool calls are embedded in the
// content string rather than in the structured ToolCalls field. This simulates
// a malformed LLM response that the fallback parser must recover from.
// The content should contain tool-call-like patterns (e.g., JSON with
// "tool_calls", XML <function=...>, etc.) so the fallback parser detects them.
func (m *MockProvider) AddMalformedResponse(content string) *MockProvider {
	m.AddResponse(&core.ChatResponse{
		Choices: []core.ChatChoice{{
			Message: core.Message{
				Role:    "assistant",
				Content: content,
				// ToolCalls intentionally nil — the fallback parser must extract them.
			},
		}},
		Usage: core.ChatUsage{
			PromptTokens:     50,
			CompletionTokens: 30,
			TotalTokens:      80,
		},
	})
	return m
}

// Chat implements core.Provider.
func (m *MockProvider) Chat(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	m.mu.Lock()

	// Deep-copy the request for recording
	reqCopy := *req
	reqCopy.Messages = make([]core.Message, len(req.Messages))
	copy(reqCopy.Messages, req.Messages)
	m.Calls = append(m.Calls, &reqCopy)
	callNum := len(m.Calls)

	if m.respIndex >= len(m.responses) {
		m.mu.Unlock()
		return nil, fmt.Errorf("mock provider: no more responses configured (got %d calls)", m.respIndex+1)
	}

	err := m.errors[m.respIndex]
	resp := m.responses[m.respIndex]
	m.respIndex++

	// Capture blockUntil and clear it before releasing the lock
	block := m.blockUntil
	m.blockUntil = nil

	// Check if we should block on this specific call number
	var blockOnCh chan struct{}
	if m.blockOnCall > 0 && callNum == m.blockOnCall {
		blockOnCh = m.blockOnCh
	}
	m.mu.Unlock()

	// Block until the channel is closed or context is cancelled
	if block != nil || blockOnCh != nil {
		ch := block
		if ch == nil {
			ch = blockOnCh
		}
		// Check context first to avoid race with channel close
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		select {
		case <-ch:
			// Channel closed — proceed normally
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ChatStream implements core.Provider.
// When streaming is enabled (via WithStreaming), it delivers the configured
// response as chunked content via the StreamHandler. Otherwise, it calls
// OnDone with the response directly.
func (m *MockProvider) ChatStream(ctx context.Context, req *core.ChatRequest, handler core.StreamHandler) error {
	m.mu.Lock()

	// Record the call
	reqCopy := *req
	reqCopy.Messages = make([]core.Message, len(req.Messages))
	copy(reqCopy.Messages, req.Messages)
	m.Calls = append(m.Calls, &reqCopy)
	callNum := len(m.Calls)

	if m.respIndex >= len(m.responses) {
		m.mu.Unlock()
		return fmt.Errorf("mock provider: no more responses configured (got %d calls)", m.respIndex+1)
	}

	err := m.errors[m.respIndex]
	resp := m.responses[m.respIndex]
	m.respIndex++

	// Capture blockUntil
	block := m.blockUntil
	m.blockUntil = nil

	// Check if we should block on this specific call number
	var blockOnCh chan struct{}
	if m.blockOnCall > 0 && callNum == m.blockOnCall {
		blockOnCh = m.blockOnCh
	}

	// Capture streaming config
	useStreaming := m.streaming
	delay := m.streamDelay
	var chunks []string
	if m.streamChunkIdx < len(m.streamChunks) {
		chunks = m.streamChunks[m.streamChunkIdx]
	}
	m.streamChunkIdx++
	m.mu.Unlock()

	if err != nil {
		return err
	}

	// Block if needed
	if block != nil || blockOnCh != nil {
		ch := block
		if ch == nil {
			ch = blockOnCh
		}
		select {
		case <-ctx.Done():
			handler.OnError(ctx.Err())
			return ctx.Err()
		default:
		}
		select {
		case <-ch:
		case <-ctx.Done():
			handler.OnError(ctx.Err())
			return ctx.Err()
		}
	}

	// If streaming is enabled, deliver chunks then OnDone
	if useStreaming && len(chunks) > 0 {
		for _, chunk := range chunks {
			// Check context between chunks
			select {
			case <-ctx.Done():
				handler.OnError(ctx.Err())
				return ctx.Err()
			default:
			}
			handler.OnContent(chunk)
			if delay > 0 {
				time.Sleep(delay)
			}
		}
		handler.OnDone(resp)
		return nil
	}

	// Fallback: no streaming configured, just call OnDone with the response
	handler.OnDone(resp)
	return nil
}

// WithStreaming enables streaming mode. When true, ChatStream will deliver
// the response content as chunks via the StreamHandler instead of returning
// it all at once. Configure chunks with AddStreamChunks or use the content
// from AddTextResponse / AddToolCallResponse (split into word-sized chunks).
func (m *MockProvider) WithStreaming() *MockProvider {
	m.streaming = true
	return m
}

// WithStreamDelay sets the delay between streaming chunks.
func (m *MockProvider) WithStreamDelay(delay time.Duration) *MockProvider {
	m.streamDelay = delay
	return m
}

// AddStreamChunks configures explicit chunk sequences for streaming.
// Each call to ChatStream will use the next chunk sequence in order.
func (m *MockProvider) AddStreamChunks(chunks ...string) *MockProvider {
	m.streamChunks = append(m.streamChunks, chunks)
	return m
}

// Info implements core.Provider.
func (m *MockProvider) Info() core.ProviderInfo {
	return m.info
}

// EstimateTokens implements core.Provider.
func (m *MockProvider) EstimateTokens(_ *core.ChatRequest) int {
	return m.tokenCount
}

// Reset clears all recorded calls and resets the response index.
func (m *MockProvider) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
	m.respIndex = 0
	m.streamChunkIdx = 0
	m.streaming = false
	m.streamDelay = 0
	m.streamChunks = nil
}

// CallCount returns the number of Chat calls made.
func (m *MockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

// LastRequest returns the last Chat request, or nil.
func (m *MockProvider) LastRequest() *core.ChatRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Calls) == 0 {
		return nil
	}
	return m.Calls[len(m.Calls)-1]
}
