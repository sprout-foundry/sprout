package zsh

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

// CommandType represents the type of zsh command
type CommandType string

const (
	CommandTypeExternal CommandType = "external"
	CommandTypeBuiltin  CommandType = "builtin"
	CommandTypeAlias    CommandType = "alias"
	CommandTypeFunction CommandType = "function"
)

// CommandInfo contains information about a detected command
type CommandInfo struct {
	Name  string      // Command name
	Type  CommandType // external, builtin, alias, function
	Path  string      // For external commands, the full path
	Value string      // For aliases, the expanded value
}

// IsCommand checks if the given input starts with a valid zsh command.
// It extracts the first word and checks if it exists in zsh's command tables.
// Returns (isCommand, commandInfo, error).
func IsCommand(input string) (bool, *CommandInfo, error) {
	// Extract the first word (command name)
	cmdName := extractFirstWord(input)
	if cmdName == "" {
		return false, nil, nil
	}

	// Check if we're running in zsh
	if !isZshShell() {
		return false, nil, nil
	}

	// Query zsh for command information
	info, err := queryZshCommand(cmdName)
	if err != nil {
		return false, nil, fmt.Errorf("failed to query zsh: %w", err)
	}

	if info == nil {
		return false, nil, nil
	}

	return true, info, nil
}

// extractFirstWord extracts the first word from the input string.
// It respects quoted strings and handles basic shell word splitting.
func extractFirstWord(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	var buf bytes.Buffer
	inQuotes := false
	inDoubleQuotes := false

	for _, r := range input {
		if r == '\'' && !inDoubleQuotes {
			inQuotes = !inQuotes
			continue
		}
		if r == '"' && !inQuotes {
			inDoubleQuotes = !inDoubleQuotes
			continue
		}

		// If we hit a space outside of quotes, we're done
		if unicode.IsSpace(r) && !inQuotes && !inDoubleQuotes {
			break
		}

		// Append the character
		buf.WriteRune(r)
	}

	return buf.String()
}

// isZshShell checks if the current shell is zsh
func isZshShell() bool {
	shell := os.Getenv("SHELL")
	return strings.Contains(shell, "zsh")
}

// queryZshCommand spawns a zsh process to check if a command exists
// and returns information about it
func queryZshCommand(cmdName string) (*CommandInfo, error) {
	// Create a zsh script that checks for the command
	script := fmt.Sprintf(`
# Check in order: external commands, builtins, aliases, functions
if [[ -n ${commands[%s]} ]]; then
    echo "external|${commands[%s]}"
elif [[ -n ${builtins[%s]} ]]; then
    echo "builtin|%s"
elif [[ -n ${aliases[%s]} ]]; then
    echo "alias|${aliases[%s]}"
elif [[ -n ${functions[%s]} ]]; then
    echo "function|%s"
else
    echo "notfound"
fi
`, cmdName, cmdName, cmdName, cmdName, cmdName, cmdName, cmdName, cmdName)

	// Execute the script in zsh
	cmd := exec.Command("zsh", "-c", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("zsh execution failed: %w (stderr: %s)", err, stderr.String())
	}

	// Parse the output
	output := strings.TrimSpace(stdout.String())
	if output == "" || output == "notfound" {
		return nil, nil
	}

	parts := strings.SplitN(output, "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected zsh output format: %s", output)
	}

	cmdType := parts[0]
	cmdValue := parts[1]

	info := &CommandInfo{
		Name: cmdName,
	}

	switch cmdType {
	case "external":
		info.Type = CommandTypeExternal
		info.Path = cmdValue
	case "builtin":
		info.Type = CommandTypeBuiltin
	case "alias":
		info.Type = CommandTypeAlias
		info.Value = cmdValue
	case "function":
		info.Type = CommandTypeFunction
	default:
		return nil, fmt.Errorf("unknown command type: %s", cmdType)
	}

	return info, nil
}
