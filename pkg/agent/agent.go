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
	if a.asyncDelegateTracker == nil {
		a.asyncDelegateTracker = NewAsyncDelegateTracker()
	}
	if a.clarificationManager == nil && a.eventBus != nil {
		a.clarificationManager = NewClarificationManager(a.eventBus)
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

	// Shell CWD tracking — updated by cd commands in handleShellCommand.
	// Tools like commit use this instead of workspaceRoot when available,
	// so that git operations run in the correct directory after the agent
	// has changed directories via shell commands.
	shellCwd     string
	prevShellCwd string
	shellCwdMu   sync.RWMutex

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

	// rootPersonaID tracks the persona of the top-level (depth 0) agent in the spawn chain.
	// Propagated to subagents so that depth limits and spawn restrictions can be enforced
	// based on the root persona (e.g., EA gets 3 levels, orchestrator gets 2).
	rootPersonaID string

	// delegateDepth tracks how deep this agent is in the delegate nesting chain.
	// 0 = top-level agent, 1 = first-level delegate, etc.
	delegateDepth int

	// allowedTools restricts which tools this agent may use.
	// When non-nil, only tools whose names (lowercased) are keys in this map
	// can be invoked. Used by the delegate tool to limit tool access for child agents.
	allowedTools map[string]bool

	// clarificationManager handles clarification requests between delegate and parent agents.
	clarificationManager *ClarificationManager

	// delegateID is the ID of this agent when acting as a delegate (empty for root agents).
	delegateID string

	// filesReadThisTurn tracks paths the agent called read_file on during
	// the current turn. Used by the SP-046 staleness rule in
	// checkWriteStaleness — see workspace_sync.go. Reset at turn boundaries
	// via ResetFileReadsForNewTurn.
	filesReadThisTurn *turnFileTracker

	// fileMetadata holds per-path WorkspaceFileMetadata populated by the
	// platform-side sync layer. checkWriteStaleness consults it to refuse
	// writes over files with pending unsynced browser edits.
	fileMetadata *workspaceMetadataStore

	fileReadsMu sync.Mutex

	// Fleet budget tracking (set by SubagentRunner for parallel subagents).
	// When non-nil/nonzero, each LLM call debits tokens to the shared fleet
	// budget tracker. If the budget is exceeded mid-run, fleetBudgetTrunc
	// is set and the conversation loop truncates gracefully.
	fleetBudgetTracker *atomic.Int64
	fleetBudgetLimit   int64
	fleetBudgetTrunc   atomic.Bool

	// asyncDelegateTracker manages the lifecycle of asynchronous delegates.
	// Lazy-initialized in initSubManagers.
	asyncDelegateTracker *AsyncDelegateTracker
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

// SetFleetBudget enables per-LLM-call fleet budget tracking for this agent.
// When tracker is non-nil and limit > 0, each LLM call will debit its token
// usage to the shared tracker. If the budget is exceeded, fleetBudgetTrunc
// is set and the conversation loop will truncate gracefully.
func (a *Agent) SetFleetBudget(tracker *atomic.Int64, limit int64) {
	a.fleetBudgetTracker = tracker
	a.fleetBudgetLimit = limit
	a.fleetBudgetTrunc.Store(false)
}

// FleetBudgetExceeded reports whether the fleet budget was exceeded during
// this agent's execution (mid-run truncation).
func (a *Agent) FleetBudgetExceeded() bool {
	return a.fleetBudgetTrunc.Load()
}
