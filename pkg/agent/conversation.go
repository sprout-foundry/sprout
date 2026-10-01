package agent

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ProcessQuery handles the main conversation loop with the LLM
func (a *Agent) ProcessQuery(userQuery string) (string, error) {
	return a.processQueryWithSeed(QuerySourceUnknown, userQuery)
}

// ProcessQueryAs is ProcessQuery with an explicit caller source recorded on
// the query guard for accurate busy-state messaging.
func (a *Agent) ProcessQueryAs(source, userQuery string) (string, error) {
	return a.processQueryWithSeed(source, userQuery)
}

func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
	return a.ProcessQueryWithContinuityAs(QuerySourceUnknown, userQuery)
}

func (a *Agent) ProcessQueryWithContinuityAs(source, userQuery string) (string, error) {
	if userQuery != "" {
		a.EnableWakeupIfDisabled()
		// A real user query re-engages the wakeup budget. Auto-resume
		// turns (source == QuerySourceAutoResume) must not reset it or a
		// cascading chain of resumes would never exhaust.
		if source != QuerySourceAutoResume {
			a.ResetWakeupBudget()
		}
	}
	if notifications := a.DrainNotifications(); len(notifications) > 0 {
		wakeupMsg := FormatWakeupBatch(notifications)
		if userQuery != "" {
			// The user sees only their own text in the chat bubble; the
			// wakeup batch goes to the model, not the transcript.
			a.setPendingQueryDisplay(userQuery)
			userQuery = wakeupMsg + "\n\n" + userQuery
		} else {
			a.setPendingQueryDisplay(FormatWakeupDisplay(notifications))
			userQuery = wakeupMsg
		}
	}
	// Commit any uncommitted changes and auto-save state on exit.
	defer func() {
		if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
			a.Logger().Debug("DEFER: Attempting to commit %d tracked changes\n", a.GetChangeCount())
			if commitErr := a.CommitChanges("Session cleanup - ensuring changes are not lost"); commitErr != nil {
				a.Logger().Debug("Warning: Failed to commit tracked changes during cleanup: %v\n", commitErr)
			} else {
				a.Logger().Debug("DEFER: Successfully committed tracked changes during cleanup\n")
			}
		} else {
			a.Logger().Debug("DEFER: No changes to commit (enabled: %v, count: %d)\n", a.IsChangeTrackingEnabled(), a.GetChangeCount())
		}

		a.autoSaveState()
		a.Logger().Debug("DEFER: Auto-saved memory state\n")
	}()

	if a.state.GetPreviousSummary() != "" {
		prevSupplement := fmt.Sprintf(
			"## Context From Previous Session\n\n%s\n\nNote: The user cannot see the previous session's responses. Build upon that work but present your response as if it's the first time addressing this topic.",
			a.state.GetPreviousSummary())
		if existing := a.state.GetPendingSystemSupplement(); existing != "" {
			a.setPendingSystemSupplement(existing + "\n\n" + prevSupplement)
		} else {
			a.setPendingSystemSupplement(prevSupplement)
		}
	}

	return a.ProcessQueryAs(source, userQuery)
}

func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	tools := BuildToolDefinitionsForAgent(a)

	// LCM allowlist: applied first as the broadest narrowing pass.
	if allow := a.contextProfile.ToolAllowlist; len(allow) > 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(allow))
	}

	// Filter subagent tools by mode and depth.
	filtered := make([]api.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Function.Name == "run_parallel_subagents" {
			if a.contextProfile.Mode == configuration.ContextModeLowContext || !a.CanSpawnSubagents() {
				continue
			}
		}
		if tool.Function.Name == "run_subagent" && !a.CanSpawnSubagents() {
			continue
		}
		filtered = append(filtered, tool)
	}
	tools = filtered

	if mcpTools := a.getMCPTools(); mcpTools != nil {
		tools = append(tools, mcpTools...)
	}

	// Custom provider tool filtering preserves skill and memory tools.
	if customProvider, ok := a.getCurrentCustomProvider(); ok {
		if len(customProvider.ToolCalls) > 0 {
			allowedToolSet := makeAllowedToolSet(customProvider.ToolCalls)
			for _, t := range alwaysIncludedTools {
				allowedToolSet[t] = struct{}{}
			}
			tools = filterToolsByName(tools, allowedToolSet)
		}
	}

	// Persona tool filter skipped in LCM mode (allowlist is final).
	if personaAllowlist := a.getActivePersonaToolAllowlist(); len(personaAllowlist) > 0 &&
		len(a.contextProfile.ToolAllowlist) == 0 {
		tools = filterToolsByName(tools, makeAllowedToolSet(personaAllowlist))
	}

	return tools
}

func (a *Agent) getCurrentCustomProvider() (*configuration.CustomProviderConfig, bool) {
	if a.configManager == nil {
		return nil, false
	}
	config := a.configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return nil, false
	}

	provider, exists := config.CustomProviders[string(a.getClientType())]
	if !exists {
		return nil, false
	}
	return &provider, true
}

// alwaysIncludedTools are always available regardless of custom provider filtering.
var alwaysIncludedTools = []string{
	"list_skills",
	"activate_skill",
	"manage_memory",
	"TodoWrite",
	"TodoRead",
}

func makeAllowedToolSet(toolNames []string) map[string]struct{} {
	toolSet := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		toolSet[trimmed] = struct{}{}
	}
	return toolSet
}

func filterToolsByName(tools []api.Tool, allowed map[string]struct{}) []api.Tool {
	filtered := make([]api.Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[tool.Function.Name]; !ok {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func (a *Agent) shouldUseDirectMultimodalImageReasoning(messages []api.Message) bool {
	if a == nil || a.client == nil {
		return false
	}
	if !api.ResolveVisionCapability(a.client).AcceptsImages {
		return false
	}

	for _, msg := range messages {
		if msg.Role != "user" || len(msg.Images) == 0 {
			continue
		}
		return true
	}

	return false
}

func (a *Agent) ClearConversationHistory() {
	a.state.SetMessages([]api.Message{})
	a.clearTurnCheckpoints()
	a.state.SetCurrentIteration(0)
	a.state.SetPreviousSummary("")

	a.Logger().Debug("[clean] Conversation history cleared\n")
}

func (a *Agent) SetConversationOptimization(enabled bool) {
	if a.state.GetOptimizer() != nil {
		a.state.GetOptimizer().SetEnabled(enabled)
		if enabled {
			a.Logger().Debug("[*] Conversation optimization enabled\n")
		} else {
			a.Logger().Debug("[tool] Conversation optimization disabled\n")
		}
	}
}

func (a *Agent) GetOptimizationStats() map[string]interface{} {
	if a.state.GetOptimizer() != nil {
		return a.state.GetOptimizer().GetOptimizationStats()
	}
	return map[string]interface{}{
		"enabled": false,
		"message": "Optimizer not initialized",
	}
}
