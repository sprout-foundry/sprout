package agent

import (
	"context"
	"fmt"
	"strings"

	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// defaultSubagentMaxIterations caps the number of LLM iterations a subagent
// may run. 0 (unlimited) let a stuck subagent loop 164+ times burning tokens
// before the user noticed. 50 is generous for real coding tasks but stops
// runaway loops within a few minutes. Can be overridden per-call via
// SubagentOptions.MaxIterations once that field exists.
const defaultSubagentMaxIterations = 50

// createSubagent creates a new in-process agent for subagent execution.
// parentCtx is used as the base for the subagent's interrupt context so
// that cancellation of the parent's run context (Ctrl+C, timeout, etc.)
// propagates into the subagent's in-flight LLM calls — without this the
// subagent's HTTP requests ignore cancellation and the goroutine leaks.
func (r *SubagentRunner) createSubagent(opts SubagentOptions, parentCtx context.Context) (*Agent, error) {
	if r.shared == nil || r.shared.ConfigManager == nil {
		return nil, fmt.Errorf("shared state and config manager are required")
	}

	// Resolve provider/model: use opts overrides, then parent agent, then config defaults
	provider := opts.Provider
	model := opts.Model

	if provider == "" && r.parentAgent != nil {
		parentProvider := r.parentAgent.GetProvider()
		if parentProvider != "" && parentProvider != "unknown" {
			provider = parentProvider
		}
	}
	if model == "" && r.parentAgent != nil {
		parentModel := r.parentAgent.GetModel()
		if parentModel != "" && parentModel != "unknown" {
			model = parentModel
		}
	}

	// Resolve client type from config
	clientType, finalModel, err := r.shared.ConfigManager.ResolveProviderModel(provider, model)
	if err != nil {
		return nil, fmt.Errorf("resolve provider/model: %w", err)
	}

	// Create client via factory (or test hook for testing)
	var client agent_api.ClientInterface
	if r.testClientFactory != nil {
		client, err = r.testClientFactory(clientType, finalModel)
	} else {
		client, err = factory.CreateProviderClient(clientType, finalModel)
	}
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	// Build system prompt
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		// Use a minimal default for subagents
		systemPrompt = "You are a helpful coding assistant that can execute tools to complete tasks."
	}

	// Determine effective workspace root
	effectiveWorkspaceRoot := r.shared.WorkspaceRoot
	if opts.WorkingDir != "" {
		effectiveWorkspaceRoot = opts.WorkingDir
	}

	// Create interrupt context derived from the parent's context so
	// cancellation (Ctrl+C, timeout, runCtx cancel) propagates into the
	// subagent's LLM calls. Previously this used context.Background(),
	// making the subagent un-cancellable — the in-flight HTTP request
	// ignored the parent's interrupt and the goroutine leaked.
	interruptCtx, interruptCancel := context.WithCancel(parentCtx)

	// Create sub-managers
	stateMgr := NewAgentStateManager(false)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// Construct the agent struct
	agent := &Agent{
		client:              client,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       defaultSubagentMaxIterations, // bounded to prevent runaway loops
		clientType:          clientType,
		debug:               r.parentAgent != nil && r.parentAgent.debug,
		configManager:       r.shared.ConfigManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        interruptCtx,
		interruptCancel:     interruptCancel,
		parentInterruptCtx:  parentCtx, // preserve parent link across resetInterruptForNewQuery/ClearInterrupt
		workspaceRoot:       effectiveWorkspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		// Shared resources
		todoMgr:        r.shared.TodoManager,
		eventBus:       r.shared.EventBus,
		embeddingMgr:   r.shared.EmbeddingMgr,
	}

	// Share the parent's clarificationManager so subagents can call
	// request_clarification through the same manager instance.
	if r.parentAgent != nil && r.parentAgent.clarificationManager != nil {
		agent.clarificationManager = r.parentAgent.clarificationManager
	}

	// SP-059 Phase 2c: enable a lightweight change tracker on the subagent
	// so the returned envelope can include a structured FilesModified
	// manifest. Tracking just records writes in memory; it does not
	// participate in the parent's revision/commit flow unless the parent
	// also has tracking enabled (handled elsewhere). Cheap to keep always
	// on — the cost is one entry per write.
	agent.EnableChangeTracking("subagent run")

	// Inherit the parent's TerminalManager. Without this, subagents (and
	// recursively their own subagents) try to call shell_command with
	// background=true / check_background / stop_background and fail with
	// "background mode requires WebUI terminal manager" even though the
	// root agent has a TerminalManager attached. The TerminalManager is
	// process-scoped (one per WebUI server); chat IDs route work to the
	// right session pool, so direct inheritance by reference is correct.
	if r.parentAgent != nil {
		if tm := r.parentAgent.GetTerminalManager(); tm != nil {
			agent.terminalManager = tm
		}
	}

	// Set subagentDepth based on parent's depth + 1.
	// This enables configurable nesting: EA (0) → orchestrator (1) → coder/tester (2).
	agent.subagentDepth = r.parentAgent.subagentDepth + 1

	// Propagate rootPersonaID from parent so depth limits can vary by root persona.
	if r.parentAgent.rootPersonaID != "" {
		agent.rootPersonaID = r.parentAgent.rootPersonaID
	}

	// SP-058: propagate the active risk-profile override so the user's
	// session-level --risk-profile (or per-step workflow override)
	// continues to apply inside subagents. Without this the subagent
	// would fall back to the config-level setting and a user who set
	// --risk-profile=readonly would find subagents running under the
	// config default instead — silently bypassing their intent. The
	// readonly profile's DefaultRisk=Critical still blocks subagent
	// writes (Critical is checked before the IsSubagent auto-approve),
	// so this propagation is what makes readonly actually readonly
	// during delegation.
	agent.riskProfileOverride = r.parentAgent.riskProfileOverride

	// Propagate session folder allowlist into the subagent so paths
	// the user already approved at the root level don't re-prompt
	// inside delegated work. The snapshot is a copy — the subagent
	// can add its own entries without leaking back to the parent
	// (intentional: subagent-acquired approvals shouldn't outlive
	// the delegation).
	for _, f := range r.parentAgent.SnapshotSessionAllowedFolders() {
		agent.AddSessionAllowedFolder(f)
	}

	// SP-051: tag every event this subagent publishes with depth + persona
	// so the CLI tool-timeline can indent and color-badge by who's running.
	// Merge (not replace) so parent-set chat/client/user routing keys still
	// flow through subagent events to the right WebUI client.
	parentMeta := r.parentAgent.output.GetEventMetadata()
	merged := make(map[string]interface{}, len(parentMeta)+3)
	for k, v := range parentMeta {
		merged[k] = v
	}
	merged["subagent_depth"] = agent.subagentDepth
	if persona := strings.TrimSpace(opts.Persona); persona != "" {
		merged["active_persona"] = persona
	}
	agent.SetEventMetadata(merged)

	return agent, nil
}
