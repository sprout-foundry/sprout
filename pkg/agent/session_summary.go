package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// topicSummaryMaxRunes is the one-line budget for a session topic summary.
// It fits the CLI picker's detail column at 80 columns and the WebUI
// switcher's preview row; longer text is capped here and may be truncated
// again at render.
const topicSummaryMaxRunes = 120

// topicSignals are the conversation's own signals a deterministic topic
// summary is built from. Nothing here is invented: every field comes from
// recorded actions, invoked tool names, or the project plan's goal.
type topicSignals struct {
	filesCreated  []string
	filesModified []string
	commands      []string
	toolsUsed     []string
	planGoal      string
}

// topicSummaryMaxNames bounds how many file names a summary spells out
// before it collapses the rest into a count.
const topicSummaryMaxNames = 3

// topicSummaryMaxTools bounds the tool-name list in a summary.
const topicSummaryMaxTools = 5

// topicSignalsFromConversation derives summary signals from a conversation's
// messages and recorded task actions. Tool names come from assistant tool
// calls; file paths and commands come from the task actions the tools record.
func topicSignalsFromConversation(messages []api.Message, actions []TaskAction, planGoal string) topicSignals {
	s := topicSignals{planGoal: strings.TrimSpace(planGoal)}
	seenTools := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			name := strings.TrimSpace(call.Function.Name)
			if name == "" {
				continue
			}
			if _, ok := seenTools[name]; ok {
				continue
			}
			seenTools[name] = struct{}{}
			s.toolsUsed = append(s.toolsUsed, name)
		}
	}

	seenFiles := make(map[string]struct{})
	seenCmds := make(map[string]struct{})
	addFile := func(dst []string, path string) []string {
		path = strings.TrimSpace(path)
		if path == "" {
			return dst
		}
		base := filepath.Base(filepath.FromSlash(path))
		if _, ok := seenFiles[base]; ok {
			return dst
		}
		seenFiles[base] = struct{}{}
		return append(dst, base)
	}
	for _, action := range actions {
		switch action.Type {
		case "file_created":
			s.filesCreated = addFile(s.filesCreated, action.Details)
		case "file_modified":
			s.filesModified = addFile(s.filesModified, action.Details)
		case "command_executed", "command_background_executed":
			cmd := firstLine(action.Details)
			if cmd == "" {
				continue
			}
			if _, ok := seenCmds[cmd]; ok {
				continue
			}
			seenCmds[cmd] = struct{}{}
			s.commands = append(s.commands, cmd)
		}
	}
	return s
}

// template renders the deterministic topic summary: the plan goal when one
// exists, then what was created/updated, then what was run, then which tools
// were used. It returns "" when the conversation carries no work signals —
// the caller then falls back to the first-message preview.
func (s topicSignals) template() string {
	var parts []string
	if s.planGoal != "" {
		parts = append(parts, capRunes(s.planGoal, 60))
	}
	if phrase := s.filesPhrase(); phrase != "" {
		parts = append(parts, phrase)
	}
	if phrase := s.commandPhrase(); phrase != "" {
		parts = append(parts, phrase)
	}
	if len(parts) == 0 && len(s.toolsUsed) > 0 {
		tools := s.toolsUsed
		if len(tools) > topicSummaryMaxTools {
			tools = tools[:topicSummaryMaxTools]
		}
		parts = append(parts, "Used "+strings.Join(tools, ", "))
	}
	return capRunes(strings.Join(parts, " — "), topicSummaryMaxRunes)
}

func (s topicSignals) filesPhrase() string {
	var segs []string
	if len(s.filesCreated) > 0 {
		segs = append(segs, "Created "+namedList(s.filesCreated))
	}
	if len(s.filesModified) > 0 {
		segs = append(segs, "Updated "+namedList(s.filesModified))
	}
	return strings.Join(segs, ", ")
}

func (s topicSignals) commandPhrase() string {
	if len(s.commands) == 0 {
		return ""
	}
	phrase := "Ran " + capRunes(s.commands[0], 40)
	if len(s.commands) > 1 {
		phrase += fmt.Sprintf(" (+%d more)", len(s.commands)-1)
	}
	return phrase
}

// namedList renders up to topicSummaryMaxNames file names, collapsing the
// remainder into a count so the summary stays one line.
func namedList(names []string) string {
	if len(names) <= topicSummaryMaxNames {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s +%d more", strings.Join(names[:topicSummaryMaxNames], ", "), len(names)-topicSummaryMaxNames)
}

// capRunes truncates s to at most max runes, appending an ellipsis when it
// had to cut. Truncation is rune-safe so multi-byte text is never split.
func capRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// ResolveSessionTopicSummary returns the one-line topic summary to display
// for a session: the stored summary when present, else the deterministic
// template computed from the session's own signals, else the first-message
// preview. An absent stored field is the normal state for sessions saved
// before summaries existed — resolution degrades, never fails.
func ResolveSessionTopicSummary(state *ConversationState) string {
	if state == nil {
		return ""
	}
	if stored := strings.TrimSpace(state.TopicSummary); stored != "" {
		return stored
	}
	signals := topicSignalsFromConversation(state.Messages, state.TaskActions, loadPlanGoal(state.WorkingDirectory))
	if templated := signals.template(); templated != "" {
		return templated
	}
	return sessionPreviewFromMessages(state.Messages, topicSummaryMaxRunes)
}

// SessionTopicSummaryForDisplayScoped resolves the display summary for a
// stored session, loading its state file. It backs the lazy backfill: old
// sessions gain a summary the first time they are displayed, at zero model
// cost.
func SessionTopicSummaryForDisplayScoped(sessionID, workingDir string) string {
	state, ok := loadSessionStateForSession(sessionID, workingDir)
	if !ok {
		return ""
	}
	return ResolveSessionTopicSummary(state)
}

// SessionTopicSummaryForDisplay resolves the display summary for an already-
// listed session: the stored summary when present, else the deterministic
// template loaded from the session's file (the lazy backfill).
func SessionTopicSummaryForDisplay(info SessionInfo) string {
	if stored := strings.TrimSpace(info.TopicSummary); stored != "" {
		return stored
	}
	return SessionTopicSummaryForDisplayScoped(info.SessionID, info.WorkingDirectory)
}

// sessionLabelMaxRunes bounds the label fallback (first user message) far
// above the old 50-rune preview cap: the topic summary now carries the
// disambiguating detail, so the label may run long and let the renderer
// truncate.
const sessionLabelMaxRunes = 200

// GetSessionLabelScoped returns the session's display label: the stored
// name, else the first user message without the 50-rune preview cap, else
// "".
func GetSessionLabelScoped(sessionID, workingDir string) string {
	state, ok := loadSessionStateForSession(sessionID, workingDir)
	if !ok {
		return ""
	}
	if strings.TrimSpace(state.Name) != "" {
		return strings.TrimSpace(state.Name)
	}
	return sessionPreviewFromMessages(state.Messages, sessionLabelMaxRunes)
}

// loadPlanGoal returns the project plan's goal for root, or "" when there is
// no plan or it cannot be read — an absent or invalid plan is "no signal",
// not an error.
func loadPlanGoal(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	plan, err := planstore.New().Load(root)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(plan.Goal)
}

// topicSummaryTimeout bounds the optional model-written summary call at
// save time. The template is always available, so a slow provider must not
// stall a session save; a var so tests can shrink it.
var topicSummaryTimeout = 5 * time.Second

// topicSummaryModelClient is the minimal model seam the topic summary needs:
// one system+user prompt to one completion. Production adapts the client the
// factory builds for the configured `summarizer` role; tests pass a fake.
type topicSummaryModelClient interface {
	Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// topicSummarySystemPrompt is the instruction for the model-written summary.
// The posture matches the other summarizer roles: the model describes what
// happened, it does not invent.
const topicSummarySystemPrompt = "Summarize what this agent conversation was ABOUT in one short line " +
	"(topic and outcome, at most 120 characters). Use ONLY the conversation excerpt provided; " +
	"do not invent detail. Reply with the summary line and nothing else."

// buildTopicSummaryPrompt renders the conversation as a compact excerpt for
// the summarizer model: user lines, assistant prose, invoked tool names, and
// the first line of each tool result. It returns ("", "") when nothing in
// the conversation is summarizable.
func buildTopicSummaryPrompt(messages []api.Message) (string, string) {
	var b strings.Builder
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			content := strings.TrimSpace(StripUserMessageTimestamp(msg.Content))
			if content == "" {
				continue
			}
			b.WriteString("[user] " + firstLine(content) + "\n")
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				names := make([]string, 0, len(msg.ToolCalls))
				for _, call := range msg.ToolCalls {
					if name := strings.TrimSpace(call.Function.Name); name != "" {
						names = append(names, name)
					}
				}
				if len(names) > 0 {
					b.WriteString("[assistant/tools] " + strings.Join(names, ", ") + "\n")
				}
				continue
			}
			if content := strings.TrimSpace(msg.Content); content != "" {
				b.WriteString("[assistant] " + firstLine(content) + "\n")
			}
		case "tool":
			if line := firstLine(msg.Content); line != "" {
				b.WriteString("[tool] " + line + "\n")
			}
		}
	}
	transcript := strings.TrimSpace(b.String())
	if transcript == "" {
		return "", ""
	}
	const maxTranscriptChars = 8000
	if len(transcript) > maxTranscriptChars {
		transcript = transcript[:maxTranscriptChars]
	}
	return topicSummarySystemPrompt, transcript
}

// writeTopicSummary produces the model-written topic summary via the
// summarizer role's client. It returns "" whenever the model cannot be
// consulted or returns nothing — every failure path degrades to the
// deterministic template, never blocks the save, and never invents detail.
func writeTopicSummary(ctx context.Context, client topicSummaryModelClient, messages []api.Message) string {
	if client == nil {
		return ""
	}
	system, user := buildTopicSummaryPrompt(messages)
	if user == "" {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, topicSummaryTimeout)
	defer cancel()
	out, err := client.Complete(callCtx, system, user)
	if err != nil {
		return ""
	}
	out = strings.TrimSpace(out)
	out = firstLine(out)
	if out == "" {
		return ""
	}
	return capRunes(out, topicSummaryMaxRunes)
}

// newTopicSummaryClientForTesting is the seam tests stub to control client
// creation; production resolves the summarizer role's client.
var newTopicSummaryClientForTesting = (*Agent).newTopicSummaryClient

// newTopicSummaryClient builds the optional model client for the session
// topic summary from the agent's configuration. It returns nil unless the
// `summarizer` role is configured and a client can be created — a nil
// client means "use the deterministic template".
func (a *Agent) newTopicSummaryClient() topicSummaryModelClient {
	if a == nil {
		return nil
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return nil
	}
	role := cfg.GetRole(configuration.RoleSummarizer)
	if role.Provider == "" && role.Model == "" {
		return nil
	}
	provider, model := cfg.ResolveRole(configuration.RoleSummarizer)
	if provider == "" {
		return nil
	}
	client, err := factory.CreateProviderClient(api.ClientType(provider), model)
	if err != nil || client == nil {
		return nil
	}
	return &topicSummaryClientAdapter{c: client}
}

// topicSummaryClientAdapter adapts an api.ClientInterface to the
// topicSummaryModelClient seam.
type topicSummaryClientAdapter struct {
	c api.ClientInterface
}

func (m *topicSummaryClientAdapter) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	resp, err := m.c.SendChatRequest(ctx, []api.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, nil, "", false)
	if err != nil {
		return "", fmt.Errorf("session topic summary model call: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("session topic summary model call: no choices in response")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

// generateTopicSummary returns the topic summary for the agent's current
// conversation: model-written when the summarizer role is configured and
// answers, otherwise the deterministic template from the conversation's own
// signals. Called from the session-save path, a natural boundary.
func (a *Agent) generateTopicSummary(ctx context.Context) string {
	messages := a.state.GetMessages()
	actions := a.GetTaskActions()
	client := newTopicSummaryClientForTesting(a)
	if client == nil {
		// The template path: the plan goal is only consulted when the
		// model is not, so the no-summarizer default costs no avoidable
		// work and a model failure degrades straight to the template.
		signals := topicSignalsFromConversation(messages, actions, loadPlanGoal(a.currentWorkspaceRoot()))
		return signals.template()
	}
	if summary := writeTopicSummary(ctx, client, messages); summary != "" {
		return summary
	}
	signals := topicSignalsFromConversation(messages, actions, loadPlanGoal(a.currentWorkspaceRoot()))
	return signals.template()
}
