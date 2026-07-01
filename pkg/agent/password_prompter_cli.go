package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"golang.org/x/term"
)

// Compile-time assertion that CLIPasswordPrompter satisfies
// PasswordPrompter. Catches signature drift if the interface changes.
var _ tools.PasswordPrompter = (*CLIPasswordPrompter)(nil)

// CLIPasswordPrompter implements PasswordPrompter for CLI terminal sessions.
// It reads the password from stdin with echo disabled using golang.org/x/term.
type CLIPasswordPrompter struct{}

// NewCLIPasswordPrompter constructs a CLI password prompter.
func NewCLIPasswordPrompter() *CLIPasswordPrompter {
	return &CLIPasswordPrompter{}
}

// Prompt asks the user to type a password on the terminal.
//
// If stdin is not a TTY it returns ErrNoInteractiveSurface immediately.
// The reason string is printed to stderr as a prompt label.
// Terminal state is always restored via defer even when an error occurs.
func (cli *CLIPasswordPrompter) Prompt(ctx context.Context, reason string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", tools.ErrNoInteractiveSurface
	}

	fd := int(os.Stdin.Fd())
	oldState, err := term.GetState(fd)
	if err != nil {
		return "", agenterrors.Wrap(err, "get terminal state")
	}
	// Restore terminal state even if ReadPassword encounters an error or
	// context is cancelled while the goroutine is blocked on the read.
	defer term.Restore(fd, oldState)

	// Write the prompt to stderr so it doesn't interfere with stdout piping.
	fmt.Fprintf(os.Stderr, "%s: ", reason)

	// Read in a goroutine so we can select on context cancellation. NOTE:
	// if ctx is cancelled while term.ReadPassword is blocked, the goroutine
	// is abandoned — there is no portable way to interrupt a blocking
	// terminal read. It will exit when the user types a response or stdin
	// closes.
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
