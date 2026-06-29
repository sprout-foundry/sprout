//go:build !js

package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// passwordPromptRe matches common password/passphrase prompts at end of a line.
// Covers sudo, passwd, ssh-keygen, gpg, openssl, and generic "Password:" forms.
//
// Pattern breakdown:
//   - (?i)                        — case-insensitive
//   - (password|pass\s*phrase)    — "password" or "passphrase" or "pass phrase"
//   - \s*(?:for\s+\S+)?\s*:\s*$  — optional "for <user>", then colon, end-of-line
//
// Compiled once at package level to avoid per-command allocation.
var passwordPromptRe = regexp.MustCompile(`(?i)(?:password|pass\s*phrase)\s*(?:for\s+\S+)?\s*:\s*$`)

// maxPasswordAttempts caps the number of times we'll try to answer a password
// prompt. Prevents infinite loops when the password is wrong and the command
// re-prompts (e.g., sudo re-asks after a bad password).
const maxPasswordAttempts = 3

// runShellCommandWithPasswordSupport executes a shell command in silent mode
// with password-prompt detection. It connects stdin/stdout/stderr pipes so it
// can detect password prompts on stdout/stderr, forward them to the registered
// PasswordPrompter, and pipe the response to the child's stdin.
//
// This replaces CombinedOutput for the LLM tool-call path when a prompter is
// available. The captured output is redacted before returning so the password
// value never appears in the tool response.
func runShellCommandWithPasswordSupport(ctx context.Context, command string, prompter PasswordPrompter) (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-c", command)

	if wd := filesystem.WorkspaceRootFromContext(ctx); wd != "" {
		cmd.Dir = wd
	} else if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}

	// Set process group so we can kill the entire group on cancellation.
	setProcessGroup(cmd)

	// Create pipes for stdin (to send password) and stdout/stderr (to scan for prompts).
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("get stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("get stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("get stderr pipe: %w", err)
	}

	// Shared buffer to capture output from both stdout and stderr.
	var outputBuf syncBuffer

	// Track the password value so we can redact it from the output.
	var password string
	var attempts int
	var mu sync.Mutex // guards password, attempts, and stdinClosed

	// stdinClosed tracks whether stdin has been closed after sending a password.
	var stdinClosed bool

	// scanAndTee reads lines from a pipe, writes them to the output buffer,
	// and checks each line for password prompts. It continues scanning after
	// handling a prompt so all output is captured.
	scanAndTee := func(reader io.Reader) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()

			// Always tee the line to the output buffer.
			_, _ = outputBuf.Write([]byte(line + "\n"))

			// Check if this line is a password prompt.
			if !passwordPromptRe.MatchString(line) {
				continue
			}

			mu.Lock()
			if attempts >= maxPasswordAttempts {
				// Cap reached — close stdin so the child gets EOF and can exit.
				if !stdinClosed {
					stdinClosed = true
					_ = stdinPipe.Close()
				}
				mu.Unlock()
				continue
			}
			attempts++
			mu.Unlock()

			resp, err := prompter.Prompt(ctx, fmt.Sprintf("%s needs your password", command))
			if err != nil {
				// No interactive surface — close stdin so the child gets EOF
				// and can exit instead of blocking forever on read.
				mu.Lock()
				if !stdinClosed {
					stdinClosed = true
					_ = stdinPipe.Close()
				}
				mu.Unlock()
				continue
			}

			// Remember the password for redaction.
			mu.Lock()
			password = resp

			// Only send and close stdin once.
			if !stdinClosed {
				stdinClosed = true
				_, writeErr := fmt.Fprintf(stdinPipe, "%s\n", resp)
				if writeErr != nil {
					// stdin already closed or broken — harmless
				}
				_ = stdinPipe.Close()
			}
			mu.Unlock()
		}
	}

	// Start the process.
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}

	// Kill the process if context is cancelled. This closes stdout/stderr
	// pipes, which unblocks the scanner goroutines below.
	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
	}()

	// Scan stdout and stderr concurrently.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanAndTee(stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		scanAndTee(stderrPipe)
	}()

	// Wait for scanners to finish (pipes close when child exits or is killed).
	wg.Wait()

	// Wait for the command to exit (already killed if ctx was cancelled).
	waitErr := cmd.Wait()
	exitCode := extractExitCode(waitErr)

	// If ctx was cancelled, return the context error.
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Redact the password from captured output (if we captured one).
	output := outputBuf.String()
	if password != "" {
		output = strings.ReplaceAll(output, password, "[REDACTED]")
	}

	finalOutput := buildShellOutputWithStatus(output, command, exitCode, waitErr)
	return finalOutput, nil
}
