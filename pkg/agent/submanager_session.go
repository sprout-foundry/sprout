package agent

import (
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// SessionManager is composed of the session/scoped sub-interfaces.
// It owns all state that is scoped to a single conversation/session.
type SessionManager interface {
	MessageStore
	SessionStore
	CheckpointStore
	SummaryStore
	OptimizerStore
	ContextBudgetStore
	ConversationPrunerStore
	CommandHistoryStore
	PauseStore
	SessionConfigStore
	ConfigOverrideStore
	IterationStore
	SessionIntentStore
}

// AgentSessionManager implements SessionManager, holding all
// session-scoped state previously owned by AgentStateManager. Implements:
// MessageStore, SessionStore, CheckpointStore, SummaryStore,
// OptimizerStore, ContextBudgetStore, ConversationPrunerStore,
// CommandHistoryStore, PauseStore, SessionConfigStore,
// ConfigOverrideStore, IterationStore, SessionIntentStore
// (13 sub-interfaces).
//
// All methods are nil-safe: calling any getter/setter on a nil
// *AgentSessionManager returns the zero value without panicking. This
// preserves the legacy behavior of *AgentStateManager (which had a single
// underlying mu, so a nil receiver returned zero values from the methods
// defined on a nil struct literal in tests).
type AgentSessionManager struct {
	mu sync.RWMutex // General mutex for protecting most shared state

	// MessageStore
	messages          []api.Message
	messageTimestamps []time.Time

	// SessionStore
	sessionID string

	// CheckpointStore
	turnCheckpoints []TurnCheckpoint
	checkpointMu    sync.RWMutex

	// SummaryStore
	previousSummary string

	// OptimizerStore
	optimizer *ConversationOptimizer

	// ContextBudgetStore
	currentContextTokens int
	maxContextTokens     int
	contextWarningIssued bool

	// ConversationPrunerStore
	conversationPruner *ConversationPruner

	// CommandHistoryStore
	commandHistory []string
	historyIndex   int
	historyMu      sync.Mutex

	// PauseStore
	pauseState *PauseState
	pauseMutex sync.Mutex

	// SessionConfigStore
	sessionProvider api.ClientType
	sessionModel    string

	// ConfigOverrideStore
	configOverrides map[string]interface{}

	// IterationStore
	currentIteration int

	// SessionIntentStore
	sessionIntentEmbedding []float32
}

// NewAgentSessionManager creates a new AgentSessionManager with sensible defaults.
func NewAgentSessionManager(debug bool) *AgentSessionManager {
	return &AgentSessionManager{
		messages:           []api.Message{},
		optimizer:          NewConversationOptimizer(true, debug),
		conversationPruner: NewConversationPruner(debug),
		commandHistory:     []string{},
		historyIndex:       -1,
	}
}

// --- Messages ---

func (m *AgentSessionManager) GetMessages() []api.Message {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.messages
}

func (m *AgentSessionManager) SetMessages(msgs []api.Message) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = msgs
	// Rebuild timestamps to match the new message count. Existing
	// timestamps are preserved for messages that remain; new messages
	// (if the slice grew) get the current time.
	if len(m.messageTimestamps) < len(msgs) {
		now := time.Now()
		for i := len(m.messageTimestamps); i < len(msgs); i++ {
			m.messageTimestamps = append(m.messageTimestamps, now)
		}
	} else if len(m.messageTimestamps) > len(msgs) {
		m.messageTimestamps = m.messageTimestamps[:len(msgs)]
	}
}

// GetMessageTimestamps returns the creation timestamps for each message.
func (m *AgentSessionManager) GetMessageTimestamps() []time.Time {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.messageTimestamps
}

// SetMessageTimestamps sets the creation timestamps for each message.
func (m *AgentSessionManager) SetMessageTimestamps(ts []time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messageTimestamps = ts
}

func (m *AgentSessionManager) AddMessage(msg api.Message) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	m.messageTimestamps = append(m.messageTimestamps, time.Now())
}

// --- Session ---

// Session ID accessors hold m.mu — autoSaveState (state.go) writes
// sessionID from the main flow while RecordTurnCheckpointAsync
// (turn_checkpoints.go) reads it from a background goroutine. The
// race detector caught this on TestRunSeamlessPlanning_ContextCancelled.

func (m *AgentSessionManager) GetSessionID() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionID
}

func (m *AgentSessionManager) SetSessionID(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionID = id
}

// --- Turn checkpoints ---

func (m *AgentSessionManager) GetTurnCheckpoints() []TurnCheckpoint {
	if m == nil {
		return nil
	}
	return m.turnCheckpoints
}

func (m *AgentSessionManager) SetTurnCheckpoints(cps []TurnCheckpoint) {
	if m == nil {
		return
	}
	m.turnCheckpoints = cps
}

func (m *AgentSessionManager) AddTurnCheckpoint(cp TurnCheckpoint) {
	if m == nil {
		return
	}
	m.turnCheckpoints = append(m.turnCheckpoints, cp)
}

// --- Checkpoint mutex ---

func (m *AgentSessionManager) GetCheckpointMutex() *sync.RWMutex {
	if m == nil {
		return nil
	}
	return &m.checkpointMu
}

// --- Summary ---

func (m *AgentSessionManager) GetPreviousSummary() string {
	if m == nil {
		return ""
	}
	return m.previousSummary
}

func (m *AgentSessionManager) SetPreviousSummary(summary string) {
	if m == nil {
		return
	}
	m.previousSummary = summary
}

// --- Optimizer ---

func (m *AgentSessionManager) GetOptimizer() *ConversationOptimizer {
	if m == nil {
		return nil
	}
	return m.optimizer
}

func (m *AgentSessionManager) SetOptimizer(o *ConversationOptimizer) {
	if m == nil {
		return
	}
	m.optimizer = o
}

// --- Context tokens ---

func (m *AgentSessionManager) GetCurrentContextTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentContextTokens
}

func (m *AgentSessionManager) SetCurrentContextTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentContextTokens = n
}

func (m *AgentSessionManager) GetMaxContextTokens() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxContextTokens
}

func (m *AgentSessionManager) SetMaxContextTokens(n int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxContextTokens = n
}

// --- Context warning ---

func (m *AgentSessionManager) IsContextWarningIssued() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contextWarningIssued
}

func (m *AgentSessionManager) SetContextWarningIssued(v bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.contextWarningIssued = v
}

// --- Conversation pruner ---

func (m *AgentSessionManager) GetConversationPruner() *ConversationPruner {
	if m == nil {
		return nil
	}
	return m.conversationPruner
}

func (m *AgentSessionManager) SetConversationPruner(pruner *ConversationPruner) {
	if m == nil {
		return
	}
	m.conversationPruner = pruner
}

// --- Command history ---

func (m *AgentSessionManager) GetCommandHistory() []string {
	if m == nil {
		return nil
	}
	return m.commandHistory
}

func (m *AgentSessionManager) SetCommandHistory(h []string) {
	if m == nil {
		return
	}
	m.commandHistory = h
}

func (m *AgentSessionManager) GetHistoryIndex() int {
	if m == nil {
		return 0
	}
	return m.historyIndex
}

func (m *AgentSessionManager) SetHistoryIndex(i int) {
	if m == nil {
		return
	}
	m.historyIndex = i
}

func (m *AgentSessionManager) GetHistoryMutex() *sync.Mutex {
	if m == nil {
		return nil
	}
	return &m.historyMu
}

// --- Pause ---

func (m *AgentSessionManager) GetPauseState() *PauseState {
	if m == nil {
		return nil
	}
	return m.pauseState
}

func (m *AgentSessionManager) SetPauseState(ps *PauseState) {
	if m == nil {
		return
	}
	m.pauseState = ps
}

func (m *AgentSessionManager) GetPauseMutex() *sync.Mutex {
	if m == nil {
		return nil
	}
	return &m.pauseMutex
}

// --- Session config ---

func (m *AgentSessionManager) GetSessionProvider() api.ClientType {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionProvider
}

func (m *AgentSessionManager) SetSessionProvider(ct api.ClientType) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionProvider = ct
}

func (m *AgentSessionManager) GetSessionModel() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionModel
}

func (m *AgentSessionManager) SetSessionModel(model string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionModel = model
}

// --- Config overrides ---

func (m *AgentSessionManager) GetConfigOverrides() map[string]interface{} {
	if m == nil {
		return nil
	}
	return m.configOverrides
}

func (m *AgentSessionManager) SetConfigOverrides(overrides map[string]interface{}) {
	if m == nil {
		return
	}
	m.configOverrides = overrides
}

// --- Current iteration ---

func (m *AgentSessionManager) GetCurrentIteration() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentIteration
}

func (m *AgentSessionManager) SetCurrentIteration(iter int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentIteration = iter
}

// --- Session intent embedding ---

func (m *AgentSessionManager) GetSessionIntentEmbedding() []float32 {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.sessionIntentEmbedding == nil {
		return nil
	}
	// Return a defensive copy
	result := make([]float32, len(m.sessionIntentEmbedding))
	copy(result, m.sessionIntentEmbedding)
	return result
}

func (m *AgentSessionManager) SetSessionIntentEmbedding(emb []float32) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if emb == nil || len(emb) == 0 {
		m.sessionIntentEmbedding = nil
		return
	}
	// Store a defensive copy
	m.sessionIntentEmbedding = make([]float32, len(emb))
	copy(m.sessionIntentEmbedding, emb)
}

func (m *AgentSessionManager) SetSessionIntentEmbeddingIfNil(emb []float32) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionIntentEmbedding != nil {
		return false
	}
	if emb == nil || len(emb) == 0 {
		return false
	}
	m.sessionIntentEmbedding = make([]float32, len(emb))
	copy(m.sessionIntentEmbedding, emb)
	return true
}
