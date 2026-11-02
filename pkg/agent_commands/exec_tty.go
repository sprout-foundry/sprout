package commands

// exec_tty.go — `!<command>` on the user's own terminal.
//
// The capture path (tools.ExecuteShellCommand) is right for the WebUI, which
// renders the captured text, but wrong for a person at a terminal: the child
// gets /dev/null for stdin and its own process group, so sudo, ssh, git
// credential prompts and anything else that asks a question either read EOF
// or are stopped by SIGTTIN when they open /dev/tty. Pagers and editors see
// a pipe instead of a terminal. On a terminal, `!` hands the terminal to the
// child the way a shell would.

import (
	"errors"
	"io"
	"os"
	"os/exec"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/utils/shellexec"

	"golang.org/x/term"
)

// execOnTerminal decides whether `!` runs on the terminal: only for CLI
// output (no injected writer — the WebUI injects one) with a terminal on both
// ends. A variable so tests can force either path.
var execOnTerminal = func(out io.Writer) bool {
	return out == io.Writer(os.Stdout) &&
		term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// runOnTerminal runs command with the terminal attached: stdin, stdout and
// stderr inherited, and the child in sprout's own (foreground) process group
// so it may read the terminal. For the child's lifetime the steer reader
// releases stdin to cooked mode and the spinner and streamed prose stand
// down; afterwards the terminal modes a full-screen child may have left
// behind (alternate screen, hidden cursor, raw termios, scroll region) are
// restored before the reader takes stdin back.
func runOnTerminal(command, dir string) (exitCode int, err error) {
	cmd := shellexec.Command(command)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	runErr := clihooks.WithCookedStdin(func() error {
		clihooks.BeginForegroundChild()
		defer clihooks.EndForegroundChild()
		err := cmd.Run()
		console.ResyncTerminal()
		return err
	})
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, runErr
}
