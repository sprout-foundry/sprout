package core

import (
	"fmt"
	"sort"
	"strings"
)

// TurnCheckpoint captures a summary of a completed conversation turn.
// It records the message range consumed by the turn and a compact summary
// that can replace the original messages during context compaction.
type TurnCheckpoint struct {
	// StartIndex is the index of the first message in the turn (the user query).
	StartIndex int `json:"start_index"`

	// EndIndex is the index of the last message in the turn (the final assistant response).
	EndIndex int `json:"end_index"`

	// UserMessage is the original user query that started the turn, truncated
	// to 2000 characters. This is the best target for embedding-based search
	// to find "what did the user ask that led to this outcome?"
	UserMessage string `json:"user_message"`

	// Summary is a concise description of what happened in the turn.
	Summary string `json:"summary"`

	// ActionableSummary is a bullet-list of accomplishments with file paths,
	// commands run, and other concrete details useful for continued context.
	ActionableSummary string `json:"actionable_summary"`

	// FileChanges is the structured manifest of files this turn modified,
	// suitable for git-style display (M / A / D / R). Populated by the
	// consumer via the OnCheckpoint hook or by building checkpoints directly
	// rather than seed's built-in TurnSummaryBuilder. Optional — empty when
	// the consumer doesn't track changes.
	FileChanges []FileChange `json:"file_changes,omitempty"`

	// RevisionID is the consumer's pointer to the persisted change set for
	// this turn (e.g., a change-tracker revision ID). When set, the summary
	// text can include it so the model knows it can call the history-viewing
	// tool to recover the exact diff. Optional.
	RevisionID string `json:"revision_id,omitempty"`

	// ArchiveLine is a single-line, commit-title-style condensation of this
	// checkpoint, used when meta-compaction has rolled this turn up into the
	// session archive (typically when the session accumulates >100
	// checkpoints). When non-empty, BuildCheckpointCompactedMessages
	// prefers this over Summary/ActionableSummary — it's the tightest form.
	ArchiveLine string `json:"archive_line,omitempty"`
}

// FileChange is the git-style change record for a single file touched in a
// turn. Op uses one of "A" (added), "M" (modified), "D" (deleted), "R"
// (renamed) to mirror git's status codes — anything else is "?" (other).
// Path is workspace-relative.
type FileChange struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

// TurnSummaryBuilder builds a TurnCheckpoint from a slice of messages
// representing a single conversation turn. It extracts the user question,
// tool calls, errors, files modified, and final status to produce both
// a narrative summary and an actionable bullet list.
type TurnSummaryBuilder struct {
	// KnownFileTools is a set of tool names that operate on files.
	// If nil, the default set is used.
	KnownFileTools map[string]bool

	// KnownShellTools is a set of tool names that execute shell commands.
	// If nil, the default set is used.
	KnownShellTools map[string]bool

	// KnownErrorPatterns are substrings that indicate a tool result is an error.
	// If nil, the default set is used.
	KnownErrorPatterns []string
}

// defaultFileTools is the set of tool names that operate on files.
var defaultFileTools = map[string]bool{
	"read_file":        true,
	"write_file":       true,
	"edit_file":        true,
	"create_file":      true,
	"delete_file":      true,
	"list_files":       true,
	"search_files":     true,
	"glob_files":       true,
	"file_search":      true,
	"grep":             true,
	"patch_file":       true,
	"append_file":      true,
	"read_structured":  true,
	"write_structured": true,
	"patch_structured": true,
}

// defaultShellTools is the set of tool names that execute shell commands.
var defaultShellTools = map[string]bool{
	"shell":       true,
	"execute":     true,
	"run_command": true,
	"bash":        true,
	"sh":          true,
	"cmd":         true,
}

// defaultErrorPatterns are substrings that indicate a tool result is an error.
// All matching is case-insensitive, so only lowercase patterns are needed.
var defaultErrorPatterns = []string{
	"error:",
	"failed",
	"permission denied",
	"not found",
	"does not exist",
	"no such file",
	"timeout",
	"refused",
	"denied",
}

// NewTurnSummaryBuilder creates a new builder with default configuration.
func NewTurnSummaryBuilder() *TurnSummaryBuilder {
	return &TurnSummaryBuilder{}
}

// fileTools returns the set of known file tools, using defaults if not configured.
func (b *TurnSummaryBuilder) fileTools() map[string]bool {
	if b.KnownFileTools != nil {
		return b.KnownFileTools
	}
	return defaultFileTools
}

// shellTools returns the set of known shell tools, using defaults if not configured.
func (b *TurnSummaryBuilder) shellTools() map[string]bool {
	if b.KnownShellTools != nil {
		return b.KnownShellTools
	}
	return defaultShellTools
}

// errorPatterns returns the list of error patterns, using defaults if not configured.
func (b *TurnSummaryBuilder) errorPatterns() []string {
	if b.KnownErrorPatterns != nil {
		return b.KnownErrorPatterns
	}
	return defaultErrorPatterns
}

// Build constructs a TurnCheckpoint from the given messages.
// The messages should represent a single turn: starting with a user query,
// followed by any number of assistant/tool-call/tool-result cycles,
// and ending with the final assistant response.
// Returns a checkpoint with StartIndex=0 and EndIndex=len(messages)-1
// since the caller is responsible for setting the actual indices in state.
func (b *TurnSummaryBuilder) Build(messages []Message) TurnCheckpoint {
	extracted := b.extractTurnData(messages)

	summary := b.buildSummary(extracted)
	actionable := b.buildActionableSummary(extracted)

	return TurnCheckpoint{
		StartIndex:        0,
		EndIndex:          len(messages) - 1,
		UserMessage:       truncateString(extracted.userQuestion, 2000),
		Summary:           summary,
		ActionableSummary: actionable,
	}
}

// turnData holds the extracted information from a turn.
type turnData struct {
	userQuestion  string
	toolCalls     []toolCallInfo
	errors        []string
	filesRead     []string
	filesModified []string
	shellCommands []string
	finalResponse string
	status        turnStatus
}

// toolCallInfo captures details about a single tool call.
type toolCallInfo struct {
	name      string
	arguments string
	result    string
	isError   bool
}

// turnStatus represents the outcome of a conversation turn.
type turnStatus int

const (
	statusCompleted turnStatus = iota
	statusInterrupted
	statusError
	statusPartial
)

func (s turnStatus) String() string {
	switch s {
	case statusCompleted:
		return "completed"
	case statusInterrupted:
		return "interrupted"
	case statusError:
		return "error"
	case statusPartial:
		return "partial"
	default:
		return "unknown"
	}
}

// extractTurnData parses messages and extracts structured information.
func (b *TurnSummaryBuilder) extractTurnData(messages []Message) turnData {
	var data turnData

	fileTools := b.fileTools()
	shellTools := b.shellTools()
	errorPatterns := b.errorPatterns()

	seenFilesRead := make(map[string]bool)
	seenFilesModified := make(map[string]bool)

	for i, msg := range messages {
		switch msg.Role {
		case "user":
			if data.userQuestion == "" {
				data.userQuestion = truncateString(msg.Content, 300)
			}

		case "assistant":
			// Track tool calls
			for _, tc := range msg.ToolCalls {
				info := toolCallInfo{
					name:      tc.Function.Name,
					arguments: tc.Function.Arguments,
				}

				// Extract file paths from arguments
				if fileTools[tc.Function.Name] {
					if path := extractFilePath(tc.Function.Arguments); path != "" {
						if isFileWriteTool(tc.Function.Name) {
							if !seenFilesModified[path] {
								seenFilesModified[path] = true
								data.filesModified = append(data.filesModified, path)
							}
						} else {
							if !seenFilesRead[path] {
								seenFilesRead[path] = true
								data.filesRead = append(data.filesRead, path)
							}
						}
					}
				}

				// Track shell commands
				if shellTools[tc.Function.Name] {
					if cmd := extractShellCommand(tc.Function.Arguments); cmd != "" {
						data.shellCommands = append(data.shellCommands, cmd)
					}
				}

				data.toolCalls = append(data.toolCalls, info)
			}

			// Last assistant message is the final response
			if i == len(messages)-1 || msg.Content != "" {
				if msg.Content != "" {
					data.finalResponse = msg.Content
				}
			}

		case "tool":
			// Match tool result to the most recent unmatched tool call by scanning
			// backwards from the end. Tool results arrive in order, so the last
			// unmatched call is the correct one.
			if len(data.toolCalls) > 0 {
				lastIdx := len(data.toolCalls) - 1
				for lastIdx >= 0 && data.toolCalls[lastIdx].result != "" {
					lastIdx--
				}
				if lastIdx < 0 {
					// All tool calls already matched; skip orphaned result
					continue
				}
				content := strings.TrimSpace(msg.Content)

				// Check for errors
				isError := false
				for _, pattern := range errorPatterns {
					if strings.Contains(strings.ToLower(msg.Content), strings.ToLower(pattern)) {
						isError = true
						break
					}
				}

				if isError && data.toolCalls[lastIdx].result == "" {
					data.errors = append(data.errors, truncateString(msg.Content, 200))
				}

				data.toolCalls[lastIdx].result = content
				data.toolCalls[lastIdx].isError = isError
			}
		}
	}

	// Determine status
	data.status = b.determineStatus(data)

	return data
}

// determineStatus assesses the outcome of the turn.
func (b *TurnSummaryBuilder) determineStatus(data turnData) turnStatus {
	if len(data.errors) > 0 && data.finalResponse == "" {
		return statusError
	}

	// Check for truncation indicators in final response
	if data.finalResponse != "" {
		if strings.HasSuffix(data.finalResponse, "...") {
			return statusPartial
		}
		// Check for abrupt endings
		lastChar := len(data.finalResponse)
		if lastChar > 0 {
			lastByte := data.finalResponse[lastChar-1]
			if lastByte == ',' || lastByte == '(' || lastByte == '[' {
				return statusPartial
			}
		}
	}

	if data.finalResponse == "" && len(data.toolCalls) > 0 {
		return statusInterrupted
	}

	return statusCompleted
}

// buildSummary creates a narrative summary of the turn.
func (b *TurnSummaryBuilder) buildSummary(data turnData) string {
	var parts []string

	// Start with what was asked
	if data.userQuestion != "" {
		parts = append(parts, fmt.Sprintf("User asked: %s", data.userQuestion))
	}

	// Describe tool usage
	if len(data.toolCalls) > 0 {
		toolNames := make(map[string]int)
		for _, tc := range data.toolCalls {
			toolNames[tc.name]++
		}

		// Sort tool names for deterministic output.
		sortedNames := make([]string, 0, len(toolNames))
		for name := range toolNames {
			sortedNames = append(sortedNames, name)
		}
		sort.Strings(sortedNames)

		var toolDesc []string
		for _, name := range sortedNames {
			count := toolNames[name]
			if count > 1 {
				toolDesc = append(toolDesc, fmt.Sprintf("%s (%dx)", name, count))
			} else {
				toolDesc = append(toolDesc, name)
			}
		}
		parts = append(parts, fmt.Sprintf("Used tools: %s", strings.Join(toolDesc, ", ")))
	}

	// Describe file operations
	if len(data.filesRead) > 0 {
		files := uniqueStrings(data.filesRead)
		if len(files) <= 3 {
			parts = append(parts, fmt.Sprintf("Read files: %s", strings.Join(files, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("Read %d files including %s", len(files), strings.Join(files[:3], ", ")))
		}
	}

	if len(data.filesModified) > 0 {
		files := uniqueStrings(data.filesModified)
		if len(files) <= 3 {
			parts = append(parts, fmt.Sprintf("Modified files: %s", strings.Join(files, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("Modified %d files including %s", len(files), strings.Join(files[:3], ", ")))
		}
	}

	// Describe shell commands
	if len(data.shellCommands) > 0 {
		cmds := uniqueStrings(data.shellCommands)
		if len(cmds) <= 3 {
			parts = append(parts, fmt.Sprintf("Ran commands: %s", strings.Join(cmds, ", ")))
		} else {
			parts = append(parts, fmt.Sprintf("Ran %d commands including %s", len(cmds), strings.Join(cmds[:3], ", ")))
		}
	}

	// Describe errors
	if len(data.errors) > 0 {
		parts = append(parts, fmt.Sprintf("Encountered %d error(s)", len(data.errors)))
	}

	// Final status
	if data.finalResponse != "" {
		responseSummary := truncateString(strings.TrimSpace(data.finalResponse), 250)
		parts = append(parts, fmt.Sprintf("Response: %s", responseSummary))
		if data.status != statusCompleted {
			parts = append(parts, fmt.Sprintf("Status: %s", data.status))
		}
	} else {
		parts = append(parts, fmt.Sprintf("Status: %s", data.status))
	}

	return strings.Join(parts, ". ") + "."
}

// buildActionableSummary creates a bullet-list of accomplishments.
func (b *TurnSummaryBuilder) buildActionableSummary(data turnData) string {
	var bullets []string

	if data.userQuestion != "" {
		bullets = append(bullets, fmt.Sprintf("- Question: %s", data.userQuestion))
	}

	if len(data.filesRead) > 0 {
		files := uniqueStrings(data.filesRead)
		for _, f := range files {
			bullets = append(bullets, fmt.Sprintf("- Read: %s", f))
		}
	}

	if len(data.filesModified) > 0 {
		files := uniqueStrings(data.filesModified)
		for _, f := range files {
			bullets = append(bullets, fmt.Sprintf("- Modified: %s", f))
		}
	}

	if len(data.shellCommands) > 0 {
		cmds := uniqueStrings(data.shellCommands)
		for _, cmd := range cmds {
			bullets = append(bullets, fmt.Sprintf("- Command: %s", cmd))
		}
	}

	if len(data.errors) > 0 {
		for _, err := range data.errors {
			bullets = append(bullets, fmt.Sprintf("- Error: %s", err))
		}
	}

	if data.finalResponse != "" {
		responseSummary := truncateString(strings.TrimSpace(data.finalResponse), 300)
		bullets = append(bullets, fmt.Sprintf("- Result: %s", responseSummary))
	}

	if len(bullets) == 0 {
		return fmt.Sprintf("Turn completed with status: %s", data.status)
	}

	return strings.Join(bullets, "\n")
}

// extractFilePath extracts a file path from tool arguments.
// It tries keys in order of specificity to avoid substring collisions
// (e.g., "file" matching inside "filename").
func extractFilePath(args string) string {
	// Try most specific keys first to avoid substring collisions.
	for _, key := range []string{"filename", "path", "file"} {
		if val := extractJSONKey(args, key); val != "" {
			return val
		}
	}
	return ""
}

// extractShellCommand extracts a shell command from tool arguments.
// It tries keys in order of specificity to avoid substring collisions
// (e.g., "cmd" matching inside "command").
func extractShellCommand(args string) string {
	for _, key := range []string{"command", "cmd"} {
		if val := extractJSONKey(args, key); val != "" {
			return val
		}
	}
	return ""
}

// extractJSONKey extracts a string value for the given key from a JSON-like
// argument string. It looks for "key": and extracts the quoted string value.
// The key must be matched as a whole JSON key (surrounded by quotes).
func extractJSONKey(args, key string) string {
	target := `"` + key + `"`
	idx := strings.Index(args, target)
	if idx < 0 {
		return ""
	}
	// Verify this is a JSON key: the character after the closing quote must
	// be a colon (possibly with whitespace).
	afterKey := strings.TrimSpace(args[idx+len(target):])
	if len(afterKey) == 0 || afterKey[0] != ':' {
		return ""
	}
	return extractJSONStringValue(afterKey)
}

// extractJSONStringValue extracts a string value from a JSON value position.
// Input should start from the colon (e.g., `: "..."`).
func extractJSONStringValue(s string) string {
	// Find the colon
	colonIdx := strings.Index(s, ":")
	if colonIdx < 0 {
		return ""
	}

	rest := strings.TrimSpace(s[colonIdx+1:])
	if len(rest) == 0 {
		return ""
	}

	// Check if it starts with a quote
	if rest[0] != '"' {
		return ""
	}

	// Find the closing quote, handling escapes
	rest = rest[1:] // skip opening quote
	var result strings.Builder
	for i := 0; i < len(rest); i++ {
		if rest[i] == '\\' && i+1 < len(rest) {
			result.WriteByte(rest[i+1])
			i++
		} else if rest[i] == '"' {
			break
		} else {
			result.WriteByte(rest[i])
		}
	}

	return result.String()
}

// defaultWriteTools is the set of tool names that modify files.
var defaultWriteTools = map[string]bool{
	"write_file":       true,
	"edit_file":        true,
	"create_file":      true,
	"delete_file":      true,
	"patch_file":       true,
	"append_file":      true,
	"write_structured": true,
	"patch_structured": true,
}

// isFileWriteTool returns true if the tool name indicates a file modification.
func isFileWriteTool(name string) bool {
	return defaultWriteTools[name]
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Truncate at word boundary if possible
	truncated := s[:maxLen]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace >= maxLen/2 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "..."
}

// uniqueStrings removes duplicate strings while preserving order.
func uniqueStrings(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// BuildCheckpointSummary is a convenience function that creates a checkpoint
// summary from messages without requiring a builder instance.
func BuildCheckpointSummary(messages []Message) TurnCheckpoint {
	builder := NewTurnSummaryBuilder()
	return builder.Build(messages)
}
