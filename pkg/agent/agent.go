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
	interruptMu         sync.Mutex // protects interruptCtx + interruptCancel
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

	// BackgroundProcessManager provides background shell execution for CLI mode.
	// When nil AND terminalManager is nil, background shell features are unavailable.
	// Lazy-initialized on first use when terminalManager is nil.
	backgroundProcessManager *tools.BackgroundProcessManager

	// automateApprovedMu guards automateApprovedWorkflows.
	automateApprovedMu sync.Mutex
	// automateApprovedWorkflows tracks workflow filenames the user has
	// explicitly approved during this chat session. Subsequent run_automate
	// calls for the same workflow (e.g. retry-after-failure kicked off by
	// the primary agent) skip the intent-confirmation prompt — the user has
	// already opted in once.
	automateApprovedWorkflows map[string]struct{}

	// Embedding index manager for duplicate detection on file writes.
	embeddingMu  sync.RWMutex // protects embeddingMgr
	embeddingMgr *embedding.EmbeddingManager

	// backgroundWg tracks background goroutines that use embeddingMgr or other
	// resources. Shutdown() waits for these before closing resources.
	backgroundWg sync.WaitGroup

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

	// allowedTools restricts which tools this agent may use.
	// When non-nil, only tools whose names (lowercased) are keys in this map
	// can be invoked. Used to limit tool access for subagents.
	allowedTools map[string]bool

	// clarificationManager handles clarification requests from a subagent
	// back to the parent / user. Shared by reference from the parent
	// agent when this agent is spawned as a subagent; nil on root agents
	// that haven't been wired by their event bus initializer.
	clarificationManager *ClarificationManager

	// subagentID is this agent's identifier when acting as a subagent;
	// empty for root agents. Used as the requester ID when calling
	// request_clarification so the response routes back to the right
	// subagent.
	subagentID string

	// riskProfileOverride is a transient (per-session) override for
	// the risk cascade profile. Set by the --risk-profile CLI flag
	// and by per-step risk_profile in workflow JSON. Empty means
	// fall through to Config.RiskProfile, then to "default".
	// See agent_getters.go:EvaluateOperationRisk for resolution.
	riskProfileOverride configuration.RiskProfile

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

	// Fleet USD budget — parallels fleetBudget but in dollars. Set by the
	// workflow runner from cfg.Budget and propagated to subagents. When
	// non-nil and limit > 0, each LLM response debits its cost to the
	// shared budget. Crossing warn thresholds emits stdout/event notices;
	// hitting the cap sets the same fleetBudgetTrunc flag the token path
	// uses so the conversation loop stops gracefully in one place.
	fleetUsdBudget *FleetUsdBudget

	// budgetWarningCallback is invoked when the USD budget first crosses
	// a configured warning threshold. The function value is stored
	// atomically so the workflow runner can replace it without locking.
	budgetWarningCallback atomic.Value // func(threshold, spent, limit float64)
	// budgetExceededCallback is invoked when the USD budget is first
	// reached or surpassed. Same atomic-value pattern.
	budgetExceededCallback atomic.Value // func(spent, limit float64)

	// SP-066 Phase 2: background rollup worker. rollupW is lazily
	// initialized via rollupOnce so existing tests that construct bare
	// *Agent values continue to work without a constructor change.
	rollupOnce sync.Once
	rollupW    *rollupWorker
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
// this agent's execution (mid-run truncation). Returns true if EITHER the
// token budget or the USD budget tripped — both use the same truncation
// flag because the downstream behavior is identical.
func (a *Agent) FleetBudgetExceeded() bool {
	return a.fleetBudgetTrunc.Load()
}

// SetFleetUsdBudget attaches a shared USD budget to this agent. The budget
// is shared by reference, so all agents (primary + subagents) that hold
// the same pointer debit to the same counter.
func (a *Agent) SetFleetUsdBudget(b *FleetUsdBudget) {
	a.fleetUsdBudget = b
	a.fleetBudgetTrunc.Store(false)
}

// GetFleetUsdBudget returns the agent's USD budget, or nil if none is set.
// Used by the SubagentRunner to propagate the same budget to spawned
// subagents (so the cap is workflow-wide, not per-agent).
func (a *Agent) GetFleetUsdBudget() *FleetUsdBudget {
	return a.fleetUsdBudget
}

// SetBudgetWarningCallback registers a function invoked when the USD budget
// first crosses each configured warning threshold (fired at most once per
// threshold). Pass nil to unregister.
func (a *Agent) SetBudgetWarningCallback(fn func(threshold, spent, limit float64)) {
	if fn == nil {
		a.budgetWarningCallback.Store((func(threshold, spent, limit float64))(nil))
		return
	}
	a.budgetWarningCallback.Store(fn)
}

// SetBudgetExceededCallback registers a function invoked when the USD budget
// is first reached or surpassed. Pass nil to unregister.
func (a *Agent) SetBudgetExceededCallback(fn func(spent, limit float64)) {
	if fn == nil {
		a.budgetExceededCallback.Store((func(spent, limit float64))(nil))
		return
	}
	a.budgetExceededCallback.Store(fn)
}
