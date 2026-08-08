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

// wrapSecurityCaution surfaces security errors with the SECURITY_CAUTION_REQUIRED signal.
// Non-security errors are returned unchanged. Applies tier-aware guidance suffixes.
func wrapSecurityCaution(agent *Agent, err error) error {
	if err == nil {
		return nil
	}
	if !agenterrors.IsSecurity(err) {
		return err
	}
	safeMsg := sanitizeToolFailureMessage(err.Error())

	suffix := tierFromMessage(safeMsg)

	if agent != nil {
		agent.incrementSecurityCautionsIssued()
	}

	if agent != nil {
		agent.PublishAgentMessage("security_caution", safeMsg, nil)
	}
	return agenterrors.NewSecurityError(
		fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s. %s", safeMsg, suffix),
		err,
	)
}

// wrapSecurityCautionWithLoop is the loop-detection-aware variant of wrapSecurityCaution.
// Increments the security block counter and escalates to SECURITY_CAUTION_LOOP_DETECTED when threshold is hit.
func wrapSecurityCautionWithLoop(agent *Agent, err error, toolName string, args map[string]interface{}) error {
	if err == nil {
		return nil
	}
	if !agenterrors.IsSecurity(err) {
		return err
	}

	newCount := 0
	if agent != nil {
		newCount = agent.recordSecurityBlock(toolName, args)
	}

	safeMsg := sanitizeToolFailureMessage(err.Error())

	if agent != nil {
		agent.incrementSecurityCautionsIssued()
		if newCount == 2 {
			agent.incrementSecurityRetryAfterCaution()
		}
	}

	// Loop escalation: threshold (2) means one retry is forgivable.
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
		if agent != nil {
			if name == "run_parallel_subagents" {
				if agent.contextProfile.Mode == configuration.ContextModeLowContext {
					return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(
						"parallel subagents not supported in low-context mode", nil), name, args)
				}
				if !agent.CanSpawnSubagents() {
					return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(
						fmt.Sprintf("SUBAGENT_RESTRICTION: Agent at depth %d cannot spawn subagents (max depth: %d). "+
							"This restriction prevents runaway agent chains and ensures proper task delegation. "+
							"If you need additional work done, please complete your current task and return "+
							"your results to the parent agent for further delegation.",
							agent.SubagentDepth(), agent.MaxSubagentDepth()), nil), name, args)
				}
			}
			if name == "run_subagent" && !agent.CanSpawnSubagents() {
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
			agent.debugLog("[risk] %s: %s\n", name, unifiedAssessment.Explain())
		}

		// When UnifiedRiskResolver is enabled, use the single ResolveToolRisk assessment.
		if cfg := agent.GetConfig(); cfg != nil && cfg.UnifiedRiskResolver {
			gateErr := agent.unifiedSecurityGate(name, args)
			if gateErr != nil {
				return wrapSecurityCautionWithLoop(agent, gateErr, name, args)
			}
			return nil
		}

		// Shadow-mode logging (flag OFF, debug ON)
		if agent.debug {
			oldDecision := resolveOldDecision(secResult)
			newDecision := resolveUnifiedDecision(unifiedAssessment)
			agent.debugLog("[shadow-risk] %s: old=%s, new=%s, match=%v\n",
				name, oldDecision, newDecision, oldDecision == newDecision)
		}

		if !secResult.ShouldBlock && !secResult.ShouldPrompt {
			return nil
		}

		// Unsafe mode or session elevation skips the interactive prompt.
		filePath, mode := extractFilePathAndMode(name, args)
		if agent.staticGateAutoApprove(secResult, filePath, "", mode) {
			if agent.debug {
				agent.debugLog("[UNLOCK] Static gate auto-approve (unsafe/elevated): bypassing security validation for %s (risk: %s)\n", name, secResult.Risk)
			}
			return nil
		}

		isSubagent := agent.IsSubagent()

		// Persistent allowlist: shell commands the user previously chose "Always approve" for.
		if name == "shell_command" {
			if cmd, ok := args["command"].(string); ok && cmd != "" && agent.IsShellCommandAllowlisted(cmd) {
				return nil
			}
		}

		// WebUI approval path.
		hasInteractiveSurface := !isSubagent && agent.HasActiveWebUIClients()
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
			if name == "shell_command" {
				if cmd, ok := args["command"].(string); ok && cmd != "" {
					prompt := buildShellApprovalPrompt(secResult)
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

		// Non-interactive path: permissive-by-default (assumes container/sandbox).
		// Only unconditional hard blocks (Critical) terminate the run.
		if secResult.IsHardBlock {
			return wrapSecurityCautionWithLoop(agent, agenterrors.NewSecurityError(
				fmt.Sprintf("fatal security block (non-interactive): %s — %s. "+
					"This operation is unconditionally blocked and cannot be approved by any profile or flag. "+
					"The run will exit.",
					name, secResult.Reasoning), nil), name, args)
		}

		if agent.debug && (secResult.ShouldBlock || secResult.ShouldPrompt) {
			agent.debugLog("[non-interactive] auto-approving %s (risk: %s) — no interactive surface available\n",
				name, secResult.Risk)
		}

		return nil
	}
}
