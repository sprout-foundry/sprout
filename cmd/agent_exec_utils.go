//go:build !js

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

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"golang.org/x/term"
)

// lockingWriter wraps a bytes.Buffer with a mutex for safe concurrent writes.
type lockingWriter struct {
	buf *bytes.Buffer
	mu  *sync.Mutex
}

func (w lockingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

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

	// SP-048-4b: Inherit the user's color env vars. Previously this code
	// forced FORCE_COLOR=1, CLICOLOR_FORCE=1 and unset NO_COLOR so that
	// piped subcommands always emitted ANSI. That made sprout hostile to
	// no-color.org-aware users and broke CI logs that strip-escape the
	// output. We now defer to the parent env: if the user invoked sprout
	// with NO_COLOR=1, the subcommand sees NO_COLOR=1 too; if they want
	// forced colors, they set FORCE_COLOR before launching sprout.
	command.Env = append(os.Environ(),
		"TERM=xterm-256color",
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
	var outputMu sync.Mutex

	// Start the command
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	// Stream stdout and stderr in real-time
	// Use goroutines to handle both concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	// Copy stdout to both terminal and buffer
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stdout, lockingWriter{&outputBuf, &outputMu}), stdout)
	}()

	// Copy stderr to both terminal and buffer
	go func() {
		defer wg.Done()
		io.Copy(io.MultiWriter(os.Stderr, lockingWriter{&outputBuf, &outputMu}), stderr)
	}()

	// Wait for both streams to finish
	wg.Wait()

	// Wait for command to complete
	if err := command.Wait(); err != nil {
		return outputBuf.String(), fmt.Errorf("command failed: %w", err)
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

// formatCompletionSummary returns a compact one-line summary of turn
// metrics for the completion footer. Returns "" when the agent didn't
// actually run a turn (no tokens, no cost) so slash commands and
// direct-execution fast paths stay quiet.
//
// Format: "12.3k/128k ctx · $0.03 · 3 iters" — single segment so it
// can be passed as a single argument to console.GlyphSuccess.Printf.
//
// Mirrors the cost/ctx formatters in pkg/console/status_footer_format.go
// so the on-screen tokens/dollars match the in-footer figures byte-for-byte.
// Reuses compactTokens from agent_turn_stats.go and adds formatCompactCost
// (parallel to formatCost in pkg/console/status_footer_format.go) so the
// on-screen dollars match the footer's, then and now.
func formatCompletionSummary(chatAgent *agent.Agent) string {
	ctx := chatAgent.GetCurrentContextTokens()
	limit := chatAgent.GetMaxContextTokens()
	cost := chatAgent.GetTotalCost()
	iter := chatAgent.GetCurrentIteration()

	// Skip when the agent didn't accrue any state — covers /help, /stats,
	// direct-exec fast paths, and other slash commands that never reached
	// the LLM.
	if ctx == 0 && cost == 0 && iter == 0 {
		return ""
	}

	parts := []string{}
	if limit > 0 {
		parts = append(parts, fmt.Sprintf("%s/%s ctx", compactTokens(ctx), compactTokens(limit)))
	} else if ctx > 0 {
		parts = append(parts, fmt.Sprintf("%s ctx", compactTokens(ctx)))
	}
	if cost > 0 {
		parts = append(parts, formatCompactCost(cost))
	}
	if iter > 0 {
		parts = append(parts, fmt.Sprintf("%d iters", iter))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// formatCompactCost returns a short USD cost string. Mirrors the precision
// ladder in pkg/console/status_footer_format.go's formatCost so the
// on-screen dollars match the footer's, then and now.
func formatCompactCost(c float64) string {
	switch {
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	case c < 1.0:
		return fmt.Sprintf("$%.3f", c)
	default:
		return fmt.Sprintf("$%.2f", c)
	}
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
