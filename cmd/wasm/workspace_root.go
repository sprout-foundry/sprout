//go:build js && wasm

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/wasmshell"
)

// workspaceRoot is where the host's project lives, apart from the shell's
// home and scratch directories. The bridge resolves the host's relative paths
// against it rather than the process cwd, which the interactive terminal's
// cd moves; the agent and its shell commands work from it too. A host picks a
// different project with changeDir.
var workspaceRoot = "/workspace"

func setWorkspaceRoot(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	workspaceRoot = filepath.Clean(dir)
	wasmshell.ShellEnv.Set("PWD", workspaceRoot)
	return nil
}

// workspacePath resolves a path from the host: absolute paths stand, ~ is the
// shell's home, anything else is relative to the workspace root.
func workspacePath(p string) string {
	home := wasmshell.ShellEnv.Get("HOME")
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case filepath.IsAbs(p):
		return filepath.Clean(p)
	default:
		return filepath.Join(workspaceRoot, p)
	}
}

// fromWorkspace runs fn with the process cwd at the workspace root, then puts
// the cwd back, so the agent's commands don't depend on where the terminal
// last cd'd to.
func fromWorkspace[T any](fn func() T) T {
	prev, err := os.Getwd()
	if err == nil && prev != workspaceRoot {
		if os.Chdir(workspaceRoot) == nil {
			defer func() { _ = os.Chdir(prev) }()
		}
	}
	return fn()
}
