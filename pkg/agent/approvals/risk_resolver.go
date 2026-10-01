// Package approvals: the risk resolver (SP-141 phase 3, increment 6).
// ResolveToolRisk produces the unified risk assessment for a tool call by
// folding all security inputs onto the Low/Medium/High/Critical scale:
// the static classifier, the persona cascade, the git gates, the
// workspace security policy, and the filesystem path tiers. It operates
// on the RiskAgent seam (risk_agent.go) so it lives here, out of the god
// package.
package approvals

import (
	"fmt"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/shelltext"
)

func ResolveToolRisk(a RiskAgent, toolName string, args map[string]interface{}) RiskAssessment {
	// 1. Static classifier (always), workspace-augmented for shell commands
	var wsRoot string
	if a != nil {
		wsRoot = a.GetWorkspaceRoot()
	}
	secResult := tools.ClassifyToolCallWithWorkspace(toolName, args, wsRoot)
	assessment := AssessmentFromClassifier(secResult)

	// Downgrade privileged commands when a password prompter is registered.
	if toolName == "shell_command" && a != nil && a.HasPasswordPrompter() {
		if secResult.Category == tools.RiskCategoryPrivileged && assessment.Level.Rank() >= configuration.RiskLevelHigh.Rank() {
			assessment.Level = configuration.RiskLevelMedium
			assessment.IsHardBlock = false
			assessment.Sources = append(assessment.Sources, RiskSourcePasswordPrompter)
			assessment.Reason = "privileged command allowed with password prompter (sudo/passwd will prompt for password)"
		}
	}

	// 2. Persona cascade (shell_command only)
	if toolName == "shell_command" && a != nil {
		if cmd, ok := args["command"].(string); ok && cmd != "" {
			level := a.EvaluateOperationRisk(cmd)
			assessment = assessment.Combine(
				AssessmentFromPersonaCascade(level, fmt.Sprintf("persona/profile risk cascade: %s", level)),
			)

			// 3. Git history-rewrite gate (promptable, not a hard block).
			// Rebase is unconditionally banned; --abort is the only permitted form.
			if shelltext.IsGitHistoryRewriteCommand(cmd) {
				if IsGitRebaseCommand(cmd) {
					assessment = assessment.Combine(
						RiskAssessment{
							Level:       configuration.RiskLevelCritical,
							IsHardBlock: true,
							Sources:     []RiskSource{RiskSourceGitRebase},
							Reason:      "git rebase is banned by AGENTS.md (all forms: interactive, --continue, --skip, `git pull --rebase`); use `git merge` to integrate upstream. The only permitted invocation is `git rebase --abort` for recovery.",
						},
					)
				} else {
					cfg := a.GetConfig()
					if cfg == nil || !cfg.AllowGitHistoryRewrite {
						assessment = assessment.Combine(
							RiskAssessment{
								Level:   configuration.RiskLevelHigh,
								Sources: []RiskSource{RiskSourceGitHistoryRewrite},
								Reason:  "git history-rewrite operation requires approval",
							},
						)
					}
				}
			}

			// 4. Git write gate
			if IsGitWriteCommand(cmd) && !a.IsGitWriteAllowed() {
				assessment = assessment.Combine(
					RiskAssessment{
						Level:   configuration.RiskLevelHigh,
						Sources: []RiskSource{RiskSourceGitWrite},
						Reason:  "git write operation not allowed for current persona",
					},
				)
			}

			// 6. Workspace security policy
			if cfg := a.GetConfig(); cfg != nil && cfg.SecurityPolicy != nil {
				policyAction := cfg.SecurityPolicy.Evaluate(cmd)
				switch policyAction {
				case configuration.PolicyDeny:
					assessment = assessment.Combine(
						RiskAssessment{
							Level:       configuration.RiskLevelCritical,
							IsHardBlock: true,
							Sources:     []RiskSource{RiskSourceWorkspacePolicy},
							Reason:      "workspace security policy denies this command",
						},
					)
				case configuration.PolicyPrompt:
					if assessment.Level.Rank() <= configuration.RiskLevelLow.Rank() {
						assessment = assessment.Combine(
							RiskAssessment{
								Level:   configuration.RiskLevelMedium,
								Sources: []RiskSource{RiskSourceWorkspacePolicy},
								Reason:  "workspace security policy requires prompt for this command",
							},
						)
					}
				}
			}
		}
	}

	// 5. Filesystem path-tier (file tools). Only write tools contribute risk.
	if (toolName == "write_file" || toolName == "edit_file" ||
		toolName == "write_structured_file" || toolName == "patch_structured_file" ||
		toolName == "read_file") && a != nil {
		if pathRaw, ok := args["path"].(string); ok && pathRaw != "" {
			home := a.HomeDir()
			tier := ClassifyPathAccess(pathRaw, a.GetWorkspaceRoot(), home, a.EffectiveCwd())
			assessment.PathTier = tier
			assessment.FileMode = AccessModeForTool(toolName)

			isWriteTool := toolName == "write_file" || toolName == "edit_file" ||
				toolName == "write_structured_file" || toolName == "patch_structured_file"

			if isWriteTool {
				switch tier {
				case PathTierSensitive:
					assessment = assessment.Combine(
						RiskAssessment{
							Level:   configuration.RiskLevelHigh,
							Sources: []RiskSource{RiskSourceFSTier},
							Reason:  fmt.Sprintf("path %s is in a sensitive filesystem tier", pathRaw),
						},
					)
				case PathTierExternal:
					// Session-scoped folder allowlist: skip if user already approved this folder.
					if a.IsFolderSessionAllowed(pathRaw) {
						if a.DebugEnabled() {
							a.DebugLogf("[risk] %s path %s is under a session-allowed folder — skipping external-tier Medium contribution\n", toolName, pathRaw)
						}
					} else {
						assessment = assessment.Combine(
							RiskAssessment{
								Level:   configuration.RiskLevelMedium,
								Sources: []RiskSource{RiskSourceFSTier},
								Reason:  fmt.Sprintf("path %s is outside the workspace (external tier)", pathRaw),
							},
						)
					}
				}
			}
		}
	}

	return assessment
}
