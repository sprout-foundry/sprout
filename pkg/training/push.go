package training

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// pushTimeout is the maximum time PushSession will wait for the training
// endpoint to respond. This keeps the fire-and-forget goroutine from
// hanging on unresponsive servers.
const pushTimeout = 5 * time.Second

// PushSession pushes a PII-redacted conversation state to the training
// endpoint. It applies the same PII redaction pipeline as the export command
// (RedactContent), so no raw PII or secrets leave the machine.
//
// This is a fire-and-forget operation — errors are returned (for optional
// logging by the caller) but must never cause a session save to fail or
// panic. The caller is expected to invoke this in a goroutine.
//
// If the session's working directory matches any excludePath prefix, the
// push is skipped silently (returns nil).
func PushSession(state agent.ConversationState, endpoint string, excludePaths []string) error {
	// Skip silently if the working directory is excluded.
	if isExcludedDirectory(state.WorkingDirectory, excludePaths) {
		return nil
	}

	// Deep-copy and redact the state so the original is never mutated.
	redacted := redactConversationState(state)

	data, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Errorf("training: failed to marshal redacted state: %w", err)
	}

	url := endpoint + "/sessions"
	httpClient := &http.Client{Timeout: pushTimeout}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("training: failed to push session to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("training: endpoint %s returned non-success status %d", url, resp.StatusCode)
	}

	return nil
}

// redactConversationState returns a deep copy of the state with all PII
// and secrets scrubbed from message content and tool call arguments.
func redactConversationState(state agent.ConversationState) agent.ConversationState {
	// Copy the state by value (slices/maps below are deep-copied).
	result := state

	// Pre-scan all content for remote usernames so they can be redacted
	// even when they appear in fields without home directory paths.
	var allContent []string
	for _, msg := range state.Messages {
		allContent = append(allContent, msg.Content)
		if msg.ReasoningContent != "" {
			allContent = append(allContent, msg.ReasoningContent)
		}
		for _, tc := range msg.ToolCalls {
			allContent = append(allContent, tc.Function.Arguments)
		}
	}
	for _, action := range state.TaskActions {
		allContent = append(allContent, action.Description, action.Details)
	}
	allContent = append(allContent, state.Name, state.WorkingDirectory)
	SetRemoteUsernames(mergeUsernames(remoteUsernamesForRedaction, CollectRemoteUsernames(allContent)))

	// Deep-copy and redact messages.
	result.Messages = make([]api.Message, len(state.Messages))
	for i, msg := range state.Messages {
		msg.Content = RedactContent(msg.Content)
		msg.ReasoningContent = RedactContent(msg.ReasoningContent)
		// Redact tool call arguments within assistant messages.
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]api.ToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				tc.Function.Arguments = RedactContent(tc.Function.Arguments)
				toolCalls[j] = tc
			}
			msg.ToolCalls = toolCalls
		}
		result.Messages[i] = msg
	}

	// Redact task action descriptions and details (may contain file paths,
	// shell commands, or other PII-bearing content).
	if len(state.TaskActions) > 0 {
		actions := make([]agent.TaskAction, len(state.TaskActions))
		for i, action := range state.TaskActions {
			action.Description = RedactContent(action.Description)
			action.Details = RedactContent(action.Details)
			actions[i] = action
		}
		result.TaskActions = actions
	}

	// Redact the session name and working directory (may contain paths/usernames).
	result.Name = RedactContent(state.Name)
	result.WorkingDirectory = RedactContent(state.WorkingDirectory)

	return result
}
