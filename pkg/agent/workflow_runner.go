// workflow_runner.go — the in-process TODO-loop workflow entry point.
// Agent construction (which needs unexported Agent fields) stays here; the
// loop itself, config parsing, and all loop helpers live in the
// pkg/agent/workflow subpackage (SP-141 phase 1).

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agentworkflow "github.com/sprout-foundry/sprout/pkg/agent/workflow"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// WorkflowResult is the in-process workflow result, now defined in the
// workflow subpackage. Kept as an alias so existing callers (tool handler,
// completion-message builders, tests) are untouched.
type WorkflowResult = agentworkflow.Result

// generateWorkflowSessionID generates a workflow session ID (forwarder to
// the workflow subpackage).
func generateWorkflowSessionID() string {
	return agentworkflow.NewSessionID()
}

// RunWorkflowLoopInProcess creates a fresh agent and runs the TODO loop
// workflow in the calling goroutine (blocking). For non-blocking use,
// call it from a goroutine.
//
// The fresh agent is created using the same pattern as subagents:
// new client from factory, new state managers, proper interrupt context,
// full tool wiring via the seed tool registry, and budget tracking.
//
// configPath is the path to the workflow JSON file. The file is parsed for
// the "loop" section; if no loop section is found, an error is returned.
func RunWorkflowLoopInProcess(ctx context.Context, parentAgent *Agent, configPath string, eventBus *events.EventBus) (*WorkflowResult, error) {
	if parentAgent == nil {
		return nil, agenterrors.NewValidation("parentAgent is required", nil)
	}
	if parentAgent.configManager == nil {
		return nil, agenterrors.NewConfig("parent config manager is required", nil)
	}

	// Parse the workflow config file.
	cfg, err := agentworkflow.ParseFile(configPath)
	if err != nil {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("failed to parse workflow config %q", configPath), err)
	}

	if cfg.Loop == nil {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("workflow %q has no 'loop' section", configPath), nil)
	}

	loop := cfg.Loop
	loop.ApplyDefaults()

	// Read the gate prompt file.
	gatePromptBytes, err := os.ReadFile(filepath.Clean(loop.GatePromptFile))
	if err != nil {
		return nil, agenterrors.NewAgent("workflow_runner", fmt.Sprintf("failed to read gate_prompt_file %q", loop.GatePromptFile), err)
	}
	gatePromptText := strings.TrimSpace(string(gatePromptBytes))
	if gatePromptText == "" {
		return nil, agenterrors.NewInvalidInputError(fmt.Sprintf("gate_prompt_file %q is empty", loop.GatePromptFile), nil)
	}

	// Derive provider/model from the parent agent.
	provider := parentAgent.GetProvider()
	model := parentAgent.GetModel()

	// Resolve client type from config.
	clientType, finalModel, err := parentAgent.configManager.ResolveProviderModel(provider, model)
	if err != nil {
		return nil, agenterrors.Wrap(err, "resolve provider/model for workflow agent")
	}

	// Create client via factory.
	client, err := factory.CreateProviderClient(clientType, finalModel)
	if err != nil {
		return nil, agenterrors.Wrap(err, "create client for workflow agent")
	}

	// Build system prompt.
	systemPrompt := appendSubagentPreamble("You are a helpful coding assistant executing a TODO-based workflow.")

	// Determine effective workspace root.
	effectiveWorkspaceRoot := parentAgent.workspaceRoot
	if effectiveWorkspaceRoot == "" {
		effectiveWorkspaceRoot, _ = os.Getwd()
	}

	// Create interrupt context derived from the caller's context so
	// cancellation propagates into the workflow agent's LLM calls.
	interruptCtx, interruptCancel := context.WithCancel(ctx)

	// Create fresh sub-managers for isolation from the parent agent.
	stateMgr := NewAgentStateManager(false)
	outputMgr := NewAgentOutputManager()
	securityMgr := NewAgentSecurityManager()
	mcpMgr := NewAgentMCPManager()

	// Construct the fresh agent struct — mirrors createSubagent() from
	// subagent_creation.go.
	workflowAgent := &Agent{
		client:              client,
		clientType:          clientType,
		systemPrompt:        systemPrompt,
		baseSystemPrompt:    systemPrompt,
		maxIterations:       loop.MaxIterations,
		configManager:       parentAgent.configManager,
		shellCommandHistory: make(map[string]*ShellCommandResult),
		inputInjectionChan:  make(chan string, inputInjectionBufferSize),
		interruptCtx:        interruptCtx,
		interruptCancel:     interruptCancel,
		parentInterruptCtx:  ctx,
		workspaceRoot:       effectiveWorkspaceRoot,
		state:               stateMgr,
		output:              outputMgr,
		security:            securityMgr,
		mcpSub:              mcpMgr,
		todoMgr:             tools.NewTodoManager(),
		eventBus:            eventBus,
		shellCwd:            &shellCwdTracker{},
		subagentDepth:       parentAgent.subagentDepth + 1,
		rootPersonaID:       parentAgent.rootPersonaID,
	}

	// Propagate risk profile override from the parent so that a
	// --risk-profile=readonly applies inside the loop agent too.
	if parentAgent.riskProfileOverride != "" {
		workflowAgent.riskProfileOverride = parentAgent.riskProfileOverride
	}

	// Inherit the parent's TerminalManager so shell_command with
	// background=true / check_background works inside the loop.
	if tm := parentAgent.GetTerminalManager(); tm != nil {
		workflowAgent.SetTerminalManager(tm)
	}

	// Share the parent's clarificationManager so the workflow agent
	// can call request_clarification through the same instance.
	if parentAgent.clarificationManager != nil {
		workflowAgent.clarificationManager = parentAgent.clarificationManager
	}

	// Re-resolve the context profile from the workflow agent's OWN client and
	// config instead of leaving it as a zero-value (full mode). A workflow agent
	// running under a smaller-context model than its parent should get LCM
	// auto-activated. (SP-125 R4)
	if err := workflowAgent.resolveAndApplyContextProfile(); err != nil {
		interruptCancel()
		return nil, agenterrors.Wrap(err, "resolve context profile for workflow agent")
	}

	// Enable lightweight change tracking.
	workflowAgent.EnableChangeTracking("workflow loop")

	// Wire the event bus for publishing events.
	if eventBus != nil {
		workflowAgent.SetEventBus(eventBus)
	}

	// Set event metadata so events carry routing keys for the WebUI.
	workflowAgent.SetEventMetadata(map[string]interface{}{
		"subagent_depth": workflowAgent.subagentDepth,
		"active_persona": "workflow-loop",
	})

	// -----------------------------------------------------------------------
	// Budget setup
	// -----------------------------------------------------------------------
	var budget *FleetUsdBudget
	heartbeatSeconds := 600
	if cfg.Budget != nil && cfg.Budget.USD > 0 {
		warnAt := cfg.Budget.WarnAt
		if len(warnAt) == 0 {
			warnAt = []float64{0.50, 0.80}
		}
		budget = NewFleetUsdBudget(cfg.Budget.USD, warnAt)
		workflowAgent.SetFleetUsdBudget(budget)

		workflowAgent.SetBudgetWarningCallback(func(threshold, spent, limit float64) {
			fmt.Fprintf(os.Stderr, "\nWARNING — crossed %.0f%% threshold: $%.2f of $%.2f spent\n",
				threshold*100, spent, limit)
		})
		workflowAgent.SetBudgetExceededCallback(func(spent, limit float64) {
			fmt.Fprintf(os.Stderr, "\nCAP HIT — $%.2f of $%.2f spent; stopping.\n", spent, limit)
		})

		if cfg.Progress != nil && cfg.Progress.HeartbeatSeconds > 0 {
			heartbeatSeconds = cfg.Progress.HeartbeatSeconds
		}
	}

	// TODO file path — resolve relative to the workflow config file's directory.
	todoDir := filepath.Dir(configPath)
	todoFile := filepath.Join(todoDir, loop.TodoFile)

	// -----------------------------------------------------------------------
	// Run the TODO loop (in the workflow subpackage)
	// -----------------------------------------------------------------------
	return agentworkflow.RunLoop(ctx, workflowAgent, budget, workflowAgent, loop, gatePromptText, todoFile, time.Duration(heartbeatSeconds)*time.Second)
}
