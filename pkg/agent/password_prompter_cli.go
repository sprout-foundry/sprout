package agent

import (
	"context"
	"fmt"
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
//
// The clihooks.WithCookedStdin wrapper ensures the active CLI activity
// indicator (spinner) is suspended and the SP-055 SteerInputReader (which
// holds stdin in raw mode during a turn) is paused for the duration of
// the read. Without this, the spinner would clobber the prompt text on
// stderr, and a mid-turn call would hit EOF immediately because the steer
// reader is consuming raw-mode stdin. WithCookedStdin is a no-op when
// no hook is registered (non-interactive runs).
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

	// Suspend the CLI spinner and pause the SP-055 SteerInputReader so the
	// prompt text isn't clobbered by an in-flight indicator and so stdin
	// isn't held in raw mode by the steer reader (which would cause
	// term.ReadPassword to see odd input on a mid-turn call). Also
	// suspend the streaming callback's prose output so a mid-turn call
	// (e.g. NativeShellPassword) isn't trampled by a concurrent chunk
	// write. All three hooks no-op when no implementation is registered
	// (non-interactive).
	clihooks.SuspendIndicator()
	clihooks.PauseSteer()
	clihooks.SuspendStreaming()
	defer clihooks.ResumeIndicator()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

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
