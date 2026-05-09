package agent

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/validation"
)

const (
	inputInjectionBufferSize = 10
	asyncOutputBufferSize    = 256
)

// PauseState tracks the state when a task is paused for clarification
type PauseState struct {
	IsPaused       bool          `json:"is_paused"`
	PausedAt       time.Time     `json:"paused_at"`
	OriginalTask   string        `json:"original_task"`
	Clarifications []string      `json:"clarifications"`
	MessagesBefore []api.Message `json:"messages_before"`
}

// initSubManagers ensures all sub-managers are initialized.
// This is called lazily to support tests that create bare Agent structs
// with &Agent{} and then call methods that depend on sub-managers.
func (a *Agent) initSubManagers() {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}
	if a.output == nil {
		a.output = NewAgentOutputManager()
	}
	if a.security == nil {
		a.security = NewAgentSecurityManager()
	}
	if a.mcpSub == nil {
		a.mcpSub = NewAgentMCPManager()
	}
}

type Agent struct {
	// Core LLM coordination
	client           api.ClientInterface
	clientType       api.ClientType
	systemPrompt     string
	baseSystemPrompt string // Base prompt restored when persona is cleared
	maxIterations    int

	// Configuration
	configManager *configuration.Manager
	workspaceRoot string
	debug         bool

	// Input handling
	inputInjectionChan  chan string
	inputInjectionMutex sync.Mutex
	interruptCtx        context.Context
	interruptCancel     context.CancelFunc

	// Sub-managers — Agent coordinates through these interfaces
	state    StateManager    // Conversation history, checkpoints, tokens, cost, persona, etc.
	output   OutputManager   // Streaming, async output, event metadata, output routing
	security SecurityManager // Approvals, redaction, elevation, bypass
	mcpSub   MCPSubManager   // MCP server lifecycle and tool caching

	// Event system (bridges output and core)
	eventBus  *events.EventBus
	validator *validation.Validator

	// Tool execution support
	shellCommandHistory   map[string]*ShellCommandResult
	shellCommandHistoryMu sync.RWMutex
	changeTracker         *ChangeTracker
	preparedTools         sync.RWMutex
	lastToolNames         []string

	// UI integration
	ui UI

	// Stats callback (protected by atomic access)
	statsUpdateCallback atomic.Value // func(int, float64)

	// Debug logging
	debugLogFile  *os.File
	debugLogPath  string
	debugLogMutex sync.Mutex
	logger        *AgentLogger

	// Trace session for dataset collection
	traceSession interface{}

	// TerminalManager provides access to hidden PTY sessions for WebUI mode.
	// When nil (CLI mode), shell commands use os/exec unchanged.
	terminalManager tools.TerminalAccess

	// Embedding index manager for duplicate detection on file writes.
	embeddingMgr *embedding.EmbeddingManager
}
