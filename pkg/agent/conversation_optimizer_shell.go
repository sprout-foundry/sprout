package agent

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ShellCommandRecord tracks shell commands to detect redundancy
type ShellCommandRecord struct {
	Command      string
	Output       string
	OutputHash   string
	Timestamp    time.Time
	MessageIndex int
	IsTransient  bool // Commands like ls, find that become less relevant over time
}

// isRedundantShellCommand checks if this message is a redundant shell command
func (co *ConversationOptimizer) isRedundantShellCommand(msg api.Message, index int) bool {
	if msg.Role != "tool" {
		return false
	}

	// Check if this is a shell command result
	if !strings.Contains(msg.Content, "Tool call result for shell_command:") {
		return false
	}

	command := co.extractShellCommand(msg.Content)
	if command == "" {
		return false
	}

	// Check if we have a more recent execution of this command
	if record, exists := co.shellCommands[command]; exists {
		// This is an OLDER execution if there's a newer one
		if index < record.MessageIndex {
			return true // Mark as stale since there's a newer execution
		}
	}

	return false
}

// trackShellCommand records a shell command execution for future optimization
func (co *ConversationOptimizer) trackShellCommand(msg api.Message, index int) {
	if msg.Role != "tool" || !strings.Contains(msg.Content, "Tool call result for shell_command:") {
		return
	}

	command := co.extractShellCommand(msg.Content)
	if command == "" {
		return
	}

	output := co.extractShellOutput(msg.Content)
	hash := co.hashContent(output)
	isTransient := co.isTransientCommand(command)

	co.shellCommands[command] = &ShellCommandRecord{
		Command:      command,
		Output:       output,
		OutputHash:   hash,
		Timestamp:    time.Now(),
		MessageIndex: index,
		IsTransient:  isTransient,
	}
}

// extractShellCommand extracts the shell command from a tool call result message
func (co *ConversationOptimizer) extractShellCommand(content string) string {
	// Pattern: "Tool call result for shell_command: <command>"
	re := regexp.MustCompile(`Tool call result for shell_command:\s*([^\n]+)`)
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractShellOutput extracts the shell command output from a tool call result message
func (co *ConversationOptimizer) extractShellOutput(content string) string {
	// Find the output after the command line
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return ""
	}

	// Skip the first line (tool call result header) and join the rest
	return strings.Join(lines[1:], "\n")
}

// isTransientCommand checks if a command is transient (exploration commands that become stale)
func (co *ConversationOptimizer) isTransientCommand(command string) bool {
	transientPatterns := []string{
		"ls", "find", "grep", "tree", "pwd", "whoami", "date", "ps",
		"df", "du", "which", "whereis", "locate", "file", "stat",
	}

	cmdLower := strings.ToLower(command)
	for _, pattern := range transientPatterns {
		if strings.HasPrefix(cmdLower, pattern+" ") || cmdLower == pattern {
			return true
		}
	}
	return false
}

// createShellCommandSummary creates a summary for a redundant shell command
func (co *ConversationOptimizer) createShellCommandSummary(msg api.Message) string {
	command := co.extractShellCommand(msg.Content)
	output := co.extractShellOutput(msg.Content)

	// Count lines and characters in output
	lines := strings.Split(strings.TrimSpace(output), "\n")
	lineCount := len(lines)
	charCount := len(output)

	// Determine command type
	commandType := "command"
	if co.isTransientCommand(command) {
		commandType = "exploration command"
	}

	return fmt.Sprintf("Tool call result for shell_command: %s\n[STALE] Earlier execution of %s (%d lines output, %d chars) - see latest execution for current state",
		command, commandType, lineCount, charCount)
}

// getTrackedCommands returns list of tracked shell commands
func (co *ConversationOptimizer) getTrackedCommands() []string {
	commands := make([]string, 0, len(co.shellCommands))
	for command := range co.shellCommands {
		commands = append(commands, command)
	}
	return commands
}