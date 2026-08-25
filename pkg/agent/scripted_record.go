// Scripted playback client for testing

package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// Compile-time check that ScriptedClient implements api.ClientInterface
var _ api.ClientInterface = (*ScriptedClient)(nil)

// ScriptedClient is an enhanced mock client for comprehensive E2E testing
// It supports:
// - Sequential scripted responses with tool calls
// - Streaming simulation
// - Error injection
// - Vision support
// - Rate limit simulation
type ScriptedClient struct {
	*factory.TestClient

	// Scripted responses in order
	responses []*ScriptedResponse

	// Current index in the responses slice
	index int

	// Mutex for thread-safe access
	mu sync.Mutex

	// Rate limit simulation state
	rateLimitCounter   int
	rateLimitExceeded  bool
	rateLimitThreshold int

	// Vision support flag
	supportsVision bool

	// Vision model name
	visionModel string

	// Debug mode (atomic for lock-free access)
	debug atomic.Bool

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// TPS simulation
	lastTPS    float64
	averageTPS float64

	// Response history for debugging and testing
	responseHistory []*ScriptedResponse

	// Sent requests recording for testing
	sentRequests [][]api.Message
}

// NewScriptedClient creates a new scripted client with optional initial responses
func NewScriptedClient(responses ...*ScriptedResponse) *ScriptedClient {
	ctx, cancel := context.WithCancel(context.Background())

	client := &ScriptedClient{
		TestClient:      &factory.TestClient{},
		responses:       responses,
		index:           0,
		ctx:             ctx,
		cancel:          cancel,
		lastTPS:         100.0,
		averageTPS:      100.0,
		supportsVision:  false,
		responseHistory: make([]*ScriptedResponse, 0),
		sentRequests:    make([][]api.Message, 0),
	}

	return client
}

// NewScriptedClientWithVision creates a scripted client that supports vision models
func NewScriptedClientWithVision(model string, responses ...*ScriptedResponse) *ScriptedClient {
	ctx, cancel := context.WithCancel(context.Background())

	client := &ScriptedClient{
		TestClient:      &factory.TestClient{},
		responses:       responses,
		index:           0,
		ctx:             ctx,
		cancel:          cancel,
		lastTPS:         100.0,
		averageTPS:      100.0,
		supportsVision:  true,
		visionModel:     model,
		responseHistory: make([]*ScriptedResponse, 0),
		sentRequests:    make([][]api.Message, 0),
	}

	return client
}

// AddResponse appends a response to the end of the queue
func (c *ScriptedClient) AddResponse(response *ScriptedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = append(c.responses, response)
}

// SetResponses replaces all responses and resets all derived state.
func (c *ScriptedClient) SetResponses(responses []*ScriptedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responses = responses
	c.index = 0
	c.rateLimitCounter = 0
	c.rateLimitExceeded = false
	c.rateLimitThreshold = 0
	c.sentRequests = make([][]api.Message, 0)
}

// Reset resets the response index to the beginning
func (c *ScriptedClient) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.index = 0
	c.rateLimitCounter = 0
	c.rateLimitExceeded = false
	c.rateLimitThreshold = 0
	c.sentRequests = make([][]api.Message, 0)
}

// GetNextResponse returns the next response without advancing the index
func (c *ScriptedClient) GetNextResponse() *ScriptedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responses == nil || c.index >= len(c.responses) {
		return nil
	}
	return c.responses[c.index]
}

// AdvanceIndex advances to the next response
func (c *ScriptedClient) AdvanceIndex() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responses != nil && c.index < len(c.responses) {
		c.index++
	}
}

// GetIndex returns the current response index
func (c *ScriptedClient) GetIndex() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.index
}

// SetIndex sets the response index (useful for replay scenarios)
func (c *ScriptedClient) SetIndex(idx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if idx < 0 {
		idx = 0
	}
	if c.responses == nil || idx > len(c.responses) {
		idx = len(c.responses)
	}
	c.index = idx
}

// Length returns the number of scripted responses
func (c *ScriptedClient) Length() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responses == nil {
		return 0
	}
	return len(c.responses)
}

// LastResponse returns the last consumed response
func (c *ScriptedClient) LastResponse() *ScriptedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.responseHistory) == 0 {
		return nil
	}
	return c.responseHistory[len(c.responseHistory)-1]
}

// ResponseHistory returns all consumed responses
func (c *ScriptedClient) ResponseHistory() []*ScriptedResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Return a copy to prevent external modification
	history := make([]*ScriptedResponse, len(c.responseHistory))
	copy(history, c.responseHistory)
	return history
}

// ClearHistory clears the response history
func (c *ScriptedClient) ClearHistory() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.responseHistory = make([]*ScriptedResponse, 0)
}

// GetSentRequests returns a defensive deep copy of all recorded request message arrays.
// Both the outer slice and each inner []api.Message slice are copied to prevent
// external mutation of the client's internal state.
func (c *ScriptedClient) GetSentRequests() [][]api.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([][]api.Message, len(c.sentRequests))
	for i, msgs := range c.sentRequests {
		result[i] = append([]api.Message(nil), msgs...)
	}
	return result
}

// GetSentRequest returns a specific request's messages (nil if out of range)
func (c *ScriptedClient) GetSentRequest(index int) []api.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.sentRequests) {
		return nil
	}
	return append([]api.Message(nil), c.sentRequests[index]...)
}

// ClearSentRequests clears all recorded sent requests
func (c *ScriptedClient) ClearSentRequests() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sentRequests = make([][]api.Message, 0)
}

// debugLog logs a message if debug mode is enabled
func (c *ScriptedClient) debugLog(format string, args ...interface{}) {
	if c.debug.Load() {
		// In a real implementation, you might want to use a proper logger
		fmt.Printf("[DEBUG] %s\n", fmt.Sprintf(format, args...))
	}
}

// advanceIndex advances past the current response and records it in history.
// This must be called while NOT holding c.mu.
func (c *ScriptedClient) advanceIndex(resp *ScriptedResponse) {
	c.mu.Lock()
	if c.responses != nil && c.index < len(c.responses) {
		c.index++
	}
	if resp != nil {
		c.responseHistory = append(c.responseHistory, resp)
	}
	c.mu.Unlock()
}

// resolveUsage extracts usage metrics from a response, falling back to defaults.
func resolveUsage(resp *ScriptedResponse) ScriptedTokenUsage {
	if resp != nil && (resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 || resp.Usage.TotalTokens > 0) {
		return resp.Usage
	}
	return ScriptedTokenUsage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		EstimatedCost:    0.0,
	}
}

// Cancel cancels any pending operations
func (c *ScriptedClient) Cancel() {
	c.cancel()
}

// Close closes the client and releases resources
func (c *ScriptedClient) Close() {
	c.Cancel()
}
