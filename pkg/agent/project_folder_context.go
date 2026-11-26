package agent

import (
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// The project context file, the working directory and the memory block go in the conversation's
// first user message, not the system prompt. The system prompt and the tools are then the same
// in every directory and every session, so a server's prefix cache can serve every
// conversation's start from one image instead of prefilling the whole prompt per directory.
// The block is stamped into the message once, like the turn timestamp, so later requests
// replay it byte-identically.

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

// withProjectFolderContext prefixes the conversation's first user message with the project folder block:
// the project context file, the working directory and the memory block. The system prompt and
// the tools stay byte-identical across sessions whatever the memory store holds.
func (a *Agent) withProjectFolderContext(query string) string {
	if !a.ownsWorkspaceContext() || strings.TrimSpace(query) == "" ||
		hasUserMessage(a.GetMessages()) {
		return query
	}
	block := workspaceContextBlock(a.currentProjectFolder(), a.memoryBlock())
	if block == "" {
		return query
	}
	return block + "\n\n" + query
}

// memoryBlock renders the memories the profile puts in the first user message. It is built
// once per conversation here, at the seed of the first turn, so mid-session memory writes do
// not change a running session.
func (a *Agent) memoryBlock() string {
	if a == nil {
		return ""
	}
	return LoadMemoriesForPromptMode(a.contextProfile.MemoryPrompt, a.contextProfile.MemoryPromptMaxBytes)
}

func hasUserMessage(messages []api.Message) bool {
	for _, m := range messages {
		if m.Role == "user" {
			return true
		}
	}
	return false
}

// workspaceContextBlock is what the system prompt's tail held: the project context file, the
// working directory and the memory block, in a tag the model can tell apart from the request.
func workspaceContextBlock(workspaceRoot, memories string) string {
	var sb strings.Builder
	if files, err := LoadContextFiles(); err == nil {
		sb.WriteString(strings.TrimSpace(files))
	}
	sb.WriteString(buildCurrentWorkingDirectorySection(workspaceRoot))
	body := strings.TrimSpace(strings.Trim(strings.TrimSpace(sb.String()), "-"))
	mem := strings.TrimSpace(memories)
	if mem != "" {
		if body != "" {
			body += "\n\n" + mem
		} else {
			body = mem
		}
	}
	if body == "" {
		return ""
	}
	return projectFolderContextOpenTag + "\n" + body + "\n" + projectFolderContextCloseTag
}

const (
	projectFolderContextOpenTag  = "<project-folder-context>"
	projectFolderContextCloseTag = "</project-folder-context>"
	// Conversations saved before the rename carry the block under these tags.
	earlierContextOpenTag  = "<workspace-context>"
	earlierContextCloseTag = "</workspace-context>"
)

// StripUserMessageEnvelope is the text the user wrote: the message without its turn timestamp and
// without the project folder block the conversation's first message carries. Persisted state keeps
// the block (StripUserMessageTimestamp alone), since the system prompt does not repeat it.
func StripUserMessageEnvelope(userMessage string) string {
	body := StripUserMessageTimestamp(userMessage)
	closeTag := projectFolderContextCloseTag
	switch {
	case strings.HasPrefix(body, projectFolderContextOpenTag):
	case strings.HasPrefix(body, earlierContextOpenTag):
		closeTag = earlierContextCloseTag
	default:
		return body
	}
	end := strings.Index(body, closeTag)
	if end < 0 {
		return body
	}
	body = body[end+len(closeTag):]
	body = strings.TrimPrefix(body, "\r\n\r\n")
	return strings.TrimPrefix(body, "\n\n")
}
