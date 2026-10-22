package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"golang.org/x/term"
)

// Compile-time assertion that CLIPasswordPrompter satisfies
// PasswordPrompter. Catches signature drift if the interface changes.
var _ tools.PasswordPrompter = (*CLIPasswordPrompter)(nil)

// CLIPasswordPrompter implements PasswordPrompter for CLI terminal sessions. Reads password from stdin with echo disabled.
type CLIPasswordPrompter struct{}

// NewCLIPasswordPrompter constructs a CLI password prompter.
func NewCLIPasswordPrompter() *CLIPasswordPrompter {
	return &CLIPasswordPrompter{}
}

// Prompt asks the user to type a password on the terminal.
// Returns ErrNoInteractiveSurface if stdin is not a TTY. SuspendIndicator/PauseSteer/SuspendStreaming ensure the prompt isn't clobbered.
func (cli *CLIPasswordPrompter) Prompt(ctx context.Context, reason string) (string, error) {
	return cli.PromptInput(ctx, reason, true)
}

// PromptInput asks the user to answer a shell prompt. A secret answer is read
// without echo; a non-secret one is echoed normally, the way a terminal would
// show it.
func (cli *CLIPasswordPrompter) PromptInput(ctx context.Context, reason string, secret bool) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", tools.ErrNoInteractiveSurface
	}

	// Suspend the CLI spinner, pause the steer reader, and suspend streaming so the prompt isn't clobbered.
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeIndicator()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	// Write the prompt to stderr so it doesn't interfere with stdout piping.
	fmt.Fprintf(os.Stderr, "%s: ", reason)

	if !secret {
		return cli.readLine(ctx)
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.GetState(fd)
	if err != nil {
		return "", agenterrors.Wrap(err, "get terminal state")
	}
	// Restore terminal state even if ReadPassword encounters an error or
	// context is cancelled while the goroutine is blocked on the read.
	defer term.Restore(fd, oldState)

	// Read in a goroutine so we can select on context cancellation.
	type result struct {
		password []byte
		err      error
	}
	ch := make(chan result, 1)
	go func() {
		pwd, err := term.ReadPassword(fd)
		ch <- result{pwd, err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return "", agenterrors.Wrap(r.err, "read password")
		}
		// ReadPassword strips the terminating newline, but trim for safety.
		return strings.TrimSuffix(string(r.password), "\n"), nil
	}
}

// readLine reads one echoed line from stdin, so the user can see and edit a
// non-secret answer. Reads in a goroutine so context cancellation is
// honoured while blocked.
//
// Reads byte-by-byte from the fd rather than through a bufio.Reader: a
// buffered reader would swallow bytes the user typed past the newline, and
// that reader is discarded here, so those keystrokes would be lost before the
// steer reader or the next prompt could see them.
func (cli *CLIPasswordPrompter) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if buf[0] == '\n' {
					ch <- result{sb.String(), nil}
					return
				}
				sb.WriteByte(buf[0])
			}
			if err != nil {
				ch <- result{sb.String(), err}
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		// io.EOF with a partial line is still a usable answer.
		if r.err != nil && r.err != io.EOF {
			return "", agenterrors.Wrap(r.err, "read input")
		}
		return strings.TrimRight(r.line, "\r"), nil
	}
}
