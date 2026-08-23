package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ExecuteTool executes a tool with standardized parameter validation and error handling
func ExecuteTool(ctx context.Context, toolName string, args map[string]interface{}, agent *Agent, rawArgsJSON string) ([]api.ImageData, string, error) {
	handler, found := tools.GetNewToolRegistry().Lookup(toolName)
	if !found {
		return nil, "", agenterrors.NewInvalidInputError("unknown tool '"+toolName+"'", nil)
	}

	if agent != nil && agent.debug {
		agent.debugLog("[tool] tool dispatched via new registry: %s\n", toolName)
	}

	// Computer-use tools are restricted to the computer_user persona.
	if isComputerUseToolBlocked(toolName, agent) {
		return nil, "", agenterrors.NewPermission("tool "+toolName+" is only available to the computer_user persona", map[string]any{"tool": toolName})
	}

	// Per-session opt-in: first computer-use action in a session requires explicit user consent.
	if agent != nil && computerUseToolNames[toolName] {
		if err := agent.checkComputerUseSessionOptIn(toolName); err != nil {
			return nil, "", err
		}
	}

	// Depth-based subagent nesting prevention.
	if agent != nil {
		// In LCM mode, block parallel subagents (causes file conflicts)
		// In full mode, allow if depth permits
		if toolName == "run_parallel_subagents" {
			if agent.contextProfile.Mode == configuration.ContextModeLowContext {
				return nil, "", agenterrors.NewSecurityError("parallel subagents not supported in low-context mode", nil)
			}
			if !agent.CanSpawnSubagents() {
				errMsg := fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
					"This restriction prevents runaway agent chains and ensures proper task delegation. "+
					"If you need additional work done, please complete your current task and return "+
					"your results to the parent agent for further delegation.",
					agent.SubagentDepth(), agent.MaxSubagentDepth())
				if agent != nil && agent.debug {
					agent.debugLog("[NO] Blocked subagent tool '%s' at depth %d (max: %d)\n", toolName, agent.SubagentDepth(), agent.MaxSubagentDepth())
				}
				return nil, "", agenterrors.NewSecurityError(errMsg, nil)
			}
		}
		// For run_subagent, respect depth limit in all modes
		if toolName == "run_subagent" && !agent.CanSpawnSubagents() {
			errMsg := fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
				"This restriction prevents runaway agent chains and ensures proper task delegation. "+
				"If you need additional work done, please complete your current task and return "+
				"your results to the parent agent for further delegation.",
				agent.SubagentDepth(), agent.MaxSubagentDepth())
			if agent.debug {
				agent.debugLog("[NO] Blocked subagent tool '%s' at depth %d (max: %d)\n", toolName, agent.SubagentDepth(), agent.MaxSubagentDepth())
			}
			return nil, "", agenterrors.NewSecurityError(errMsg, nil)
		}
	}

	// Security validation — classify and block/prompt dangerous operations.
	usedUnifiedGate := false
	if agent != nil {
		if cfg := agent.GetConfig(); cfg != nil && cfg.UnifiedRiskResolver {
			if err := agent.unifiedSecurityGate(toolName, args); err != nil {
				return nil, "", err
			}
			usedUnifiedGate = true
		}
	}
	if !usedUnifiedGate {
		var wsRoot string
		if agent != nil {
			wsRoot = agent.GetWorkspaceRoot()
		}
		secResult := tools.ClassifyToolCallWithWorkspace(toolName, args, wsRoot)
		if secResult.ShouldBlock || secResult.ShouldPrompt || secResult.IntentConfirmation {
			// Workflow-declared auto-approval for run_automate.
			workflowAutoApproved := false
			if agent != nil && toolName == "run_automate" && secResult.IntentConfirmation {
				if wf, ok := args["workflow"].(string); ok && wf != "" {
					if !workflowRequiresApproval(agent, wf) {
						if agent.debug {
							agent.debugLog("[UNLOCK] run_automate %q has requires_approval=false — skipping intent prompt\n", wf)
						}
						workflowAutoApproved = true
					}
				}
			}
			// In-session re-authorization for run_automate.
			alreadyApprovedInSession := false
			if !workflowAutoApproved && agent != nil && toolName == "run_automate" && secResult.IntentConfirmation {
				if wf, ok := args["workflow"].(string); ok && agent.IsWorkflowApprovedInSession(wf) {
					if agent.debug {
						agent.debugLog("[UNLOCK] run_automate %q already approved in this session — skipping intent prompt\n", wf)
					}
					alreadyApprovedInSession = true
				}
			}
			if workflowAutoApproved || alreadyApprovedInSession {
				// fall through to handler execution below
			} else if agent != nil {
				filePath, mode := extractFilePathAndMode(toolName, args)
				if agent.staticGateAutoApprove(secResult, filePath, "", mode) && !secResult.IntentConfirmation {
					// Unsafe mode, session elevation, or path-tier allow — skip the prompt for non-hard-block operations.
					if agent.debug {
						agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated/path-tier): bypassing security validation for %s (risk: %s)\n", toolName, secResult.Risk)
					}
				} else if agent.GetUnsafeShellMode() && toolName == "shell_command" && !secResult.IsHardBlock && secResult.Risk.String() != "DANGEROUS" && !secResult.IntentConfirmation {
					// --unsafe-shell bypasses CAUTION-tier shell prompts.
					if agent.debug {
						agent.debugLog("[UNLOCK] Unsafe shell mode: bypassing shell security prompt for %s (risk: %s)\n", toolName, secResult.Risk)
					}
				}
			} else if secResult.ShouldBlock || secResult.IntentConfirmation {
				// Defense-in-depth: no agent context available for approval,
				// so reject operations that require it.
				return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security: %s — %s (no agent context for approval)", toolName, secResult.Reasoning), nil)
			} else if agent != nil {
				// Check if we're running as a subagent — subagents cannot prompt
				isSubagent := agent.IsSubagent()

				approvedViaWebUI := false

				// Prefer webui approval path when a browser tab is connected.
				if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && !isSubagent && agent.HasActiveWebUIClients() {
					// Suspend the CLI spinner and pause the steer reader before blocking on the webui response.
					clihooks.SuspendIndicator()
					clihooks.PauseSteer()
					defer clihooks.ResumeSteer()

					if agent.debug {
						agent.debugLog("[APPROVAL] Requesting security approval via webui for %s (risk: %s)\n", toolName, secResult.Risk)
					}
					// Build extras with context the webui dialog needs (command, target, risk type)
					extras := map[string]string{}
					if secResult.IntentConfirmation {
						extras["intent_confirmation"] = "true"
					}
					if secResult.RiskType != "" {
						extras["risk_type"] = formatRiskType(secResult.RiskType)
					}
					switch toolName {
					case "shell_command":
						if cmd, ok := args["command"].(string); ok && cmd != "" {
							extras["command"] = cmd
						}
					case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
						if path, ok := args["path"].(string); ok && path != "" {
							extras["target"] = path
						}
					case "git":
						if op, ok := args["operation"].(string); ok && op != "" {
							extras["target"] = fmt.Sprintf("git %s", op)
						}
					case "run_automate":
						if wf, ok := args["workflow"].(string); ok && wf != "" {
							extras["target"] = fmt.Sprintf("workflow: %s", wf)
						}
					}
					approved, outcome := mgr.RequestToolApprovalWithOutcome(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, secResult.Risk.String(), secResult.Reasoning, extras)
					if outcome == security.ApprovalOutcomeResponded {
						if !approved {
							return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, secResult.Reasoning), nil)
						}
						if toolName == "run_automate" {
							if wf, ok := args["workflow"].(string); ok {
								agent.MarkWorkflowApprovedInSession(wf)
							}
						}
						approvedViaWebUI = true
					} else if agent.debug {
						// Timed out or the browser disconnected — don't treat an
						// unanswered dialog as a deny. Fall through to the CLI
						// prompt below so a user at the terminal can respond.
						agent.debugLog("[APPROVAL] webui approval unanswered (outcome=%d) for %s — falling back to CLI prompt\n", outcome, toolName)
					}
				}
				if !approvedViaWebUI {
					// CLI: prompt user interactively via terminal stdin
					agentConfig := agent.GetConfig()
					logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
					canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

					if canPrompt {
						var prompt string
						if secResult.IntentConfirmation {
							prompt = buildIntentConfirmationPrompt(toolName, args, secResult)
						} else {
							prompt = buildSecurityPrompt(toolName, args, secResult)
						}
						if !logger.AskForConfirmation(prompt, false, false) {
							return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security rejected: %s — %s. The user declined approval.", toolName, secResult.Reasoning), nil)
						}
						if toolName == "run_automate" {
							if wf, ok := args["workflow"].(string); ok {
								agent.MarkWorkflowApprovedInSession(wf)
							}
						}
					} else if secResult.ShouldBlock {
						// NON-INTERACTIVE + DANGEROUS, no approval mechanism: always block
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", toolName, secResult.Reasoning), nil)
					} else if secResult.IntentConfirmation {
						// NON-INTERACTIVE + intent confirmation required: must ask user first
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf("security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm.", toolName, secResult.Reasoning), nil)
					} else if secResult.ShouldPrompt && !isSubagent {
						// NON-INTERACTIVE + CAUTION, needs prompt but no approval mechanism:
						// Return a terminal SecurityError — the operation cannot proceed
						// without interactive approval. LLMs reliably honor "do not retry."
						return nil, "", agenterrors.NewSecurityError(fmt.Sprintf(
							"security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm. Do not retry this exact command without changing the risk profile.",
							toolName, secResult.Reasoning), nil)
					} // NON-INTERACTIVE + CAUTION, no approval mechanism, not a subagent: auto-allow (safe operations)
				}
			}
		}
	} // end if !usedUnifiedGate

	// Build ToolEnv from agent context
	var env tools.ToolEnv
	if agent != nil {
		env.EventBus = agent.GetEventBus()
		// Use effectiveCwd so tools honor cd commands during a session.
		env.WorkspaceRoot = agent.effectiveCwd()
		// Route tool output through the agent's output system in verbose mode.
		if cfg := agent.GetConfig(); cfg != nil && cfg.OutputVerbosity == configuration.OutputVerbosityVerbose {
			env.OutputWriter = newOutputRouter(agent)
		}
		env.MaxTokensFunc = func() int { return agent.GetMaxContextTokens() }
		env.ConfigManager = agent.GetConfigManager()
		env.AskUser = newAgentAskUserService(agent)
		env.TodoManager = agent.GetTodoManager()
		// Interactive CLI means: no browser client connected AND stdin is a TTY.
		env.IsInteractiveCLI = !agent.HasActiveWebUIClients() && !isNonInteractive()
		// Wire ApprovalManager adapter so migrated tools can request security approvals.
		env.ApprovalManager = newToolsApprovalAdapter(agent)
		// Wire new ToolEnv fields for vision, embedding, and subsystem interfaces.
		env.EmbeddingMgr = agent.GetEmbeddingManager()
		env.VisionProcessor = agent.GetVisionProcessor()
		env.WebBrowser = tools.NewBrowserAdapter()
		env.SkillLoader = newSkillLoaderAdapter(agent)
		env.SearchEngine = newSearchEngineAdapter(agent)
		// Pass the raw JSON args so handlers can recover key insertion order.
		env.RawArgsJSON = rawArgsJSON
		env.Notifier = agent
		env.LifetimeCtx = agent.LifetimeCtx()
		// Carry this agent's per-agent tool dispatch set so agent-dependent
		// tools (run_subagent, run_automate, ...) route to THIS agent even
		// when other agents exist in the process.
		env.ToolFuncs = agent.toolFuncs
		// Propagate subagent depth for memory gate and other subagent-specific behaviors.
		env.SubagentDepth = agent.subagentDepth
		// Propagate Gate 1's auto-approve decision so handler-level gates skip their interactive prompt.
		env.Gate1AutoApproved = agent.GetUnsafeMode() || agent.IsSessionElevated()
		// Wire Gate 1's path-tier classifier into ToolEnv so handlers can consult it up-front.
		env.FileAccessClassifier = agent
		// Interactive off-workspace approval: handlers consult this for
		// "prompt" verdicts instead of failing with the raw error.
		env.FileAccessPrompter = agent
	} else {
		env.OutputWriter = os.Stdout
		env.MaxTokensFunc = func() int { return 0 }
	}

	if err := handler.Validate(args); err != nil {
		return nil, "", agenterrors.Wrapf(err, "validation failed for tool %q", toolName)
	}

	// Memory gate for memory-intensive subagent shell commands.
	if toolName == "shell_command" && agent.subagentDepth > 0 {
		if cmd, ok := args["command"].(string); ok && IsMemoryIntensiveCommand(cmd) {
			gate := DefaultMemoryGate()
			if err := gate.Check(); err != nil {
				return nil, "", agenterrors.NewPermission(
					fmt.Sprintf("memory gate blocked shell_command: %v", err), nil)
			}
		}
	}

	res, err := handler.Execute(ctx, env, args)
	if err != nil {
		return nil, "", err
	}

	// Convert tools.ImageData [] → []api.ImageData
	var images []api.ImageData
	if len(res.Images) > 0 {
		images = make([]api.ImageData, len(res.Images))
		for i, img := range res.Images {
			images[i] = api.ImageData{
				URL:    img.URI,
				Base64: img.Base64,
				Type:   img.MIMEType,
			}
		}
	}

	output := res.Output
	if res.IsError {
		errMsg := output
		if errMsg == "" {
			errMsg = fmt.Sprintf("tool %q returned error state", toolName)
		}
		if agent != nil && agent.debug {
			agent.debugLog("[tool] tool dispatched via new registry (error): %s\n", toolName)
		}
		return images, "", agenterrors.NewTool(toolName, errMsg, nil)
	}

	// After successful tool execution, run embedding duplicate check for write tools.
	if output != "" {
		if shouldCheckDuplicates(toolName, agent) {
			if path, ok := args["path"].(string); ok && path != "" {
				note := runDuplicateCheck(ctx, agent, path)
				if note != "" {
					output = output + note
				}
				// Keep the index fresh — async so the agent response
				// isn't blocked on re-embedding.
				reindexFileAfterWrite(agent, path)
			}
		}
	}

	return images, output, nil
}

// staticGateAutoApprove reports whether a tool call should skip the interactive
// approval prompt because the session is in a bypass state (unsafe mode,
// session elevation, or path-tier allow).
func (a *Agent) staticGateAutoApprove(secResult tools.SecurityResult, filePath, resolvedPath, mode string) bool {
	if a == nil {
		return false
	}
	if a.GetUnsafeMode() {
		return true
	}
	if a.IsSessionElevated() && !secResult.IsHardBlock {
		return true
	}
	// Path-tier allow: consult the same classifier the filesystem gate adapter uses.
	if filePath != "" {
		switch a.classifyFileAccess(filePath, resolvedPath, mode) {
		case FileAccessAllow:
			return true
		case FileAccessDeny:
			return false
		case FileAccessPrompt:
			// fall through to default false
		}
	}
	return false
}

// unifiedSecurityGate is the unified security gate. When UnifiedRiskResolver is ON
// it replaces the split Gate 1 / Gate 2 call-site path with a single ResolveToolRisk assessment.
func (a *Agent) unifiedSecurityGate(name string, args map[string]interface{}) error {
	assessment := a.ResolveToolRisk(name, args)

	if a.debug {
		a.debugLog("[unified-gate] %s: %s\n", name, assessment.Explain())
	}

	// Hard blocks are unconditional — no approval path can override
	if assessment.IsHardBlock || assessment.Level == configuration.RiskLevelCritical {
		a.logSecurityDecision(name, args, assessment, "blocked")
		return agenterrors.NewSecurityErrorWithAssessment(
			fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", name, assessment.Reason), assessment.Explain(), nil,
		)
	}

	// High risk: reuse the existing approval cascade.
	if assessment.Level == configuration.RiskLevelHigh {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			if !a.highRiskApprovedForCommand(nil, cmd) {
				a.logSecurityDecision(name, args, assessment, "blocked")
				return agenterrors.NewSecurityErrorWithAssessment(
					fmt.Sprintf("security hard block: %s — %s. This operation cannot be approved by any profile or flag.", name, assessment.Reason), assessment.Explain(), nil,
				)
			}
		} else {
			// Non-shell tool at High risk — go through the interactive prompt
			return a.unifiedSecurityPrompt(name, args, assessment)
		}
	}

	// Workflow-declared auto-approval and in-session re-authorization for run_automate.
	if name == "run_automate" && assessment.RequiresIntentConfirmation {
		if wf, ok := args["workflow"].(string); ok && wf != "" {
			if !workflowRequiresApproval(a, wf) {
				if a.debug {
					a.debugLog("[UNLOCK] run_automate %q has requires_approval=false — skipping intent prompt\n", wf)
				}
				return nil
			}
			if a.IsWorkflowApprovedInSession(wf) {
				if a.debug {
					a.debugLog("[UNLOCK] run_automate %q already approved in this session — skipping intent prompt\n", wf)
				}
				return nil
			}
		}
	}

	// Intent confirmation is orthogonal to risk level — safe-but-consequential
	// ops still need explicit user intent
	if assessment.RequiresIntentConfirmation {
		if a.IsSubagent() {
			a.logSecurityDecision(name, args, assessment, "blocked")
			return agenterrors.NewSecurityErrorWithAssessment(
				fmt.Sprintf("security confirmation required: %s — %s. Re-run interactively, use --risk-profile=permissive, or use ask_user to confirm.", name, assessment.Reason), assessment.Explain(), nil,
			)
		}
		// For intent confirmation, go through the approval prompt
		return a.unifiedSecurityPrompt(name, args, assessment)
	}

	// Medium risk: needs interactive approval
	if assessment.Level == configuration.RiskLevelMedium {
		return a.unifiedSecurityPrompt(name, args, assessment)
	}

	// Low risk: allow. Skip audit logging here — Low-risk allows are noisy
	// (the vast majority of tool calls) and provide little audit value.
	return nil
}

// unifiedSecurityPrompt handles the interactive approval flow for Medium risk or intent-confirmation operations.
func (a *Agent) unifiedSecurityPrompt(name string, args map[string]interface{}, assessment RiskAssessment) error {
	_, err := a.RequestApproval(assessment, name, args)
	return err
}

// outputRouter implements io.Writer by routing writes through the agent's
// PrintLineAsync method. It buffers partial lines and flushes them on newline
// boundaries, so streaming output from tools appears in the console the same
// way it would on a real terminal.
type outputRouter struct {
	agent *Agent
	buf   bytes.Buffer
}

// newOutputRouter creates an io.Writer that routes tool output through the
// agent's output system instead of writing directly to os.Stdout.
func newOutputRouter(agent *Agent) io.Writer {
	return &outputRouter{agent: agent}
}

// Write implements io.Writer. It accumulates data in an internal buffer and
// flushes complete lines (terminated by \n) via PrintLineAsync. Any remaining
// buffered data is held until the next Write call brings a newline.
func (w *outputRouter) Write(p []byte) (int, error) {
	if w.agent == nil {
		// Fallback: write to os.Stdout if no agent is available
		return os.Stdout.Write(p)
	}
	w.buf.Write(p)
	for {
		idx := bytes.IndexByte(w.buf.Bytes(), '\n')
		if idx < 0 {
			break
		}
		line := w.buf.Next(idx + 1)
		// Trim the trailing newline for PrintLineAsync
		w.agent.PrintLineAsync(strings.TrimRight(string(line), "\n"))
	}
	return len(p), nil
}
