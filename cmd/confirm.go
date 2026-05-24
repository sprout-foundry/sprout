//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
	"golang.org/x/term"
)

// testIsTerminal, when non-nil, overrides the real TTY check.
// Used by tests to simulate terminal/non-terminal stdin.
var testIsTerminal func() bool

// StdinIsTerminal returns true if os.Stdin is connected to a terminal.
// Used by command handlers to decide whether to show interactive prompts.
// If testIsTerminal is set (in tests), it delegates to that function.
func StdinIsTerminal() bool {
	if testIsTerminal != nil {
		return testIsTerminal()
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ConfirmPrompt displays a confirmation prompt to the user and reads their
// response from stdin. It returns true only if the user types "y" or "yes"
// (case-insensitive). Any other input (including empty) returns false.
// If reading from stdin fails (e.g., not a TTY), it returns false.
// The prompt is written to stderr so it doesn't interfere with stdout capture.
// The msg should NOT include the [y/N] suffix — it is appended automatically
// with the default letter bolded when stderr is a terminal.
func ConfirmPrompt(msg string) bool {
	fmt.Fprintf(os.Stderr, "%s %s\n", msg, console.FormatYesNoPrompt(false))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}
