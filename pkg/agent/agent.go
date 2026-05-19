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
	"github.com/sprout-foundry/sprout/pkg/security"
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

	// Conversation timing
	conversationStartTime time.Time

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
	todoMgr  *tools.TodoManager // Per-agent todo manager for session isolation

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

	// SubagentRunner manages in-process subagent execution.
	subagentRunner *SubagentRunner

	// subagentDepth tracks the nesting depth of this agent.
	// 0 = primary agent (EA), 1 = orchestrator, 2 = coder/tester, etc.
	// Used to control tool availability and prevent excessive nesting.
	subagentDepth int

	// driftDetected flag (atomic) for CLI drift notification.
	// Set to 1 when drift is detected in the async check, cleared when
	// the CLI reads and handles the notification.
	driftDetected int32

	// driftCheckDone is closed when the async drift check completes.
	// Used by the CLI to wait for the check before showing a prompt.
	//
	// Concurrency invariant: written in finalizeConversationPostHooks
	// (called synchronously during ProcessQuery), then read by the CLI
	// interactive loop after ProcessQuery returns. The write always
	// happens-before the read. Do NOT read from a different goroutine.
	driftCheckDone <-chan struct{}
}

// InjectWebUIManagers replaces the agent's internal approval and ask-user
// managers with the webui-owned instances.  This is called after the web
// server is constructed so that security prompts and ask_user requests
// created by the agent are routed through the same manager that the webui
// handlers resolve responses on — eliminating the need for global singletons.
func (a *Agent) InjectWebUIManagers(approvalMgr *security.ApprovalManager, askUserMgr *tools.AskUserManager) {
	a.security.SetApprovalMgr(approvalMgr)
	a.security.SetAskUserMgr(askUserMgr)
}

// SetDriftDetected marks that drift was detected in the async check.
func (a *Agent) SetDriftDetected() {
	atomic.StoreInt32(&a.driftDetected, 1)
}

// GetAndClearDriftDetected returns true if drift was detected and clears the flag.
func (a *Agent) GetAndClearDriftDetected() bool {
	return atomic.SwapInt32(&a.driftDetected, 0) == 1
}

// WaitForDriftCheck waits for the async drift check to complete, up to the
// given timeout. Returns true if the check completed (or was nil), false if
// the timeout expired.
func (a *Agent) WaitForDriftCheck(timeout time.Duration) bool {
	if a.driftCheckDone == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-a.driftCheckDone:
		return true
	case <-timer.C:
		return false
	}
}
