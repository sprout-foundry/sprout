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

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
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
}

// ExportResult contains statistics about an export run.
type ExportResult struct {
	SessionsScanned   int `json:"sessions_scanned"`
	SessionsExported  int `json:"sessions_exported"`
	ExamplesGenerated int `json:"examples_generated"`
	SessionsFiltered  int `json:"sessions_filtered"`
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
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
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
	Role    string `json:"role"`
	Content string `json:"content"`
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

	// Filter sessions by quality thresholds.
	var qualified []agent.ConversationState
	for _, c := range candidates {
		state, err := loadSessionState(c)
		if err != nil {
			// Skip sessions that can't be loaded but count them as filtered.
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
func meetsThresholds(state agent.ConversationState, minTurns, minActions int) bool {
	if countTurns(state.Messages) < minTurns {
		return false
	}
	if len(state.TaskActions) < minActions {
		return false
	}
	return true
}

// countTurns counts user-assistant exchange pairs. Tool messages are ignored;
// an assistant message is counted as a turn if the last non-tool message was a
// user message (or if no user message has been consumed yet for this turn).
func countTurns(messages []api.Message) int {
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

// flattenStandardMessages cleans and deduplicates a raw message slice.
//   - Strips system messages unless opts.IncludeSystem.
//   - Replaces tool-result messages with placeholders when opts.NoToolResults.
//   - Converts assistant tool calls to readable text when opts.NoToolResults.
//   - Merges consecutive messages that share the same role.
func flattenStandardMessages(messages []api.Message, opts ExportOptions) []api.Message {
	var result []api.Message
	for _, m := range messages {
		if m.Role == "system" && !opts.IncludeSystem {
			continue
		}
		if m.Role == "tool" {
			if opts.NoToolResults {
				toolName := inferToolName(m)
				charCount := len(m.Content)
				result = append(result, api.Message{
					Role:    "tool",
					Content: fmt.Sprintf("[tool result: %s, %d chars]", toolName, charCount),
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
	return deduplicateConsecutive(result)
}

// deduplicateConsecutive merges consecutive messages with the same role.
func deduplicateConsecutive(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return nil
	}
	deduped := []api.Message{messages[0]}
	for i := 1; i < len(messages); i++ {
		if messages[i].Role == deduped[len(deduped)-1].Role {
			merged := deduped[len(deduped)-1]
			merged.Content = strings.TrimSpace(merged.Content + "\n\n" + strings.TrimSpace(messages[i].Content))
			deduped[len(deduped)-1] = merged
		} else {
			deduped = append(deduped, messages[i])
		}
	}
	return deduped
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
			// Skip tool messages in ShareGPT — they're noise for training.
			if m.Role == "tool" {
				continue
			}
			msgs = append(msgs, ShareGPTMessage{
				Role:    normalizeRole(m.Role),
				Content: m.Content,
			})
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
				Source:      "ledit",
				TotalCost:   state.TotalCost,
				WorkingDir:  state.WorkingDirectory,
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
			if m.Role == "tool" {
				continue
			}
			msgs = append(msgs, OpenAIMessage{
				Role:    normalizeRole(m.Role),
				Content: m.Content,
			})
		}

		if len(msgs) == 0 {
			continue
		}

		// Each session becomes one multi-turn training example.
		examples = append(examples, OpenAITrainingExample{Messages: msgs})
	}
	return examples, nil
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, item := range examples {
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
