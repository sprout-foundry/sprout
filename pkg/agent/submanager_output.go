package agent

import (
	"strings"
	"sync"
)

// OutputManager manages all output and streaming-related state for an Agent.
type OutputManager interface {
	SetStreamingEnabled(enabled bool)
	IsStreamingEnabled() bool
	SetStreamingCallback(cb func(string))
	GetStreamingCallback() func(string)
	SetReasoningCallback(cb func(string))
	GetReasoningCallback() func(string)
	SetFlushCallback(cb func())
	GetFlushCallback() func()
	SetOutputMutex(mu *sync.Mutex)
	GetOutputMutex() *sync.Mutex
	GetStreamingBuffer() *strings.Builder
	GetReasoningBuffer() *strings.Builder
	GetOutputRouter() *OutputRouter
	SetOutputRouter(router *OutputRouter)
	GetAsyncOutput() chan string
	SetAsyncOutput(ch chan string)
	EnsureAsyncOutputWorker(fn func())
	GetAsyncBufferSize() int
	SetAsyncBufferSize(size int)
	GetEventMetadata() map[string]interface{}
	SetEventMetadata(meta map[string]interface{})
	SetEventMetadataUnlocked(meta map[string]interface{})
	GetEventMetadataMutex() *sync.RWMutex
	SetTerminalWriter(fn func(string))
	GetTerminalWriter() func(string)
}

// AgentOutputManager implements OutputManager.
type AgentOutputManager struct {
	outputMutex       *sync.Mutex
	mu                sync.RWMutex // guards the streaming/state fields below
	streamingEnabled  bool
	streamingCallback func(string)
	reasoningCallback func(string)
	streamingBuffer   strings.Builder
	reasoningBuffer   strings.Builder
	flushCallback     func()
	asyncOutput       chan string
	asyncOutputOnce   sync.Once
	asyncBufferSize   int
	outputRouter      *OutputRouter
	eventMetadataMu   sync.RWMutex
	eventMetadata     map[string]interface{}
	terminalWriter    func(string)
}

// NewAgentOutputManager creates a new AgentOutputManager with default values.
func NewAgentOutputManager() *AgentOutputManager {
	return &AgentOutputManager{
		eventMetadata: make(map[string]interface{}),
	}
}

func (m *AgentOutputManager) SetStreamingEnabled(enabled bool) {
	m.mu.Lock()
	m.streamingEnabled = enabled
	m.mu.Unlock()
}

func (m *AgentOutputManager) IsStreamingEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streamingEnabled
}

func (m *AgentOutputManager) SetStreamingCallback(cb func(string)) {
	m.mu.Lock()
	m.streamingCallback = cb
	m.mu.Unlock()
}

func (m *AgentOutputManager) GetStreamingCallback() func(string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streamingCallback
}

func (m *AgentOutputManager) SetReasoningCallback(cb func(string)) {
	m.mu.Lock()
	m.reasoningCallback = cb
	m.mu.Unlock()
}

func (m *AgentOutputManager) GetReasoningCallback() func(string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.reasoningCallback
}

func (m *AgentOutputManager) SetFlushCallback(cb func()) {
	m.mu.Lock()
	m.flushCallback = cb
	m.mu.Unlock()
}

func (m *AgentOutputManager) GetFlushCallback() func() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.flushCallback
}

func (m *AgentOutputManager) SetOutputMutex(mu *sync.Mutex) {
	m.outputMutex = mu
}

func (m *AgentOutputManager) GetOutputMutex() *sync.Mutex {
	return m.outputMutex
}

func (m *AgentOutputManager) GetStreamingBuffer() *strings.Builder {
	return &m.streamingBuffer
}

func (m *AgentOutputManager) GetReasoningBuffer() *strings.Builder {
	return &m.reasoningBuffer
}

func (m *AgentOutputManager) GetOutputRouter() *OutputRouter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outputRouter
}

func (m *AgentOutputManager) SetOutputRouter(router *OutputRouter) {
	m.mu.Lock()
	m.outputRouter = router
	m.mu.Unlock()
}

func (m *AgentOutputManager) GetAsyncOutput() chan string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.asyncOutput
}

func (m *AgentOutputManager) SetAsyncOutput(ch chan string) {
	m.mu.Lock()
	m.asyncOutput = ch
	m.mu.Unlock()
}

func (m *AgentOutputManager) EnsureAsyncOutputWorker(fn func()) {
	m.asyncOutputOnce.Do(fn)
}

func (m *AgentOutputManager) GetAsyncBufferSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.asyncBufferSize
}

func (m *AgentOutputManager) SetAsyncBufferSize(size int) {
	m.mu.Lock()
	m.asyncBufferSize = size
	m.mu.Unlock()
}

func (m *AgentOutputManager) GetEventMetadata() map[string]interface{} {
	m.eventMetadataMu.RLock()
	defer m.eventMetadataMu.RUnlock()
	return m.eventMetadata
}

func (m *AgentOutputManager) SetEventMetadata(meta map[string]interface{}) {
	m.eventMetadataMu.Lock()
	defer m.eventMetadataMu.Unlock()
	m.eventMetadata = meta
}

// SetEventMetadataUnlocked sets metadata without acquiring the mutex.
// Caller must hold m.eventMetadataMu.
func (m *AgentOutputManager) SetEventMetadataUnlocked(meta map[string]interface{}) {
	m.eventMetadata = meta
}

func (m *AgentOutputManager) GetEventMetadataMutex() *sync.RWMutex {
	return &m.eventMetadataMu
}

func (m *AgentOutputManager) SetTerminalWriter(fn func(string)) {
	m.mu.Lock()
	m.terminalWriter = fn
	m.mu.Unlock()
}

func (m *AgentOutputManager) GetTerminalWriter() func(string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.terminalWriter
}
