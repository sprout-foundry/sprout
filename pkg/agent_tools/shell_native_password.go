//go:build !js

package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// passwordRedactRe is built per-command by makePasswordRedactRe. The
// pattern matches the password value as a whole token (bounded by
// non-alphanumeric boundaries on each side) so we never corrupt benign
// output where the password happens to appear as a substring of another
// word (e.g., password "pw" would otherwise rewrite "power" into
// "[REDACTED]er").
//
// The token-boundary check uses the regex `\b` anchors, which match the
// empty string at word boundaries (alphanumeric/underscore on one side,
// non-alphanumeric or string-start/end on the other). The password value
// itself is escaped via regexp.QuoteMeta so regex metacharacters don't
// blow up redaction.

// passwordPromptRe matches common password/passphrase prompts at end of a line.
//
// Covers sudo, passwd, ssh-keygen, gpg, openssl, and generic "Password:" forms.
//
// The trailing `: \s*$` anchors to end-of-line (or end-of-buffer). The wrapper
// runs the regex against both completed lines AND un-terminated byte buffers,
// so the same pattern fires whether the prompt ends with `\n` (e.g.,
// `echo "Password:"`) or with a trailing space (e.g., sudo's `Password: `).
//
// Pattern breakdown:
//   - (?i)                        — case-insensitive
//   - (?:password|pass\s*phrase)  — "password" or "passphrase" or "pass phrase"
//   - \s*(?:for\s+\S+)?           — optional "for <user>"
//   - \s*:\s*                     — colon with optional whitespace
//
// Compiled once at package level to avoid per-command allocation.
var passwordPromptRe = regexp.MustCompile(`(?i)(?:password|pass\s*phrase)\s*(?:for\s+\S+)?\s*:\s*$`)

// maxPasswordAttempts caps the number of times we'll try to answer a password
// prompt. Prevents infinite loops when the password is wrong and the command
// re-prompts (e.g., sudo re-asks after a bad password).
const maxPasswordAttempts = 3

// promptSettleDelay is how long we wait after the last byte arrives before
// treating an un-terminated pending buffer as a password prompt. Bridges the
// gap between a child writing "Password: " (no trailing newline) and the
// subsequent stdin read.
//
// Why 250ms: sudo writes the prompt and then performs a syscall read in
// quick succession — sub-millisecond typically. Under load (CI, slow disk,
// busy terminal) the gap can stretch. 250ms is long enough to be reliable,
// short enough that the user doesn't notice an extra beat before the prompt.
//
// Completed-line prompts ignore this entirely — they fire the moment we see
// `\n`.
const promptSettleDelay = 250 * time.Millisecond

// runShellCommandWithPasswordSupport executes a shell command in silent mode
// with password-prompt detection. It connects stdin/stdout/stderr pipes so
// it can detect password prompts on stdout/stderr, forward them to the
// registered PasswordPrompter, and pipe the response to the child's stdin.
//
// This replaces CombinedOutput for the LLM tool-call path when a prompter is
// available. The captured output is redacted before returning so the password
// value never appears in the tool response.
//
// Detection strategy:
//
//   - Completed lines (containing \n): each completed line is checked
//     against passwordPromptRe immediately. Prompts emitted with trailing
//     \n fire without delay.
//   - Un-terminated prompts (e.g., sudo's `Password: ` with trailing space,
//     no newline): bytes are accumulated in a per-pipe pending buffer.
//     When the regex matches the pending buffer AND no new bytes arrive for
//     promptSettleDelay, the prompter is invoked. This avoids the infinite
//     hang of a strict line-at-a-time scanner.
//
// Implementation note: reads happen on a per-pipe background goroutine so
// the scanner loop can race the settle timer against incoming bytes via
// select. Without this separation, reader.Read would block whenever the
// child has unflushed stdout (typical: sh uses full block buffering when
// stdout is a pipe, not the line-buffered tty mode), and the timer would
// never get a chance to fire.
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

	// Shared buffer for captured output from both stdout and stderr.
	var outputBuf syncBuffer

	var password string
	var attempts int
	var stdinClosed bool
	var promptClaimed bool
	var mu sync.Mutex

	promptReason := fmt.Sprintf("%s needs your password", command)

	// handlePrompt invokes the prompter, pipes the response to the child,
	// and remembers the value for redaction.
	//
	// Concurrency: two scanner goroutines (stdout + stderr) can both detect
	// the same prompt concurrently. The slot is claimed atomically under
	// mu by setting promptClaimed = true — only the goroutine that wins
	// the race calls prompter.Prompt(). Subsequent callers see promptClaimed
	// and bail.
	//
	// Stdin closure is deferred to the outcome branch because the success
	// path needs to WRITE the response to stdin before closing. Closing
	// the pipe during the claim would lose the response. So: claim the
	// slot, call Prompt(), then in the outcome branch (success / error /
	// cap) close stdin and (on success) write the response first.
	//
	// To prevent double-close, all close paths run under mu and check
	// stdinClosed before calling stdinPipe.Close().
	handlePrompt := func() {
		mu.Lock()
		if promptClaimed {
			mu.Unlock()
			return
		}
		promptClaimed = true
		if attempts >= maxPasswordAttempts {
			// Cap reached — don't call the prompter again; just close
			// stdin so the child EOFs on its next read.
			if !stdinClosed {
				stdinClosed = true
				_ = stdinPipe.Close()
			}
			mu.Unlock()
			return
		}
		attempts++
		mu.Unlock()

		// Pause the CLI steer reader while the prompter reads from stdin.
		//
		// Why: the steer reader holds stdin in raw mode for the duration
		// of a turn so it can react to user keystrokes. If the prompter
		// (typically CLIPasswordPrompter via term.ReadPassword) tries to
		// read stdin while the steer reader is still active, the read
		// sees raw-mode input and either returns garbage or EOF. This is
		// the same pattern used by PromptForGitApprovalStdin in git.go.
		//
		// WithCookedStdin no-ops cleanly when no steer reader is
		// registered (non-interactive runs, slash commands before the
		// first turn) — safe to call unconditionally.
		var (
			resp string
			err  error
		)
		_ = clihooks.WithCookedStdin(func() error {
			resp, err = prompter.Prompt(ctx, promptReason)
			return nil // never propagate; the caller inspects err directly
		})

		if err != nil {
			// No interactive surface — close stdin so the child EOFs.
			mu.Lock()
			if !stdinClosed {
				stdinClosed = true
				_ = stdinPipe.Close()
			}
			mu.Unlock()
			return
		}

		mu.Lock()
		password = resp
		if !stdinClosed {
			stdinClosed = true
			_, _ = fmt.Fprintf(stdinPipe, "%s\n", resp)
			_ = stdinPipe.Close()
		}
		mu.Unlock()
	}

	// streamPipeAndDetect replaces the prior bufio.Scanner-based scanner,
	// which buffered entire lines and never fired the prompter on prompts
	// like sudo's `Password: ` (no trailing newline).
	//
	// Reads are driven by a dedicated goroutine so the scanner can race
	// bytes against a settle timer via select. On each chunk:
	//   1. Append to a per-pipe "pending" buffer (bytes since last \n).
	//   2. Split off complete lines. Each is tee'd to outputBuf and
	//      checked against the prompt regex — match fires immediately.
	//   3. If bytes remain without `\n` AND match the regex, arm/reset
	//      the settle timer. Only sustained silence fires the prompt.
	//   4. On EOF / pipe close, drain pending bytes and stop.
	streamPipeAndDetect := func(reader io.Reader) {
		const readChunk = 4096
		chunk := make([]byte, readChunk)

		var pending bytes.Buffer
		var settleTimer *time.Timer

		type readResult struct {
			data []byte
			err  error
		}
		// Reads deliver owned byte slices so the scanner loop can fold
		// them into pending without racing with the reader goroutine
		// (which reuses the chunk buffer on the next Read).
		reads := make(chan readResult, 8)
		stopReader := make(chan struct{})
		go func() {
			for {
				n, err := reader.Read(chunk)
				var payload []byte
				if n > 0 {
					// Copy out of the shared chunk — chunk gets reused
					// on the next Read().
					payload = make([]byte, n)
					copy(payload, chunk[:n])
				}
				select {
				case reads <- readResult{payload, err}:
				case <-stopReader:
					return
				}
				if err != nil {
					return
				}
			}
		}()
		defer close(stopReader)

		armSettle := func() {
			if settleTimer == nil {
				settleTimer = time.NewTimer(promptSettleDelay)
				return
			}
			// Stop returns false if the timer already fired and we
			// haven't drained the channel yet. Drain, then Reset.
			if !settleTimer.Stop() {
				select {
				case <-settleTimer.C:
				default:
				}
			}
			settleTimer.Reset(promptSettleDelay)
		}
		cancelSettle := func() {
			if settleTimer == nil {
				return
			}
			if !settleTimer.Stop() {
				select {
				case <-settleTimer.C:
				default:
				}
			}
			settleTimer = nil
		}

		flushCompleteLines := func() {
			for {
				idx := bytes.IndexByte(pending.Bytes(), '\n')
				if idx < 0 {
					return
				}
				line := string(pending.Next(idx + 1)) // includes \n
				_, _ = outputBuf.Write([]byte(line))

				trimmed := strings.TrimRight(line, "\r\n")
				if passwordPromptRe.MatchString(trimmed) {
					cancelSettle()
					handlePrompt()
				}
			}
		}

		var readErr error
		for readErr == nil {
			var timerC <-chan time.Time
			if settleTimer != nil {
				timerC = settleTimer.C
			}
			select {
			case <-timerC:
				settleTimer = nil
				if pending.Len() > 0 {
					snapshot := pending.String()
					if passwordPromptRe.MatchString(snapshot) {
						_, _ = outputBuf.Write([]byte(snapshot))
						pending.Reset()
						handlePrompt()
					}
				}
			case r := <-reads:
				if len(r.data) > 0 {
					_, _ = pending.Write(r.data)
					flushCompleteLines()
					if pending.Len() > 0 && passwordPromptRe.MatchString(pending.String()) {
						armSettle()
					} else {
						cancelSettle()
					}
				}
				if r.err != nil {
					readErr = r.err
				}
			}
		}

		// EOF / pipe closed — drain any remaining pending bytes.
		if pending.Len() > 0 {
			_, _ = outputBuf.Write(pending.Bytes())
			pending.Reset()
		}
		cancelSettle()
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}

	// Kill the process on context cancellation. This closes the pipes,
	// which unblocks the scanner goroutines below.
	go func() {
		<-ctx.Done()
		_ = cmd.Process.Kill()
	}()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		streamPipeAndDetect(stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		streamPipeAndDetect(stderrPipe)
	}()

	wg.Wait()

	waitErr := cmd.Wait()
	exitCode := extractExitCode(waitErr)

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Redact the password from captured output (if we captured one).
	// Token-boundary redaction: only replace the password when it stands
	// alone as a word (or separated by whitespace/punctuation), so a
	// short password like "pw" never corrupts "power" or "puzzle".
	output := outputBuf.String()
	if password != "" {
		output = redactPassword(output, password)
	}

	finalOutput := buildShellOutputWithStatus(output, command, exitCode, waitErr)
	return finalOutput, nil
}

// redactPassword replaces whole-token occurrences of password in s with
// [REDACTED]. Non-word-character boundaries so short passwords don't
// corrupt benign words that happen to contain the password as a substring,
// AND so passwords with leading/trailing special characters (e.g. "!pw")
// get redacted even though \b wouldn't match between two non-word chars.
//
// Examples (password = "pw"):
//   - "got pw\n"          → "got [REDACTED]\n"
//   - "got=pw\n"          → "got=[REDACTED]\n"
//   - "power"             → "power"   (unchanged — "pw" is not a token)
//   - "puzzle"            → "puzzle"  (unchanged — "pw" is not a token)
//   - "I saw pw today"    → "I saw [REDACTED] today"
//
// Examples (password = "!pw"):
//   - "got !pw\n"         → "got [REDACTED]\n"
//   - "!pw at start"      → "[REDACTED] at start"
//   - "x!pwy"             → "x!pwy"   (unchanged — not a whole token)
//
// The boundary check matches [^\\w] (non-word) on either side OR the
// string start/end. The boundary character (when present) is captured
// and preserved in the replacement, so "!pw,foo" redacts to "[REDACTED],foo"
// not ",[REDACTED]foo".
//
// regexp.QuoteMeta escapes regex metacharacters in password so a password
// like "a.b" doesn't accidentally match any-character-or-literally-b.
func redactPassword(s, password string) string {
	if password == "" {
		return s
	}
	pattern := `(^|[^\w])` + regexp.QuoteMeta(password) + `($|[^\w])`
	re, err := regexp.Compile(pattern)
	if err != nil {
		// Should be impossible — QuoteMeta produces only safe runes —
		// but if it does fail, fall back to no redaction rather than
		// dropping command output.
		return s
	}
	return re.ReplaceAllString(s, "${1}[REDACTED]${2}")
}
