// Package shellexec builds the *exec.Cmd that runs a command line through
// the user's shell. Every "run this string as a shell command" path in
// sprout goes through here so the shell choice is made in one place.
//
// Unix honors $SHELL and falls back to /bin/sh. Windows has neither: it
// prefers a POSIX shell (Git for Windows' bash) because the agent writes
// POSIX command lines, and falls back to cmd.exe when none is installed.
package shellexec

import (
	"context"
	"os/exec"
)

// Command returns an *exec.Cmd that runs command through the user's shell.
func Command(command string) *exec.Cmd {
	name, args := argv(command)
	cmd := exec.Command(name, args...)
	prepare(cmd, command)
	return cmd
}

// CommandContext is Command bound to ctx (the process is killed when ctx ends).
func CommandContext(ctx context.Context, command string) *exec.Cmd {
	name, args := argv(command)
	cmd := exec.CommandContext(ctx, name, args...)
	prepare(cmd, command)
	return cmd
}

// ScriptCommandContext runs the script at path with the user's shell.
func ScriptCommandContext(ctx context.Context, path string) *exec.Cmd {
	if sh := Path(); sh != "" {
		return exec.CommandContext(ctx, sh, path)
	}
	return CommandContext(ctx, `"`+path+`"`)
}
