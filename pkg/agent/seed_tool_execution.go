// Package agent: tool error handling (handleToolError), local provider
// detection, and the postProcessResult pipeline for seed tool execution.
// (split from seed_tool_registry.go)
package agent

import (
	"context"
	"errors"
	"fmt"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// logToolExecution is a legacy helper that was used to print tool execution
// messages in non-streaming mode. Now that richEventPublisher emits
// ToolLog("executing tool", ...) on tool_start for all modes, this is a
// no-op to avoid duplicate output.
func logToolExecution(_ *Agent, _ string) {
}

// handleToolError wraps a handler error into a sanitized result string and
// returns it along with the original error. Security cautions are published for terminal rendering.
func handleToolError(agent *Agent, err error, toolName string) (string, error) {
	if err == nil {
		return "", nil
	}
	safeMsg := sanitizeToolFailureMessage(err.Error())

	// Use typed error classification first to decide behavior.
	action := ClassifyError(err)

	switch action {
	case ActionEscalate:
		if agent != nil {
			agent.incrementSecurityCautionsIssued()
			agent.PublishAgentMessage("security_caution", safeMsg, nil)
			assessment := RiskAssessment{
				Sources: []RiskSource{RiskSourceHandler},
				Reason:  safeMsg,
			}
			agent.logSecurityDecision(toolName, nil, assessment, "blocked")
		}
		return "", agenterrors.NewSecurityError(
			fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s. %s", safeMsg, tierFromMessage(safeMsg)),
			err,
		)

	case ActionFail:
		if agent != nil {
			agent.PublishAgentMessage("tool_error", fmt.Sprintf("Tool '%s' failed: %s", toolName, safeMsg), nil)
		}
		return fmt.Sprintf("Error: %s", safeMsg), err

	default:
		if agent != nil {
			var label string
			switch {
			case agenterrors.IsRateLimited(err):
				label = "Tool '" + toolName + "' failed (rate limited): " + safeMsg
				provider := agent.GetProvider()
				if te := agenterrors.AsTypedError(err); te != nil {
					if p, ok := te.Details["provider"].(string); ok && p != "" {
						provider = p
					}
				}
				if provider == "" {
					var ae *agenterrors.AgentError
					if errors.As(err, &ae) && ae != nil {
						p := ae.GetMetadata("provider")
						if p != "" {
							provider = p
						}
					}
				}
				ev := events.RateLimitedEventFromError(provider, 1, 5, 0, agent.GetSessionID(), err)
				if ev != nil {
					agent.PublishRateLimited(ev)
				}
			case agenterrors.IsProviderError(err):
				label = "Tool '" + toolName + "' failed (provider): " + safeMsg
			default:
				label = "Tool '" + toolName + "' failed (transient): " + safeMsg
			}
			agent.PublishAgentMessage("tool_error", label, nil)
		}
		return fmt.Sprintf("Error: %s", safeMsg), err
	}
}

// isLocalProvider returns true if the provider runs locally and never sends data outside the network.
func isLocalProvider(agent *Agent) bool {
	if agent == nil {
		return false
	}
	ct := agent.GetProviderType()
	switch ct {
	case api.OllamaLocalClientType,
		api.OllamaClientType, // "ollama" alias for ollama-local
		api.OllamaCloudClientType,
		api.LMStudioClientType,
		api.TestClientType,
		api.EditorClientType:
		return true
	}
	return false
}

// postProcessResult applies model-specific constraints, truncation, secret redaction,
// duplicate embedding check, and TodoWrite event emission. Returns the final result string.
func postProcessResult(ctx context.Context, agent *Agent, toolName string, args map[string]interface{}, result string) string {
	if result == "" {
		return result
	}

	// 1. Model-specific constraints (constrainToolResultForModel handles fetch_url and analyze_image_content)
	result = constrainToolResultForModel(toolName, args, result)

	// 2. Universal truncation
	result = truncateToolResult(result)

	// 3. Secret redaction (only for sensitive tools, skip if local provider)
	if !isLocalProvider(agent) && isSecretSensitiveTool(toolName) && agent.security.GetOutputRedactor() != nil {
		redactResult := agent.security.GetOutputRedactor().RedactToolOutput(result, toolName, args)
		if len(redactResult.Secrets) > 0 {
			source := buildSecretSource(toolName, args)
			action, evalErr := agent.security.GetElevationGate().Evaluate(redactResult.Secrets, source)
			if evalErr != nil {
				if agent.debug {
					agent.debugLog("[security] elevation gate error: %v\n", evalErr)
				}
			}
			switch action {
			case security.SecretAllow:
				// keep original (already redacted by the redactor as fallback)
				if agent.debug {
					agent.debugLog("[security] user allowed %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
				}
				result = redactResult.Content
			case security.SecretBlock:
				if agent.debug {
					agent.debugLog("[security] blocked %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
				}
				return fmt.Sprintf("BLOCKED: detected secrets in output. Operation blocked. Found %d secret(s) — user chose to block.", len(redactResult.Secrets))
			default:
				// SecretRedact — redactResult.Content is already redacted
				if agent.debug {
					agent.debugLog("[security] redacted %d secret(s) from %s\n", len(redactResult.Secrets), toolName)
				}
				result = redactResult.Content
			}
		}
	}

	// 4. Duplicate embedding check + async re-index for write tools
	if shouldCheckDuplicates(toolName, agent) {
		if path, ok := args["path"].(string); ok && path != "" {
			if note := runDuplicateCheck(ctx, agent, path); note != "" {
				result = result + note
			}
			reindexFileAfterWrite(agent, path)
		}
	}

	// 5. TodoWrite event emission
	if toolName == "TodoWrite" {
		agent.PublishTodoUpdate(formatTodoItemsForEvent(agent.GetTodoManager().Read()))
	}

	return result
}
