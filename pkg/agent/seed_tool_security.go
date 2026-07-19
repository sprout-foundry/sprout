// Package agent: pre-execute hook, security caution wrapping, and loop
// detection for the seed tool registry. (split from seed_tool_registry.go)
package agent

import (
	"fmt"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ---------------------------------------------------------------------------
// Pre-execute hook: security classification + subagent nesting prevention
// ---------------------------------------------------------------------------

// wrapSecurityCaution surfaces security errors with the same
// SECURITY_CAUTION_REQUIRED signal that handleToolError produces for
// handler-level security errors. Seed wraps pre-execute hook errors as
// "Pre-execute hook rejected: <msg>", so prefixing the message here
// ensures the model sees "SECURITY_CAUTION_REQUIRED:" in the tool result.
// Non-security errors are returned unchanged.
//
// Integrated features:
//   - Task 1: security circuit breaker. The SAME tool+args combo blocked
//     >= securityBlockThreshold times escalates from a standard caution to a
//     hard "SECURITY_CAUTION_LOOP_DETECTED" signal.
//   - Task 2: audit logging of the block / loop_detected decision.
//   - Task 3: telemetry counters (cautions issued, retries, loops).
//   - Task 4: tier-aware guidance suffix parsed from the error message.
func wrapSecurityCaution(agent *Agent, err error) error {
	if err == nil {
		return nil
	}
	if !agenterrors.IsSecurity(err) {
		return err
	}
	safeMsg := sanitizeToolFailureMessage(err.Error())

	// Task 1: record the block and detect loops.
	// We can't recover the original toolName/args here (the hook owns those),
	// so loop detection happens at the hook level via recordSecurityBlock.
	// This function is called with the error already built; the hook calls
	// recordSecurityBlock BEFORE invoking wrapSecurityCaution when it has the
	// tool name + args. For callers that don't go through the hook (e.g.
	// direct error wrapping), loop detection is skipped — that's acceptable
	// because the only live caller is the pre-execute hook.

	// Task 4: tier-aware guidance suffix.
	suffix := tierFromMessage(safeMsg)

	// Task 3: telemetry — a caution was issued.
	if agent != nil {
		agent.incrementSecurityCautionsIssued()
	}

	if agent != nil {
		agent.PublishAgentMessage("security_caution", safeMsg, nil)
	}
	// Preserve the SecurityError type so ClassifyError still returns
	// ActionEscalate downstream. Seed's pre-execute hook path wraps the
	// error string as "Pre-execute hook rejected: <msg>", so the
	// SECURITY_CAUTION_REQUIRED prefix survives into the tool result.
	return agenterrors.NewSecurityError(
		fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s. %s", safeMsg, suffix),
		err,
	)
}

// wrapSecurityCautionWithLoop is the loop-detection-aware variant of
// wrapSecurityCaution. It is called by the pre-execute hook which has access
// to the tool name + args needed for circuit-breaker tracking. It:
//   - increments the security block counter (Task 1)
//   - escalates to SECURITY_CAUTION_LOOP_DETECTED when count >= threshold
//   - logs the audit entry (Task 2)
//   - bumps telemetry counters (Task 3)
//   - applies tier-aware suffixes (Task 4)
//
// Returns the wrapped error (loop variant when threshold is hit).
func wrapSecurityCautionWithLoop(agent *Agent, err error, toolName string, args map[string]interface{}) error {
	if err == nil {
		return nil
	}
	if !agenterrors.IsSecurity(err) {
		return err
	}

	// Task 1: record this block and check for a loop.
	newCount := 0
	if agent != nil {
		newCount = agent.recordSecurityBlock(toolName, args)
	}

	safeMsg := sanitizeToolFailureMessage(err.Error())

	// Task 3: telemetry.
	if agent != nil {
		agent.incrementSecurityCautionsIssued()
		// A retry-after-caution is when the count goes from 1→2 (the model
		// saw the caution at count 1 and retried the identical operation).
		if newCount == 2 {
			agent.incrementSecurityRetryAfterCaution()
		}
	}

	// Task 1: loop escalation. The threshold (2) means "one retry is
	// forgivable" — count 1 is the first block, count 2 is the first retry,
	// and count 3+ (> threshold) is a loop.
	if newCount > securityBlockThreshold {
		if agent != nil {
			agent.incrementSecurityLoopsDetected()
		}
		loopMsg := fmt.Sprintf(
			"SECURITY_CAUTION_LOOP_DETECTED: This exact operation has been blocked %d times. "+
				"The security decision will not change on retry. "+
				"Stop attempting this operation and choose a different approach. Last reason: %s",
			newCount, safeMsg)
		if agent != nil {
			agent.PublishAgentMessage("security_loop", safeMsg, nil)
			// Task 2: audit-log the loop detection.
			assessment := RiskAssessment{
				Level:   configuration.RiskLevelCritical,
				Sources: []RiskSource{RiskSourceClassifier},
				Reason:  fmt.Sprintf("security loop detected after %d identical blocks: %s", newCount, safeMsg),
			}
			agent.logSecurityDecision(toolName, args, assessment, "loop_detected")
		}
		return agenterrors.NewSecurityError(loopMsg, err)
	}

	// Standard caution path (count < threshold).
	suffix := tierFromMessage(safeMsg)
	if agent != nil {
		agent.PublishAgentMessage("security_caution", safeMsg, nil)
		// Task 2: audit-log the block.
		assessment := RiskAssessment{
			Sources: []RiskSource{RiskSourceClassifier},
			Reason:  safeMsg,
		}
		agent.logSecurityDecision(toolName, args, assessment, "blocked")
	}
	return agenterrors.NewSecurityError(
		fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s. %s", safeMsg, suffix),
		err,
	)
}

func newPreExecuteHook(agent *Agent) func(name string, args map[string]interface{}) error {
	if agent == nil {
		return nil
	}
	return func(name string, args map[string]interface{}) error {
		// 1. Depth-based subagent nesting prevention
		// Agents at or beyond the maximum nesting depth cannot spawn further subagents.
		// This prevents runaway agent chains while allowing configurable multi-level nesting.
		// ask_user is allowed for subagents because they share the event bus with the
		// primary agent and questions are routed through the same WebUI/CLI prompt mechanism.
		if !agent.CanSpawnSubagents() {
			if name == "run_subagent" || name == "run_parallel_subagents" {
				return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(
					fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
						"This restriction prevents runaway agent chains and ensures proper task delegation. "+
						"If you need additional work done, please complete your current task and return "+
						"your results to the parent agent for further delegation.",
						agent.SubagentDepth(), agent.MaxSubagentDepth()), nil), name, args)
			}
		}

		// 2. Security classification
		secResult := tools.ClassifyToolCall(name, args)
		unifiedAssessment := agent.ResolveToolRisk(name, args)
		if agent.debug {
			// SP-068 unified risk view, logged alongside the live gate so the
			// canonical assessment (classifier ⊕ persona cascade) is visible
			// for "why was this gated?" without changing the decision below.
			agent.debugLog("[risk] %s: %s\n", name, unifiedAssessment.Explain())
		}

		// SP-068 Phase 2: when UnifiedRiskResolver is enabled, use the
		// single ResolveToolRisk assessment for gating. When disabled
		// (default), the existing dual-gate path runs unchanged with
		// optional shadow-mode logging.
		if cfg := agent.GetConfig(); cfg != nil && cfg.UnifiedRiskResolver {
			gateErr := agent.unifiedSecurityGate(name, args)
			if gateErr != nil {
				return wrapSecurityCautionWithLoop(agent, gateErr, name, args)
			}
			return nil
		}

		// — Shadow-mode logging (flag OFF, debug ON) —
		// Compare the old dual-gate decision with the new unified resolver
		// to validate behavioral parity before the flag flips default.
		if agent.debug {
			oldDecision := resolveOldDecision(secResult)
			newDecision := resolveUnifiedDecision(unifiedAssessment)
			agent.debugLog("[shadow-risk] %s: old=%s, new=%s, match=%v\n",
				name, oldDecision, newDecision, oldDecision == newDecision)
		}

		if !secResult.ShouldBlock && !secResult.ShouldPrompt {
			return nil // safe, no action needed
		}

		// Unsafe mode or session elevation skips the interactive prompt
		// for non-hard-block operations. Shared policy with
		// ToolRegistry.ExecuteTool via staticGateAutoApprove, so clicking
		// "Elevate (session)" actually suppresses subsequent static-classifier
		// prompts on the live seed path (not just the filesystem gate).
		if agent.staticGateAutoApprove(secResult) {
			if agent.debug {
				agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated): bypassing security validation for %s (risk: %s)\n", name, secResult.Risk)
			}
			return nil
		}

		isSubagent := agent.IsSubagent()

		// Persistent allowlist: shell commands the user previously chose
		// "Always approve" for short-circuit BEFORE any prompt UI fires.
		// Critical-tier ops are evaluated separately in tool_handlers_shell.go
		// and cannot be allowlisted, so this is safe.
		if name == "shell_command" {
			if cmd, ok := args["command"].(string); ok && cmd != "" && agent.IsShellCommandAllowlisted(cmd) {
				return nil
			}
		}

		// WebUI approval path — interactive only.
		// Non-interactive runs never wait on a browser dialog: even if a
		// stale WebUI tab is connected, there's no guarantee a human is
		// watching. Fast-fail to the permissive non-interactive path below.
		hasInteractiveSurface := !agent.isNonInteractive() && !isSubagent && agent.HasActiveWebUIClients()
		if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && hasInteractiveSurface {
			if agent.debug {
				agent.debugLog("[APPROVAL] Requesting security approval via webui for %s (risk: %s)\n", name, secResult.Risk)
			}
			extras := map[string]string{}
			if secResult.RiskType != "" {
				extras["risk_type"] = formatRiskType(secResult.RiskType)
			}
			var shellCommand string
			switch name {
			case "shell_command":
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					extras["command"] = cmd
					shellCommand = cmd
					// Signal the frontend that this prompt supports the
					// 4-option dialog (Approve / Deny / Always / Elevate).
					extras["allow_options"] = "true"
				}
			case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
				if path, ok := args["path"].(string); ok && path != "" {
					extras["target"] = path
				}
			case "git":
				if op, ok := args["operation"].(string); ok && op != "" {
					extras["target"] = fmt.Sprintf("git %s", op)
				}
			}
			if name == "shell_command" && shellCommand != "" {
				decision := mgr.RequestToolApprovalDecision(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), name, secResult.Risk.String(), secResult.Reasoning, extras)
				if !decision.Approved() {
					return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil), name, args)
				}
				agent.applyApprovalDecision(decision, shellCommand)
				return nil
			}
			if !mgr.RequestToolApproval(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), name, secResult.Risk.String(), secResult.Reasoning, extras) {
				return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil), name, args)
			}
			return nil
		}

		// CLI approval path
		agentConfig := agent.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
		canPrompt := logger != nil && logger.IsInteractive() && !isSubagent

		if canPrompt {
			// shell_command gets the 4-option dialog so the user can
			// allowlist or elevate inline. Other tools stay on the
			// classic yes/no path until the dialog is generalized.
			if name == "shell_command" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					prompt := buildShellApprovalPrompt(secResult)
					// SP-124 Phase 3: this is the legacy seed-side CLI
					// fallback; the unified broker path (approval_broker.go)
					// runs the analyzer and renders the panel. We pass nil
					// here so the picker renders identically to pre-Phase-3 —
					// the analyzer runs at the broker layer, not this seed.
					choice := logger.AskForApprovalWithOptions(prompt, cmd, nil)
					decision := approvalDecisionFromCLIChoice(choice)
					if !decision.Approved() {
						return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil), name, args)
					}
					agent.applyApprovalDecision(decision, cmd)
					return nil
				}
			}
			prompt := buildSecurityPrompt(name, args, secResult)
			if !logger.AskForConfirmation(prompt, false, false) {
				return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(fmt.Sprintf("user rejected %s — %s", name, secResult.Reasoning), nil), name, args)
			}
			return nil
		}

		// Non-interactive path.
		//
		// Per the security model (see SECURITY_NONINTERACTIVE.md):
		// non-interactive runs are permissive-by-default. The assumption is
		// that automation runs inside a container/sandbox, so routine
		// approval prompts (Medium/High) are auto-approved to avoid
		// dead-ending a run that has no human to ask.
		//
		// Only unconditional hard blocks (Critical: rm -rf /, fork bombs,
		// mass source destruction) terminate the run — and they do so with
		// a fatal error so the loop exits cleanly rather than spinning.
		if secResult.IsHardBlock {
			return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(
				fmt.Sprintf("fatal security block (non-interactive): %s — %s. "+
					"This operation is unconditionally blocked and cannot be approved by any profile or flag. "+
					"The run will exit.",
					name, secResult.Reasoning), nil), name, args)
		}

		// Non-hard-block in non-interactive mode: allow (permissive-by-default).
		// Log so the auto-approval is auditable without blocking.
		if agent.debug && (secResult.ShouldBlock || secResult.ShouldPrompt) {
			agent.debugLog("[non-interactive] auto-approving %s (risk: %s) — no interactive surface available\n",
				name, secResult.Risk)
		}

		return nil
	}
}
