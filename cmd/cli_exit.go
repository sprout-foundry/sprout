//go:build !js

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/sprout-foundry/sprout/pkg/console"
)

const (
	exitOK          = 0
	exitFailure     = 1
	exitUsage       = 2
	exitInterrupted = 130
)

// errInterrupted marks a run the user stopped with a signal, so scripts can
// tell "I cancelled it" (130, the shell convention for SIGINT) from a failure.
var errInterrupted = errors.New("interrupted")

// usageError is a mistake in how the command was invoked, as opposed to a
// failure while running it. It exits 2 and points at the command's help.
type usageError struct {
	err     error
	cmdPath string
	hint    string
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func newUsageError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*usageError](err); ok {
		return err
	}
	return &usageError{err: err, cmdPath: commandPath(cmd)}
}

func usageErrorf(cmd *cobra.Command, format string, args ...any) error {
	return &usageError{err: fmt.Errorf(format, args...), cmdPath: commandPath(cmd)}
}

// usageErrorAt is usageErrorf for helpers that don't hold the *cobra.Command
// (referencing the command var from its own RunE helper is an init cycle).
func usageErrorAt(cmdPath, format string, args ...any) error {
	return &usageError{err: fmt.Errorf(format, args...), cmdPath: cmdPath}
}

// usageErrorWithHint attaches a line of guidance (typically the expected
// invocation) rendered under the error instead of the generic --help pointer.
func usageErrorWithHint(cmd *cobra.Command, hint, format string, args ...any) error {
	return &usageError{err: fmt.Errorf(format, args...), cmdPath: commandPath(cmd), hint: hint}
}

func commandPath(cmd *cobra.Command) string {
	if cmd == nil {
		return rootCmd.Name()
	}
	return cmd.CommandPath()
}

// cobraUsagePrefixes are the messages cobra produces for invocation mistakes
// it detects outside the Args/FlagError hooks (unknown subcommand, flag-group
// and required-flag validation). Pinned by TestExitCodeForCobraErrors so a
// cobra upgrade that rewords them fails loudly.
var cobraUsagePrefixes = []string{
	"unknown command ",
	"if any flags in the group ",
	"at least one of the flags in the group ",
	"required flag(s) ",
	"unknown shorthand flag",
	"unknown flag",
}

func isCobraUsageMessage(err error) bool {
	msg := err.Error()
	for _, p := range cobraUsagePrefixes {
		if strings.HasPrefix(msg, p) {
			return true
		}
	}
	return false
}

func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	if _, ok := errors.AsType[*usageError](err); ok || isCobraUsageMessage(err) {
		return exitUsage
	}
	if errors.Is(err, errInterrupted) || errors.Is(err, context.Canceled) {
		return exitInterrupted
	}
	return exitFailure
}

// installUsageErrorHooks routes every argument-count and flag-parse failure
// through usageError. Cobra consults FlagErrorFunc up the parent chain, so
// setting it on the root covers every subcommand; Args validators are
// per-command and must be wrapped individually.
func installUsageErrorHooks(root *cobra.Command) {
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return newUsageError(c, err)
	})
	var wrap func(c *cobra.Command)
	wrap = func(c *cobra.Command) {
		if orig := c.Args; orig != nil {
			c.Args = func(cmd *cobra.Command, args []string) error {
				return newUsageError(cmd, orig(cmd, args))
			}
		}
		if c != root && !c.Runnable() && c.HasSubCommands() {
			c.RunE = runCommandGroup
		}
		for _, sub := range c.Commands() {
			wrap(sub)
		}
	}
	wrap(root)
}

// runCommandGroup gives a subcommand-only group (`sprout keys`, `sprout
// config`) a body. Cobra prints help and exits 0 for a non-runnable command
// before Args validation runs, so `sprout config set x` would silently
// "succeed"; a stray word is an unknown subcommand and exits 2.
func runCommandGroup(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t") + "\n"
	}
	return &usageError{err: errors.New(msg), cmdPath: cmd.CommandPath()}
}

func renderUsageError(err error) {
	cmdPath := invokedCommandPath(os.Args[1:])
	hint := ""
	if ue, ok := errors.AsType[*usageError](err); ok {
		cmdPath, hint = ue.cmdPath, ue.hint
	}
	headline, suggestions := splitCobraSuggestions(err.Error())
	headline = humanizeFlagGroupError(headline)
	console.GlyphError.Fprintln(os.Stderr, headline)
	if len(suggestions) > 0 {
		quoted := make([]string, len(suggestions))
		for i, s := range suggestions {
			quoted[i] = fmt.Sprintf("'%s %s'", cmdPath, s)
		}
		console.Hintln(os.Stderr, "Did you mean "+strings.Join(quoted, " or ")+"?")
		return
	}
	if hint != "" {
		console.Hintln(os.Stderr, hint)
		return
	}
	console.Hintln(os.Stderr, fmt.Sprintf("Run '%s --help' for usage.", cmdPath))
}

// splitCobraSuggestions separates cobra's multi-line "unknown command"
// message into its headline and the "Did you mean this?" candidates.
func splitCobraSuggestions(msg string) (string, []string) {
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	headline := strings.TrimSpace(lines[0])
	var suggestions []string
	inSuggestions := false
	for _, l := range lines[1:] {
		t := strings.TrimSpace(l)
		switch {
		case t == "Did you mean this?":
			inSuggestions = true
		case inSuggestions && t != "":
			suggestions = append(suggestions, t)
		}
	}
	return headline, suggestions
}

// hintedError carries a line of recovery guidance rendered beneath the error.
type hintedError struct {
	err  error
	hint string
}

func (e *hintedError) Error() string { return e.err.Error() }
func (e *hintedError) Unwrap() error { return e.err }

func withHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return &hintedError{err: err, hint: hint}
}

// invokedCommandPath names the subcommand the user was running when cobra
// rejected the invocation outside our hooks (flag-group validation).
func invokedCommandPath(args []string) string {
	if c, _, err := rootCmd.Find(args); err == nil && c != nil {
		return c.CommandPath()
	}
	return rootCmd.Name()
}

// humanizeFlagGroupError rewrites cobra's flag-group validation messages
// ("if any flags in the group [a b] are set none of the others can be; [a b]
// were all set") into plain flag language.
func humanizeFlagGroupError(msg string) string {
	switch {
	case strings.HasPrefix(msg, "if any flags in the group "):
		if i := strings.LastIndex(msg, "; ["); i >= 0 {
			if names := bracketFlags(msg[i+2:]); len(names) > 1 {
				return strings.Join(names, " and ") + " can't be used together"
			}
		}
	case strings.HasPrefix(msg, "at least one of the flags in the group "):
		if names := bracketFlags(strings.TrimPrefix(msg, "at least one of the flags in the group ")); len(names) > 0 {
			return "one of " + strings.Join(names, ", ") + " is required"
		}
	}
	return msg
}

func bracketFlags(s string) []string {
	open, end := strings.Index(s, "["), strings.Index(s, "]")
	if open < 0 || end <= open {
		return nil
	}
	fields := strings.Fields(s[open+1 : end])
	for i, f := range fields {
		fields[i] = "--" + f
	}
	return fields
}
