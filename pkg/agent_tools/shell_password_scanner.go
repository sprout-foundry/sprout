//go:build !js

package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// passwordScanner wraps a pair of stdout/stderr pipe readers with
// password-prompt detection. It is extracted from the original
// runShellCommandWithPasswordSupport so the adoptable execution path
// can reuse the same detection logic without duplicating it.
//
// The scanner reads from both pipes concurrently via dedicated goroutines.
// Each chunk is tee'd to the configured output writer (an io.MultiWriter
// targeting temp file + in-memory buffer) and checked against
// passwordPromptRe. When a prompt is detected, the prompter is invoked
// and the response is piped to the child's stdin.
//
// Two detection modes:
//   - Completed lines (\n-terminated): checked immediately.
//   - Un-terminated prompts (sudo's "Password: " with trailing space):
//     armed via a settle timer that fires after promptSettleDelay of
//     pipe silence.
type passwordScanner struct {
	cmd          *exec.Cmd
	prompter     PasswordPrompter
	promptReason string

	// stdinPipe is the child process's stdin writer. Closed after the
	// password is written (or immediately if no interactive surface).
	stdinPipe io.WriteCloser

	// output is where all non-prompt bytes are tee'd.
	output io.Writer

	// Internal state shared between the two scanner goroutines.
	mu            sync.Mutex
	password      string
	attempts      int
	stdinClosed   bool
	promptClaimed bool
	wg            sync.WaitGroup
}

// newPasswordScanner creates a scanner wired to the given command's pipes.
// The caller must have already created the pipes via cmd.StdinPipe(),
// cmd.StdoutPipe(), cmd.StderrPipe(). output receives all captured bytes.
func newPasswordScanner(cmd *exec.Cmd, prompter PasswordPrompter, promptReason string, output io.Writer) (*passwordScanner, error) {
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("get stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("get stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("get stderr pipe: %w", err)
	}

	ps := &passwordScanner{
		cmd:          cmd,
		prompter:     prompter,
		promptReason: promptReason,
		stdinPipe:    stdinPipe,
		output:       output,
	}

	ps.wg.Add(2)
	go func() {
		defer ps.wg.Done()
		ps.streamPipeAndDetect(stdoutPipe)
	}()
	go func() {
		defer ps.wg.Done()
		ps.streamPipeAndDetect(stderrPipe)
	}()

	return ps, nil
}

// Wait blocks until both scanner goroutines have finished (EOF on both
// pipes). The caller is responsible for calling cmd.Wait() after this
// returns.
func (ps *passwordScanner) Wait() {
	ps.wg.Wait()
}

// Password returns the captured password (for redaction). Must be called
// after Wait().
func (ps *passwordScanner) Password() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.password
}

// CloseStdin closes the child's stdin pipe. Safe to call multiple times.
func (ps *passwordScanner) CloseStdin() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if !ps.stdinClosed {
		ps.stdinClosed = true
		_ = ps.stdinPipe.Close()
	}
}

// handlePrompt invokes the prompter, pipes the response to the child,
// and remembers the value for redaction.
//
// secret distinguishes a masked answer (password) from an ordinary one
// (a confirmation or a value). Only a secret answer is remembered for
// redaction; a non-secret answer is part of the transcript and must not be
// scrubbed from it.
//
// Concurrency: two scanner goroutines (stdout + stderr) can both detect
// the same prompt concurrently. The slot is claimed atomically under
// mu by setting promptClaimed = true — only the goroutine that wins
// the race calls prompter.Prompt(). Subsequent callers see promptClaimed
// and bail.
//
// The claim is released by releasePrompt when the command produces new
// output after an answer, which is how a second, distinct prompt ("Overwrite?
// [y/N]" then "Enter a name:") becomes answerable. Releasing it here instead
// would let the losing goroutine re-claim the slot for the prompt that was
// just answered.
func (ps *passwordScanner) handlePrompt(secret bool) {
	ps.mu.Lock()
	if ps.promptClaimed {
		ps.mu.Unlock()
		return
	}
	ps.promptClaimed = true
	// Only secret prompts count against the cap: it exists to stop a
	// wrong-password re-prompt loop, not to limit how many questions a
	// wizard-style command may ask.
	if secret && ps.attempts >= maxPasswordAttempts {
		if !ps.stdinClosed {
			ps.stdinClosed = true
			_ = ps.stdinPipe.Close()
		}
		ps.mu.Unlock()
		return
	}
	if secret {
		ps.attempts++
	}
	ps.mu.Unlock()

	var (
		resp string
		err  error
	)
	_ = clihooks.WithCookedStdin(func() error {
		resp, err = PromptForAnswer(context.Background(), ps.prompter, ps.promptReason, secret)
		return nil
	})

	if err != nil {
		ps.mu.Lock()
		if !ps.stdinClosed {
			ps.stdinClosed = true
			_ = ps.stdinPipe.Close()
		}
		ps.mu.Unlock()
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()
	if secret {
		ps.password = resp
	}
	if !ps.stdinClosed {
		_, _ = fmt.Fprintf(ps.stdinPipe, "%s\n", resp)
		// A secret answer closes stdin: the password is the only thing the
		// child should receive, and leaving the pipe open lets a confused
		// program block on a second read forever. A non-secret answer keeps
		// stdin open for follow-up prompts.
		if secret {
			ps.stdinClosed = true
			_ = ps.stdinPipe.Close()
		}
	}
}

// releasePrompt re-opens the prompt slot so a later, distinct prompt in the
// same command can be answered. Called when new output arrives: a duplicate
// detection of an already-answered prompt is not preceded by new output, so
// it cannot re-trigger a dialog.
func (ps *passwordScanner) releasePrompt() {
	ps.mu.Lock()
	if ps.stdinClosed {
		// stdin is gone; nothing can be answered any more.
		ps.mu.Unlock()
		return
	}
	ps.promptClaimed = false
	ps.mu.Unlock()
}

// streamPipeAndDetect reads from a single pipe, tees output to the
// configured writer, and detects password prompts.
func (ps *passwordScanner) streamPipeAndDetect(reader io.Reader) {
	const readChunk = 4096
	chunk := make([]byte, readChunk)

	var pending bytes.Buffer
	var settleTimer *time.Timer

	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult, 8)
	stopReader := make(chan struct{})
	go func() {
		for {
			n, err := reader.Read(chunk)
			var payload []byte
			if n > 0 {
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
			line := string(pending.Next(idx + 1))
			_, _ = ps.output.Write([]byte(line))

			trimmed := strings.TrimRight(line, "\r\n")
			if ok, secret := isInteractivePrompt(trimmed); ok {
				cancelSettle()
				ps.handlePrompt(secret)
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
				if ok, secret := isInteractivePrompt(snapshot); ok {
					_, _ = ps.output.Write([]byte(snapshot))
					pending.Reset()
					ps.handlePrompt(secret)
				}
			}
		case r := <-reads:
			if len(r.data) > 0 {
				// New output after an answer means the command moved past the
				// prompt it was blocked on; a later prompt is a new question.
				ps.releasePrompt()
				_, _ = pending.Write(r.data)
				flushCompleteLines()
				if pending.Len() > 0 {
					if ok, _ := isInteractivePrompt(pending.String()); ok {
						armSettle()
					} else {
						cancelSettle()
					}
				} else {
					cancelSettle()
				}
			}
			if r.err != nil {
				readErr = r.err
			}
		}
	}

	if pending.Len() > 0 {
		_, _ = ps.output.Write(pending.Bytes())
		pending.Reset()
	}
	cancelSettle()
}
