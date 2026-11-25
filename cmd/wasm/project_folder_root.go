//go:build js && wasm

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/wasmshell"
)

// projectFolder is where the host's project lives, apart from the shell's
// home and scratch directories. The bridge resolves the host's relative paths
// against it rather than the process cwd, which the interactive terminal's
// cd moves; the agent and its shell commands work from it too. A host picks a
// different project with changeDir.
var projectFolder = "/workspace"

func setProjectFolder(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	projectFolder = filepath.Clean(dir)
	wasmshell.ShellEnv.Set("PWD", projectFolder)
	return nil
}

// projectFolderPath resolves a path from the host: absolute paths stand, ~ is the
// shell's home, anything else is relative to the project folder.
func projectFolderPath(p string) string {
	home := wasmshell.ShellEnv.Get("HOME")
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case filepath.IsAbs(p):
		return filepath.Clean(p)
	default:
		return filepath.Join(projectFolder, p)
	}
}

// fromProjectFolder runs fn with the process cwd at the project folder, then puts
// the cwd back, so the agent's commands don't depend on where the terminal
// last cd'd to.
func fromProjectFolder[T any](fn func() T) T {
	prev, err := os.Getwd()
	if err == nil && prev != projectFolder {
		if os.Chdir(projectFolder) == nil {
			defer func() { _ = os.Chdir(prev) }()
		}
	}
	return fn()
}
