// Package training provides utilities for exporting session data into
// training-ready formats (ShareGPT, OpenAI fine-tuning JSONL, Alpaca).
package training

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Export options / result
// ---------------------------------------------------------------------------

// ExportOptions configures what sessions to export and how to format them.
type ExportOptions struct {
	// Format is one of "sharegpt", "openai", "alpaca".
	Format string

	// Output is the destination file path.
	Output string

	// All, when true, exports sessions from every directory scope.
	All bool

	// MinTurns is the minimum number of user+assistant exchanges required.
	MinTurns int

	// MinActions is the minimum number of TaskActions required.
	MinActions int

	// NoToolResults, when true, replaces tool-result messages with short
	// placeholders instead of including the raw content.
	NoToolResults bool

	// IncludeSystem includes system-prompt messages in the output.
	IncludeSystem bool

	// Session, when non-empty, exports only the session with this ID.
	Session string

	// StructuredTools, when true, preserves the OpenAI function-calling
	// schema: assistant messages retain their ToolCalls arrays and tool
	// results keep role:"tool" with tool_call_id. When false (default),
	// tool calls are flattened to text and tool results are converted to
	// user-role messages with a "[Tool Result]" prefix.
	StructuredTools bool

	// IncludeSubagents, when true, extracts single-task examples from
	// run_subagent and run_parallel_subagents tool calls within each
	// session and appends them to the output alongside the regular
	// conversation examples. Only meaningful for the "openai" format.
	IncludeSubagents bool

	// ExcludePaths is a list of absolute path prefixes. Sessions whose
	// WorkingDirectory starts with any of these paths are excluded.
	ExcludePaths []string
}

// ExportResult contains statistics about an export run.
type ExportResult struct {
	SessionsScanned   int    `json:"sessions_scanned"`
	SessionsExported  int    `json:"sessions_exported"`
	ExamplesGenerated int    `json:"examples_generated"`
	SessionsFiltered  int    `json:"sessions_filtered"`
	OutputPath        string `json:"output_path"`
}

// ---------------------------------------------------------------------------
// ShareGPT types
// ---------------------------------------------------------------------------

// ShareGPTConversation represents one conversation in ShareGPT format.
type ShareGPTConversation struct {
	ID       string            `json:"id"`
	Messages []ShareGPTMessage `json:"messages"`
	Metadata ShareGPTMetadata  `json:"metadata"`
}

// ShareGPTMessage is a single message in a ShareGPT conversation.
type ShareGPTMessage struct {
	Role       string          `json:"role"` // "system", "user", "assistant"
	Content    string          `json:"content"`
	ToolCalls  []api.ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// ShareGPTMetadata holds extra information about a ShareGPT conversation.
type ShareGPTMetadata struct {
	SessionID   string  `json:"session_id"`
	SessionName string  `json:"session_name"`
	Source      string  `json:"source"`
	Model       string  `json:"model,omitempty"`
	Provider    string  `json:"provider,omitempty"`
	TotalCost   float64 `json:"total_cost"`
	WorkingDir  string  `json:"working_directory"`
}

// ---------------------------------------------------------------------------
// OpenAI fine-tuning types
// ---------------------------------------------------------------------------

// OpenAITrainingExample is one training example for OpenAI fine-tuning JSONL.
type OpenAITrainingExample struct {
	Messages []OpenAIMessage `json:"messages"`
}

// OpenAIMessage is a single message within an OpenAI training example.
type OpenAIMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []api.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// ---------------------------------------------------------------------------
// Alpaca types
// ---------------------------------------------------------------------------

// AlpacaExample is one training example in Alpaca format.
type AlpacaExample struct {
	Instruction string `json:"instruction"`
	Input       string `json:"input"`
	Output      string `json:"output"`
}

// ---------------------------------------------------------------------------
// Core export
// ---------------------------------------------------------------------------

// ExportSessions reads sessions according to opts and writes the result to
// opts.Output. It returns a summary of the operation.
func ExportSessions(opts ExportOptions) (*ExportResult, error) {
	if err := validateOptions(opts); err != nil {
		return nil, fmt.Errorf("get config dir: %w", err)
	}

	// Collect candidate sessions.
	var candidates []candidateSession
	if opts.Session != "" {
		cs, err := loadSpecificSession(opts.Session)
		if err != nil {
			return nil, fmt.Errorf("create output directory: %w", err)
		}
		if cs != nil {
			candidates = append(candidates, *cs)
		}
	} else if opts.All {
		sessions, err := agent.ListAllSessionsWithTimestamps()
		if err != nil {
			return nil, fmt.Errorf("failed to list sessions: %w", err)
		}
		for _, s := range sessions {
			candidates = append(candidates, candidateSession{Info: s})
		}
	} else {
		sessions, err := agent.ListSessionsWithTimestamps()
		if err != nil {
			return nil, fmt.Errorf("failed to list sessions: %w", err)
		}
		for _, s := range sessions {
			candidates = append(candidates, candidateSession{Info: s})
		}
	}

	// Sort candidates newest-first so output is deterministic.
	sortCandidateSessionsNewestFirst(candidates)

	result := &ExportResult{
		SessionsScanned: len(candidates),
		OutputPath:      opts.Output,
	}

	// Filter sessions by quality thresholds and exclusions.
	var qualified []agent.ConversationState
	for _, c := range candidates {
		state, err := loadSessionState(c)
		if err != nil {
			// Skip sessions that can't be loaded but count them as filtered.
			result.SessionsFiltered++
			continue
		}
		if isExcludedDirectory(state.WorkingDirectory, opts.ExcludePaths) {
			result.SessionsFiltered++
			continue
		}
		if !meetsThresholds(*state, opts.MinTurns, opts.MinActions) {
			result.SessionsFiltered++
			continue
		}
		qualified = append(qualified, *state)
	}

	result.SessionsExported = len(qualified)

	// Scan working directories for remote usernames (e.g. /home/aprice)
	// so they can be redacted even when they appear in command output
	// without the full home directory path.
	var workingDirs []string
	for _, q := range qualified {
		if q.WorkingDirectory != "" {
			workingDirs = append(workingDirs, q.WorkingDirectory)
		}
	}
	existingUsers := remoteUsernamesForRedaction
	SetRemoteUsernames(append(existingUsers, scanWorkingDirsForUsernames(workingDirs)...))

	// Build format-specific output.
	switch opts.Format {
	case "sharegpt":
		examples, err := buildShareGPT(qualified, opts)
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
		result.ExamplesGenerated = len(examples)
		if err := writeJSONArray(examples, opts.Output); err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}

	case "openai":
		examples, err := buildOpenAI(qualified, opts)
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
		result.ExamplesGenerated = len(examples)
		if err := writeJSONL(examples, opts.Output); err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}

	case "alpaca":
		examples, err := buildAlpaca(qualified, opts)
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
		result.ExamplesGenerated = len(examples)
		if err := writeJSONArray(examples, opts.Output); err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported format %q: must be one of sharegpt, openai, alpaca", opts.Format)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

type candidateSession struct {
	Info agent.SessionInfo
}

func validateOptions(opts ExportOptions) error {
	supportedFormats := map[string]bool{"sharegpt": true, "openai": true, "alpaca": true}
	if !supportedFormats[opts.Format] {
		return fmt.Errorf("unsupported format %q: must be one of sharegpt, openai, alpaca", opts.Format)
	}
	if strings.TrimSpace(opts.Output) == "" {
		return fmt.Errorf("--output is required")
	}
	if opts.MinTurns < 0 {
		return fmt.Errorf("--min-turns must be >= 0")
	}
	if opts.MinActions < 0 {
		return fmt.Errorf("--min-actions must be >= 0")
	}
	return nil
}

func loadSpecificSession(sessionID string) (*candidateSession, error) {
	sessions, err := agent.ListAllSessionsWithTimestamps()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	for _, s := range sessions {
		if s.SessionID == sessionID {
			return &candidateSession{Info: s}, nil
		}
	}
	return nil, nil
}

func loadSessionState(c candidateSession) (*agent.ConversationState, error) {
	if c.Info.StoragePath != "" {
		return agent.ImportStateFromJSONFile(c.Info.StoragePath)
	}
	return agent.LoadStateWithoutAgentScoped(c.Info.SessionID, c.Info.WorkingDirectory)
}

// meetsThresholds checks whether a conversation passes quality filters.
// meetsThresholds checks if a session has enough substance for training.
// The primary quality signal is agentic richness (tool calls + tool results),
// not user turn count — automated workflow sessions may have a single user
// prompt followed by a deep chain of subagent orchestration with dozens of
// tool calls, which are high-value training data.
func meetsThresholds(state agent.ConversationState, minTurns, minActions int) bool {
	if len(state.TaskActions) < minActions {
		return false
	}
	// Count turn-like exchanges: either user→assistant pairs OR assistant
	// messages with tool calls (agentic turns that don't need a preceding
	// user message, common in automated workflows).
	turns := countAgenticTurns(state.Messages)
	if turns < minTurns {
		return false
	}
	return true
}

// countAgenticTurns counts meaningful conversation turns, treating
// assistant messages with tool calls as turns even without a preceding
// user message. This ensures automated workflow sessions (1 user prompt →
// many autonomous tool-calling turns) score high rather than being
// filtered as single-turn.
func countAgenticTurns(messages []api.Message) int {
	turns := 0
	pendingUser := false
	for _, m := range messages {
		switch m.Role {
		case "user":
			pendingUser = true
		case "assistant":
			if pendingUser {
				turns++
				pendingUser = false
			} else if len(m.ToolCalls) > 0 {
				// Autonomous agentic turn — no user prompt needed.
				// This captures workflow/automation sessions where the
				// model chains tool calls autonomously.
				turns++
			}
		case "tool":
			// Tool messages don't affect turn counting.
		}
	}
	return turns
}

// ---------------------------------------------------------------------------
// Message cleaning
// ---------------------------------------------------------------------------

// toolCallsToText converts structured tool calls into human-readable text.
func toolCallsToText(toolCalls []api.ToolCall, existingContent string) string {
	var sb strings.Builder
	if strings.TrimSpace(existingContent) != "" {
		sb.WriteString(existingContent)
		sb.WriteString("\n\n")
	}
	for i, tc := range toolCalls {
		name := tc.Function.Name
		args := tc.Function.Arguments
		sb.WriteString(fmt.Sprintf("Tool call: %s(%s)", name, args))
		if i < len(toolCalls)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// inferToolName returns a short label for a tool-role message.
func inferToolName(msg api.Message) string {
	if strings.TrimSpace(msg.Content) == "" {
		return "empty"
	}
	return "result"
}

// toolResultMarker is the prefix applied to tool-result messages that have
// been converted to user-role content. It is also used by
// deduplicateConsecutive as a guard: messages carrying this marker (or the
// "Tool call:" marker from toolCallsToText) are never merged, even when
// adjacent to another message of the same role, so conversation flow stays
// intact.
const toolResultMarker = "[Tool Result]"

// toolCallMarker is the text prefix emitted by toolCallsToText for assistant
// messages that invoked tools. Like toolResultMarker, it is used as a
// dedup guard.
const toolCallMarker = "Tool call:"

// flattenStandardMessages cleans and prepares a raw message slice for
// training export.
//
// System messages are stripped unless opts.IncludeSystem.
//
// When opts.StructuredTools is true, the OpenAI function-calling schema is
// preserved verbatim:
//   - assistant messages keep their ToolCalls arrays (not flattened to text)
//   - tool messages keep role:"tool" and their ToolCallID
// No deduplication is applied in this mode — structured messages must never
// be merged.
//
// When opts.StructuredTools is false (default), tool calls and results are
// flattened to text for models that don't understand function-calling:
//   - assistant ToolCalls are converted to readable text via toolCallsToText
//     (when opts.NoToolResults is true; otherwise left intact but still
//     non-structured for export purposes)
//   - tool messages are kept as role:"tool" through this function so the
//     format builders can decide how to render them. (They carry
//     placeholders when opts.NoToolResults is true.)
// Consecutive same-role messages are merged via deduplicateConsecutive,
// which never merges messages carrying the tool markers.
func flattenStandardMessages(messages []api.Message, opts ExportOptions) []api.Message {
	var result []api.Message
	for _, m := range messages {
		if m.Role == "system" && !opts.IncludeSystem {
			continue
		}

		if opts.StructuredTools {
			// Structured mode: keep everything as-is (assistant tool_calls,
			// tool role, tool_call_id). No flattening.
			result = append(result, m)
			continue
		}

		// Non-structured mode.
		if m.Role == "tool" {
			if opts.NoToolResults {
				// Compress to a placeholder but keep role:"tool" so format
				// builders can convert to user-role with a prefix.
				toolName := inferToolName(m)
				charCount := len(m.Content)
				result = append(result, api.Message{
					Role:       "tool",
					Content:    fmt.Sprintf("[tool result: %s, %d chars]", toolName, charCount),
					ToolCallID: m.ToolCallID,
				})
			} else {
				result = append(result, m)
			}
			continue
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && opts.NoToolResults {
			text := toolCallsToText(m.ToolCalls, m.Content)
			result = append(result, api.Message{
				Role:    "assistant",
				Content: text,
			})
			continue
		}
		result = append(result, m)
	}

	// Pre-scan all message content for remote usernames (from /home/<user>
	// paths on other machines) so they can be redacted even when they appear
	// without the full path context (e.g. in command output).
	var allContent []string
	for i := range result {
		allContent = append(allContent, result[i].Content)
		for j := range result[i].ToolCalls {
			allContent = append(allContent, result[i].ToolCalls[j].Function.Arguments)
		}
	}
	SetRemoteUsernames(mergeUsernames(remoteUsernamesForRedaction, CollectRemoteUsernames(allContent)))

	// Apply credential redaction to all message content after cleaning.
	for i := range result {
		result[i].Content = RedactContent(result[i].Content)
	}

	// Redact PII in tool call arguments (file paths, usernames, etc.).
	for i := range result {
		for j := range result[i].ToolCalls {
			result[i].ToolCalls[j].Function.Arguments = RedactContent(result[i].ToolCalls[j].Function.Arguments)
		}
	}

	// Skip deduplication in structured mode — structured messages must
	// never be merged.
	if opts.StructuredTools {
		return result
	}
	return deduplicateConsecutive(result)
}

// deduplicateConsecutive merges consecutive messages with the same role.
//
// It guards against merging messages that carry tool markers
// (toolResultMarker or toolCallMarker): a tool-result message converted to
// user-role must never be merged with an adjacent real user message, and a
// tool-call-flattened assistant message must never be merged with an
// adjacent plain assistant message. This preserves conversation flow.
func deduplicateConsecutive(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return nil
	}
	deduped := []api.Message{messages[0]}
	for i := 1; i < len(messages); i++ {
		last := deduped[len(deduped)-1]
		cur := messages[i]
		if cur.Role == last.Role && !hasToolMarker(last) && !hasToolMarker(cur) {
			merged := last
			merged.Content = strings.TrimSpace(merged.Content + "\n\n" + strings.TrimSpace(cur.Content))
			deduped[len(deduped)-1] = merged
		} else {
			deduped = append(deduped, cur)
		}
	}
	return deduped
}

// hasToolMarker reports whether a message's content contains one of the tool
// markers that should prevent deduplication merging.
func hasToolMarker(m api.Message) bool {
	return strings.Contains(m.Content, toolResultMarker) ||
		strings.Contains(m.Content, toolCallMarker) ||
		strings.Contains(m.Content, "[tool result:")
}

// normalizeRole maps internal roles to training-friendly roles.
func normalizeRole(role string) string {
	switch strings.ToLower(role) {
	case "system", "user", "assistant":
		return role
	default:
		return "user"
	}
}

// ---------------------------------------------------------------------------
// Format builders
// ---------------------------------------------------------------------------

func buildShareGPT(states []agent.ConversationState, opts ExportOptions) ([]ShareGPTConversation, error) {
	conversations := make([]ShareGPTConversation, 0, len(states))
	for _, state := range states {
		cleaned := flattenStandardMessages(state.Messages, opts)

		var msgs []ShareGPTMessage
		for _, m := range cleaned {
			msgs = append(msgs, toShareGPTMessage(m, opts))
		}

		if len(msgs) == 0 {
			continue
		}

		conversations = append(conversations, ShareGPTConversation{
			ID:       state.SessionID,
			Messages: msgs,
			Metadata: ShareGPTMetadata{
				SessionID:   state.SessionID,
				SessionName: state.Name,
				Source:      "sprout",
				TotalCost:   state.TotalCost,
				WorkingDir:  RedactContent(state.WorkingDirectory),
			},
		})
	}
	return conversations, nil
}

func buildOpenAI(states []agent.ConversationState, opts ExportOptions) ([]OpenAITrainingExample, error) {
	var examples []OpenAITrainingExample
	for _, state := range states {
		cleaned := flattenStandardMessages(state.Messages, opts)

		var msgs []OpenAIMessage
		for _, m := range cleaned {
			msgs = append(msgs, toOpenAIMessage(m, opts))
		}

		if len(msgs) == 0 {
			continue
		}

		// Each session becomes one multi-turn training example.
		examples = append(examples, OpenAITrainingExample{Messages: msgs})

		// When IncludeSubagents is set, extract single-task examples
		// from run_subagent / run_parallel_subagents tool calls. These
		// are extracted from the raw (unflattened) messages because the
		// flattening step may convert tool messages to user-role text.
		if opts.IncludeSubagents {
			subExs := extractSubagentExamples(state)
			examples = append(examples, subExs...)
		}
	}
	return examples, nil
}

// toShareGPTMessage converts a cleaned api.Message into a ShareGPTMessage.
//
// In structured mode, tool messages keep role:"tool" with their
// tool_call_id, and assistant messages keep their tool_calls arrays.
//
// In non-structured mode, tool messages are converted to user-role messages
// with a "[Tool Result] " prefix so the conversation flow is preserved and
// the model learns to react to tool outputs. Tool messages are never
// dropped.
func toShareGPTMessage(m api.Message, opts ExportOptions) ShareGPTMessage {
	if m.Role == "tool" {
		if opts.StructuredTools {
			// Structured mode: keep role:"tool" and tool_call_id intact.
			return ShareGPTMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			}
		}
		// Non-structured: convert to user-role with prefix.
		return ShareGPTMessage{
			Role:    "user", // tool results become user messages for training
			Content: toolResultMarker + " " + m.Content,
		}
	}
	return ShareGPTMessage{
		Role:       normalizeRole(m.Role),
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}
}

// toOpenAIMessage converts a cleaned api.Message into an OpenAIMessage.
// The conversion logic mirrors toShareGPTMessage — see its docs.
func toOpenAIMessage(m api.Message, opts ExportOptions) OpenAIMessage {
	if m.Role == "tool" {
		if opts.StructuredTools {
			return OpenAIMessage{
				Role:       "tool",
				Content:    m.Content,
				ToolCallID: m.ToolCallID,
			}
		}
		return OpenAIMessage{
			Role:    "user", // tool results become user messages for training
			Content: toolResultMarker + " " + m.Content,
		}
	}
	return OpenAIMessage{
		Role:       normalizeRole(m.Role),
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	}
}

func buildAlpaca(states []agent.ConversationState, opts ExportOptions) ([]AlpacaExample, error) {
	var examples []AlpacaExample
	for _, state := range states {
		cleaned := flattenStandardMessages(state.Messages, opts)

		if len(cleaned) == 0 {
			continue
		}

		example := alpacaFromConversation(cleaned)
		if example != nil {
			examples = append(examples, *example)
		}
	}
	return examples, nil
}

// alpacaFromConversation heuristically converts a cleaned conversation into
// a single Alpaca example:
//   - First user message → instruction
//   - Intermediate conversation context → input
//   - Last assistant message → output
func alpacaFromConversation(messages []api.Message) *AlpacaExample {
	var firstUser, lastAssistant string
	for _, m := range messages {
		if m.Role == "user" && firstUser == "" {
			firstUser = strings.TrimSpace(m.Content)
		}
		if m.Role == "assistant" {
			lastAssistant = strings.TrimSpace(m.Content)
		}
	}

	if firstUser == "" || lastAssistant == "" {
		return nil
	}

	trimmedFirst := firstUser
	// Build the "input" from intermediate conversation context.
	var inputParts []string
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		trimmed := strings.TrimSpace(m.Content)
		if trimmed == trimmedFirst || trimmed == lastAssistant {
			continue
		}
		inputParts = append(inputParts, fmt.Sprintf("%s: %s", m.Role, m.Content))
	}

	return &AlpacaExample{
		Instruction: firstUser,
		Input:       strings.Join(inputParts, "\n"),
		Output:      lastAssistant,
	}
}

// ---------------------------------------------------------------------------
// Subagent example extraction
// ---------------------------------------------------------------------------

// minSubagentOutputLen is the minimum character length for a subagent's
// output to be considered a useful training example. Shorter outputs are
// usually error messages or "insufficient output" placeholders.
const minSubagentOutputLen = 50

// subagentToolNames are the tool-call function names that trigger subagent
// extraction.
var subagentToolNames = map[string]bool{
	"run_subagent":           true,
	"run_parallel_subagents": true,
}

// extractSubagentExamples walks a conversation's messages looking for
// assistant tool calls to run_subagent or run_parallel_subagents. For each
// match, it builds an OpenAI fine-tuning example from the subagent's
// task prompt (user) and output (assistant).
//
// For run_subagent, exactly one example is produced per call.
//
// For run_parallel_subagents, the tool result is a JSON object keyed by
// task ID (task-1, task-2, …); one example is produced per task whose
// output passes the minimum-length filter.
//
// Examples are filtered out when the output is empty or shorter than
// minSubagentOutputLen characters.
func extractSubagentExamples(state agent.ConversationState) []OpenAITrainingExample {
	var examples []OpenAITrainingExample

	// Build a lookup from tool-call ID → tool-result content for quick
	// resolution.
	resultByID := make(map[string]string)
	for _, m := range state.Messages {
		if m.Role == "tool" && m.ToolCallID != "" {
			resultByID[m.ToolCallID] = m.Content
		}
	}

	for _, m := range state.Messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			name := tc.Function.Name
			if !subagentToolNames[name] {
				continue
			}
			result, ok := resultByID[tc.ID]
			if !ok {
				continue
			}
			args := tc.Function.Arguments

			switch name {
			case "run_subagent":
				if ex := subagentExampleFromSingle(args, result); ex != nil {
					examples = append(examples, *ex)
				}
			case "run_parallel_subagents":
				exs := subagentExamplesFromParallel(args, result)
				examples = append(examples, exs...)
			}
		}
	}

	return examples
}

// subagentExampleFromSingle builds one training example from a
// run_subagent tool call + result.
func subagentExampleFromSingle(argsJSON, result string) *OpenAITrainingExample {
	args := parseToolCallArgs(argsJSON)
	prompt := extractStringArg(args, "prompt")
	persona := extractStringArg(args, "persona")

	output := extractSubagentOutput(result)
	if len(strings.TrimSpace(output)) < minSubagentOutputLen {
		return nil
	}

	system := personaToSystemPrompt(persona)

	prompt = RedactContent(prompt)
	output = RedactContent(output)
	system = RedactContent(system)

	return &OpenAITrainingExample{
		Messages: []OpenAIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
			{Role: "assistant", Content: output},
		},
	}
}

// subagentExamplesFromParallel builds one or more training examples from
// a run_parallel_subagents tool call + result. The arguments may contain
// a "subagents", "tasks", or "prompts" array of strings or objects; the
// result is a JSON object keyed by task ID.
func subagentExamplesFromParallel(argsJSON, result string) []OpenAITrainingExample {
	args := parseToolCallArgs(argsJSON)

	// Collect task prompts and IDs in order. The args may use "subagents",
	// "tasks", or "prompts". Each element may be a string (auto-ID) or
	// an object with "id" and "prompt" fields.
	var specs []parallelTaskSpec
	for _, key := range []string{"subagents", "tasks", "prompts"} {
		if raw, ok := args[key]; ok {
			specs = extractTaskSpecs(raw)
			if len(specs) > 0 {
				break
			}
		}
	}

	// Parse the result into a task-ID → output map.
	taskOutputs := parseParallelSubagentResult(result)

	var examples []OpenAITrainingExample
	for _, spec := range specs {
		// Try the task's own ID, then fall back to "task-N", then
		// 0-based index.
		output, ok := taskOutputs[spec.id]
		if !ok {
			output, ok = taskOutputs[fmt.Sprintf("task-%d", len(examples)+1)]
		}
		if !ok {
			continue
		}

		if len(strings.TrimSpace(output)) < minSubagentOutputLen {
			continue
		}

		prompt := RedactContent(spec.prompt)
		output = RedactContent(output)

		examples = append(examples, OpenAITrainingExample{
			Messages: []OpenAIMessage{
				{Role: "system", Content: subagentDefaultSystem},
				{Role: "user", Content: prompt},
				{Role: "assistant", Content: output},
			},
		})
	}

	return examples
}

// ---------------------------------------------------------------------------
// Subagent helper functions
// ---------------------------------------------------------------------------

// subagentDefaultSystem is the fallback system prompt when no persona is
// provided in the tool call arguments.
const subagentDefaultSystem = "You are a helpful coding assistant."

// personaToSystemPrompt converts a persona name into a system prompt. When
// the persona is empty or unknown, a generic default is returned.
func personaToSystemPrompt(persona string) string {
	persona = strings.TrimSpace(strings.ToLower(persona))
	switch persona {
	case "coder", "":
		return subagentDefaultSystem
	default:
		return fmt.Sprintf("You are a %s assistant. Complete the task thoroughly and report your results.", persona)
	}
}

// extractSubagentOutput parses a run_subagent result and extracts the
// subagent's stdout output. The result is a JSON object with a "stdout"
// field. If parsing fails, the raw content is returned as-is.
func extractSubagentOutput(result string) string {
	result = strings.TrimSpace(result)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		return result
	}
	if stdout, ok := m["stdout"].(string); ok {
		return stdout
	}
	return result
}

// parseParallelSubagentResult parses a run_parallel_subagents result into
// a map of task-ID → extracted stdout output. The result is a JSON object
// keyed by task ID, each value being a nested object with a "stdout"
// field. Non-parseable results return an empty map.
func parseParallelSubagentResult(result string) map[string]string {
	result = strings.TrimSpace(result)
	out := make(map[string]string)

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &topLevel); err != nil {
		return out
	}

	for taskID, raw := range topLevel {
		var taskResult map[string]interface{}
		if err := json.Unmarshal(raw, &taskResult); err != nil {
			continue
		}
		if stdout, ok := taskResult["stdout"].(string); ok {
			out[taskID] = stdout
		}
	}

	return out
}

// parseToolCallArgs parses the JSON arguments string from a tool call into
// a map. Returns an empty map on parse failure.
func parseToolCallArgs(argsJSON string) map[string]interface{} {
	args := make(map[string]interface{})
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		return args
	}
	// Best-effort parse — ignore errors.
	_ = json.Unmarshal([]byte(argsJSON), &args)
	return args
}

// extractStringArg safely extracts a string value from a parsed args map.
func extractStringArg(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		// Some providers may send non-string types; marshal to string.
		b, err := json.Marshal(val)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// extractTaskPrompts extracts task prompts from a "subagents"/"tasks"/
// "prompts" array value. Each element may be a simple string or an object
// with a "prompt" field.
func extractTaskPrompts(raw interface{}) []string {
	specs := extractTaskSpecs(raw)
	prompts := make([]string, len(specs))
	for i, s := range specs {
		prompts[i] = s.prompt
	}
	return prompts
}

// parallelTaskSpec holds an ID and prompt for one parallel subagent task.
type parallelTaskSpec struct {
	id     string
	prompt string
}

// extractTaskSpecs extracts task specs (ID + prompt) from a
// "subagents"/"tasks"/"prompts" array value. Each element may be a simple
// string (auto-generated ID "task-N") or an object with "id" and "prompt"
// fields.
func extractTaskSpecs(raw interface{}) []parallelTaskSpec {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var specs []parallelTaskSpec
	for i, item := range arr {
		switch v := item.(type) {
		case string:
			specs = append(specs, parallelTaskSpec{
				id:     fmt.Sprintf("task-%d", i+1),
				prompt: v,
			})
		case map[string]interface{}:
			spec := parallelTaskSpec{
				id: fmt.Sprintf("task-%d", i+1),
			}
			if id, ok := v["id"].(string); ok && id != "" {
				spec.id = id
			}
			if p, ok := v["prompt"].(string); ok {
				spec.prompt = p
			}
			specs = append(specs, spec)
		}
	}
	return specs
}

// ---------------------------------------------------------------------------
// File writers
// ---------------------------------------------------------------------------

// writeJSONArray writes data as a pretty-printed JSON array.
func writeJSONArray(data interface{}, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("failed to write JSON: %w", err)
	}
	return nil
}

// writeJSONL writes one JSON object per line (OpenAI fine-tuning format).
func writeJSONL(examples []OpenAITrainingExample, path string) error {
	items := make([]interface{}, len(examples))
	for i := range examples {
		items[i] = examples[i]
	}
	return writeJSONLGeneric(items, path)
}

// writeJSONLGeneric writes one JSON object per line. It is the shared
// implementation used by writeJSONL and the file-change exporter so that
// the mkdir/create/encode loop is not duplicated.
func writeJSONLGeneric(items []interface{}, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return fmt.Errorf("failed to write JSONL line: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Utility: sort sessions newest-first
// ---------------------------------------------------------------------------

// sortCandidateSessionsNewestFirst sorts candidateSession slices by LastUpdated descending.
func sortCandidateSessionsNewestFirst(candidates []candidateSession) {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Info.LastUpdated.After(candidates[j].Info.LastUpdated)
	})
}

// isExcludedDirectory returns true if dir starts with any of the given
// exclude path prefixes. Path comparison is case-sensitive and uses
// filesystem-aware prefix matching.
func isExcludedDirectory(dir string, excludePaths []string) bool {
	if len(excludePaths) == 0 || dir == "" {
		return false
	}
	for _, prefix := range excludePaths {
		if prefix == "" {
			continue
		}
		// Filesystem-aware prefix: the directory must start with the prefix
		// followed by a path separator or end exactly at the prefix.
		if strings.HasPrefix(dir, prefix) {
			rest := dir[len(prefix):]
			if rest == "" || rest[0] == filepath.Separator {
				return true
			}
		}
	}
	return false
}
