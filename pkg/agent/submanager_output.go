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
}

// AgentOutputManager implements OutputManager.
type AgentOutputManager struct {
	outputMutex       *sync.Mutex
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
}

// NewAgentOutputManager creates a new AgentOutputManager with default values.
func NewAgentOutputManager() *AgentOutputManager {
	return &AgentOutputManager{
		eventMetadata: make(map[string]interface{}),
	}
}

func (m *AgentOutputManager) SetStreamingEnabled(enabled bool) {
	m.streamingEnabled = enabled
}

func (m *AgentOutputManager) IsStreamingEnabled() bool {
	return m.streamingEnabled
}

func (m *AgentOutputManager) SetStreamingCallback(cb func(string)) {
	m.streamingCallback = cb
}

func (m *AgentOutputManager) GetStreamingCallback() func(string) {
	return m.streamingCallback
}

func (m *AgentOutputManager) SetReasoningCallback(cb func(string)) {
	m.reasoningCallback = cb
}

func (m *AgentOutputManager) GetReasoningCallback() func(string) {
	return m.reasoningCallback
}

func (m *AgentOutputManager) SetFlushCallback(cb func()) {
	m.flushCallback = cb
}

func (m *AgentOutputManager) GetFlushCallback() func() {
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
	return m.outputRouter
}

func (m *AgentOutputManager) SetOutputRouter(router *OutputRouter) {
	m.outputRouter = router
}

func (m *AgentOutputManager) GetAsyncOutput() chan string {
	return m.asyncOutput
}

func (m *AgentOutputManager) SetAsyncOutput(ch chan string) {
	m.asyncOutput = ch
}

func (m *AgentOutputManager) EnsureAsyncOutputWorker(fn func()) {
	m.asyncOutputOnce.Do(fn)
}

func (m *AgentOutputManager) GetAsyncBufferSize() int {
	return m.asyncBufferSize
}

func (m *AgentOutputManager) SetAsyncBufferSize(size int) {
	m.asyncBufferSize = size
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
