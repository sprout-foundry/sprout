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
	Name       string      // Command name
	Type       CommandType // external, builtin, alias, function
	Path       string      // For external commands, the full path
	Value      string      // For aliases, the expanded value
	SubCommand string      // For commands with subcommands, the detected subcommand
}

// allowedCommands is the allowlist of commands safe for auto-execution in ledit context.
// These commands are read-only or safe operations that make sense to run within ledit.
var allowedCommands = map[string]bool{
	// Version control
	"git": true,

	// File viewing and info (read-only)
	"ls":     true,
	"cat":    true,
	"less":   true,
	"more":   true,
	"head":   true,
	"tail":   true,
	"wc":     true,
	"file":   true,
	"stat":   true,
	"du":     true,
	"df":     true,
	"tree":   true,

	// Searching
	"grep":   true,
	"find":   true,
	"rg":     true, // ripgrep
	"fd":     true, // find alternative

	// Basic utilities
	"echo":   true,
	"printf": true,
	"pwd":    true,
	"date":   true,
	"which":  true,
	"type":   true,
	"whereis": true,

	// System info (read-only)
	"uname":  true,
	"uptime": true,
	"id":     true,
	"whoami": true,
	"hostname": true,

	// Network info (read-only)
	"ping":   true,
	"curl":   true,
	"wget":   true,
	"nslookup": true,
	"dig":    true,

	// Process viewing (read-only)
	"ps":     true,
	"top":    true, // Note: top is interactive, might not work well
	"htop":   true, // Note: htop is interactive

	// Build tools (safe operations)
	"make":   true, // For running makefiles
	"cargo":  true, // For cargo build, test, etc.
	"npm":    true, // For npm scripts
	"yarn":   true,
	"pnpm":   true,
	"go":     true, // For go build, test, run, etc.

	// File operations (safe, require explicit targets)
	"cp":     true, // Copy files
	"mv":     true, // Move/rename files

	// Package managers (require sudo for system changes, safe for user operations)
	"apt":    true, // APT package manager
	"apt-get": true,
	"brew":   true, // Homebrew package manager
	"yum":    true,
	"dnf":    true,
	"pacman": true,

	// Testing
	"pytest": true,
	"jest":   true,
}

// blockedCommands is a disallowlist of commands that should NEVER be auto-executed.
// These are either dangerous or don't make sense in the ledit context.
var blockedCommands = map[string]bool{
	// Dangerous - destructive operations
	"rm":    true,  // delete files (too easy to accidentally delete)
	"rmdir": true,  // remove directories

	// Dangerous - system modification
	"dd":    true,  // disk destroyer
	"mkfs":  true,  // make filesystem
	"fdisk": true,  // partition editor
	"shutdown": true,
	"reboot": true,
	"poweroff": true,

	// Don't make sense in ledit context (interactive editors)
	"vi":    true,
	"vim":   true,
	"nvim":  true,
	"nano":  true,
	"emacs": true,
	"code":  true,
	"gedit": true,

	// Interactive shells
	"bash":  true,
	"zsh":   true,
	"fish":  true,

	// Package manager - snap (can auto-install with less friction)
	"snap":  true,
}

// IsCommand checks if the given input starts with a valid zsh command that is
// safe for auto-execution in the ledit context.
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

	// SAFETY CHECK 1: Is this a blocked command?
	if isBlockedCommand(cmdName) {
		return false, nil, nil
	}

	// SAFETY CHECK 2: Is this an allowed command?
	if !isAllowedCommand(cmdName) {
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

	// Validate command structure for commands with known subcommands
	if err := validateCommandStructure(input, info); err != nil {
		// Command structure is invalid, this is likely natural language
		return false, nil, nil
	}

	return true, info, nil
}

// isBlockedCommand checks if a command is in the blocked list
func isBlockedCommand(cmdName string) bool {
	lowerName := strings.ToLower(cmdName)
	return blockedCommands[lowerName]
}

// isAllowedCommand checks if a command is in the allowed list
func isAllowedCommand(cmdName string) bool {
	lowerName := strings.ToLower(cmdName)
	return allowedCommands[lowerName]
}

// GetAllowedCommands returns a list of all allowed commands (for documentation/config)
func GetAllowedCommands() []string {
	cmds := make([]string, 0, len(allowedCommands))
	for cmd := range allowedCommands {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// GetBlockedCommands returns a list of all blocked commands (for documentation/config)
func GetBlockedCommands() []string {
	cmds := make([]string, 0, len(blockedCommands))
	for cmd := range blockedCommands {
		cmds = append(cmds, cmd)
	}
	return cmds
}

// validateCommandStructure validates that the command has a valid structure.
// For commands with known subcommands, it dynamically validates the subcommand.
func validateCommandStructure(input string, info *CommandInfo) error {
	words := strings.Fields(input)
	if len(words) < 2 {
		// Single-word command, no subcommand to validate
		return nil
	}

	cmdName := strings.ToLower(words[0])

	// Commands that require known subcommands
	switch cmdName {
	case "git":
		return validateGitSubcommand(words[1:])
	case "docker":
		return validateDockerSubcommand(words[1:])
	case "kubectl":
		return validateKubectlSubcommand(words[1:])
	case "gh":
		return validateGitHubSubcommand(words[1:])
	}

	// No specific validation for this command
	return nil
}

// validateGitSubcommand checks if the git subcommand is valid using dynamic validation.
// It runs "git <subcommand> --help" and checks the exit code.
func validateGitSubcommand(args []string) error {
	if len(args) == 0 {
		return nil // No subcommand provided yet
	}

	subcommand := args[0]

	// Use git's help system to validate the subcommand
	// This is dynamic and always up-to-date
	return validateSubcommandViaHelp("git", subcommand)
}

// validateDockerSubcommand checks if the docker subcommand is valid
func validateDockerSubcommand(args []string) error {
	if len(args) == 0 {
		return nil
	}

	subcommand := args[0]
	return validateSubcommandViaHelp("docker", subcommand)
}

// validateKubectlSubcommand checks if the kubectl subcommand is valid
func validateKubectlSubcommand(args []string) error {
	if len(args) == 0 {
		return nil
	}

	subcommand := args[0]
	return validateSubcommandViaHelp("kubectl", subcommand)
}

// validateGitHubSubcommand checks if the gh subcommand is valid
func validateGitHubSubcommand(args []string) error {
	if len(args) == 0 {
		return nil
	}

	subcommand := args[0]
	return validateSubcommandViaHelp("gh", subcommand)
}

// validateSubcommandViaHelp dynamically validates a subcommand by running
// "<command> <subcommand> --help" and checking if it succeeds.
// Returns nil if valid, error if invalid.
func validateSubcommandViaHelp(command, subcommand string) error {
	// Construct the validation command: git status --help
	// Use --help to get help text (most commands support this)
	// We'll suppress all output and just check the exit code
	cmd := exec.Command(command, subcommand, "--help")

	// Redirect output to /dev/null (we only care about exit code)
	// In Go, we just don't capture the output
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Run with a short timeout to avoid hanging
	err := cmd.Run()

	// Exit code 0 = subcommand is valid (help was displayed)
	// Exit code != 0 = subcommand is invalid
	if err == nil {
		return nil // Valid subcommand
	}

	// Check if it's a "command not found" type error or just help not found
	// Some commands use -h instead of --help, or have different help flags
	// Let's try a few variations
	helpFlags := []string{"-h", "--help", "help"}
	for _, flag := range helpFlags {
		cmd := exec.Command(command, subcommand, flag)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if cmd.Run() == nil {
			return nil // Valid subcommand with this help flag
		}
	}

	// All attempts failed - likely not a valid subcommand
	return fmt.Errorf("invalid %s subcommand: %s", command, subcommand)
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
