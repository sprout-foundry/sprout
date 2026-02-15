// Agent execution utilities: command execution, formatting, and helper functions
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"golang.org/x/term"
)

// ExecuteCommand runs a shell command and streams its output in real-time.
// Returns the combined output (for error messages) and any error that occurred.
func ExecuteCommand(cmd string) (string, error) {
	// Enhance command to force colors for git and other tools
	enhancedCmd := enhanceCommandForColors(cmd)

	// Run command through bash -c with color support
	command := exec.Command("bash", "-c", enhancedCmd)

	// Explicitly set working directory to current directory
	if wd, err := os.Getwd(); err == nil {
		command.Dir = wd
	}

	// Set environment to force color output
	command.Env = append(os.Environ(),
		"FORCE_COLOR=1",
		"TERM=xterm-256color",
		"CLICOLOR_FORCE=1",
		"NO_COLOR=", // Unset NO_COLOR if it exists
	)

	// Create pipes for stdout and stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Buffer to capture output for error messages
	var outputBuf bytes.Buffer

	// Start the command
	if err := command.Start(); err != nil {
		return "", err
	}

	// Stream stdout and stderr in real-time
	// Use goroutines to handle both concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy stdout to both terminal and buffer
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stdout, &outputBuf), stdout)
	}()

	// Copy stderr to both terminal and buffer
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stderr, &outputBuf), stderr)
	}()

	// Wait for both streams to finish
	wg.Wait()

	// Wait for command to complete
	if err := command.Wait(); err != nil {
		return outputBuf.String(), err
	}

	return outputBuf.String(), nil
}

// enhanceCommandForColors modifies commands to force color output
func enhanceCommandForColors(cmd string) string {
	trimmed := strings.TrimSpace(cmd)

	// For git commands, inject -c color.ui=always after "git"
	if strings.HasPrefix(trimmed, "git ") {
		// Split "git" and the rest
		parts := strings.SplitN(trimmed, " ", 2)
		if len(parts) == 2 {
			subcommand := strings.TrimSpace(parts[1])
			// Inject color config before the subcommand
			return fmt.Sprintf("git -c color.ui=always %s", subcommand)
		}
	}

	// For ls, ensure --color=auto is present (works with FORCE_COLOR)
	if strings.HasPrefix(trimmed, "ls ") {
		if !strings.Contains(trimmed, "--color") {
			return strings.Replace(trimmed, "ls", "ls --color=auto", 1)
		}
	}

	// For grep, ensure --color=auto is present
	if strings.HasPrefix(trimmed, "grep ") {
		if !strings.Contains(trimmed, "--color") {
			return strings.Replace(trimmed, "grep", "grep --color=auto", 1)
		}
	}

	return cmd
}

// GetCompletions provides tab completion for commands and files
func GetCompletions(input string, chatAgent *agent.Agent) []string {
	var completions []string

	// Get current word for completion
	words := strings.Fields(input)
	if len(words) == 0 {
		return completions
	}

	currentWord := words[len(words)-1]

	// If it starts with '/', complete slash commands
	if strings.HasPrefix(currentWord, "/") {
		registry := agent_commands.NewCommandRegistry()
		commands := registry.ListCommands()
		for _, cmd := range commands {
			if strings.HasPrefix(cmd.Name(), currentWord[1:]) {
				completions = append(completions, "/"+cmd.Name())
			}
		}
	} else {
		// File path completion
		if strings.Contains(currentWord, "/") || len(words) == 1 {
			// Simple file completion
			matches, _ := filepath.Glob(currentWord + "*")
			for _, match := range matches {
				if info, err := os.Stat(match); err == nil {
					if info.IsDir() {
						match += "/"
					}
					completions = append(completions, match)
				}
			}
		}
	}

	return completions
}

// IsCI checks if running in CI environment
func IsCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}

// FormatDuration formats duration in human readable format
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// GetTerminalWidth attempts to get the terminal width for separators
// Returns a conservative width to avoid wrapping
func GetTerminalWidth() int {
	// Try using golang.org/x/term to get terminal width
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err == nil && width > 0 {
		// Subtract 2 to be conservative and avoid wrapping
		width = width - 2

		// Cap at reasonable limits
		if width > 200 {
			return 200
		}
		if width < 40 {
			return 40
		}
		return width
	}

	// Fallback to a safe default that works in most terminals
	return 78
}
