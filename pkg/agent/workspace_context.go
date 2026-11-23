package agent

import (
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// With workspace_context_placement "user", the project context file and the working directory
// leave the system prompt for the conversation's first user message. The system prompt and the
// tools are then the same in every directory, so a server's prefix cache can serve every
// conversation's start from one image instead of prefilling the whole prompt per directory.
// The block is stamped into the message once, like the turn timestamp, so later requests replay
// it byte-identically.

func (a *Agent) workspaceContextInUserMessage() bool {
	if a == nil || a.configManager == nil {
		return false
	}
	cfg := a.configManager.GetConfig()
	return cfg != nil && strings.EqualFold(strings.TrimSpace(cfg.WorkspaceContextPlacement), "user")
}

// withWorkspaceContext prefixes the conversation's first user message with the workspace block.
func (a *Agent) withWorkspaceContext(query string) string {
	if !a.workspaceContextInUserMessage() || strings.TrimSpace(query) == "" ||
		hasUserMessage(a.GetMessages()) {
		return query
	}
	block := workspaceContextBlock(a.currentWorkspaceRoot())
	if block == "" {
		return query
	}
	return block + "\n\n" + query
}

func hasUserMessage(messages []api.Message) bool {
	for _, m := range messages {
		if m.Role == "user" {
			return true
		}
	}
	return false
}

// workspaceContextBlock is what the system prompt's tail held: the project context file and the
// working directory, in a tag the model can tell apart from the request.
func workspaceContextBlock(workspaceRoot string) string {
	var sb strings.Builder
	if files, err := LoadContextFiles(); err == nil {
		sb.WriteString(strings.TrimSpace(files))
	}
	sb.WriteString(buildCurrentWorkingDirectorySection(workspaceRoot))
	body := strings.TrimSpace(strings.Trim(strings.TrimSpace(sb.String()), "-"))
	if body == "" {
		return ""
	}
	return "<workspace-context>\n" + body + "\n</workspace-context>"
}
