package agent

import (
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// The project context file and the working directory go in the conversation's first user message,
// not the system prompt. The system prompt and the tools are then the same in every directory,
// so a server's prefix cache can serve every conversation's start from one image instead of
// prefilling the whole prompt per directory. The block is stamped into the message once, like
// the turn timestamp, so later requests replay it byte-identically.

// ownsWorkspaceContext: the agent's system prompt is the embedded one, which used to carry the
// workspace context. A system_prompt_text override or a persona that replaced the prompt never
// carried it, so those conversations get no block either.
func (a *Agent) ownsWorkspaceContext() bool {
	if a == nil || a.personaReplacedPrompt {
		return false
	}
	if a.configManager != nil {
		if cfg := a.configManager.GetConfig(); cfg != nil && strings.TrimSpace(cfg.SystemPromptText) != "" {
			return false
		}
	}
	return true
}

// withWorkspaceContext prefixes the conversation's first user message with the workspace block.
func (a *Agent) withWorkspaceContext(query string) string {
	if !a.ownsWorkspaceContext() || strings.TrimSpace(query) == "" ||
		hasUserMessage(a.GetMessages()) {
		return query
	}
	block := workspaceContextBlock(a.currentProjectFolder())
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
	return workspaceContextOpenTag + "\n" + body + "\n" + workspaceContextCloseTag
}

const (
	workspaceContextOpenTag  = "<workspace-context>"
	workspaceContextCloseTag = "</workspace-context>"
)

// StripUserMessageEnvelope is the text the user wrote: the message without its turn timestamp and
// without the workspace block the conversation's first message carries. Persisted state keeps
// the block (StripUserMessageTimestamp alone), since the system prompt does not repeat it.
func StripUserMessageEnvelope(userMessage string) string {
	body := StripUserMessageTimestamp(userMessage)
	if !strings.HasPrefix(body, workspaceContextOpenTag) {
		return body
	}
	end := strings.Index(body, workspaceContextCloseTag)
	if end < 0 {
		return body
	}
	body = body[end+len(workspaceContextCloseTag):]
	body = strings.TrimPrefix(body, "\r\n\r\n")
	return strings.TrimPrefix(body, "\n\n")
}
