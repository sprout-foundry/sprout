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

// export.go — the training-export core: the export option/result and
// per-format types, the ExportSessions entry point, session loading and
// candidate filtering, and the JSON/JSONL writers. Format builders live in
// export_formatters.go; subagent-example extraction in export_subagent.go.

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
	Role       string         `json:"role"` // "system", "user", "assistant"
	Content    string         `json:"content"`
	ToolCalls  []api.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
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

	// Scan working directories for remote usernames (e.g. /home/deva)
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
