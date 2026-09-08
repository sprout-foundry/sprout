// Package testgit redirects git's config lookup inside test binaries so a
// subprocess git call can never read or write the developer's real
// ~/.gitconfig or /etc/gitconfig. Tests that commit without setting a
// repo-local identity fall back to the fixed sandbox identity below, and
// gpgsign is disabled so machines with commit signing don't prompt or fail.
package testgit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const config = `[user]
	name = Test User
	email = test@example.com
[init]
	defaultBranch = main
[commit]
	gpgsign = false
[tag]
	gpgsign = false
`

var dir string

// Configure points GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM at throwaway
// files containing a fixed test identity. Call it from a package TestMain
// before m.Run(); every git subprocess spawned by the test binary inherits
// the redirected config via os.Environ.
func Configure() {
	if dir != "" {
		return
	}
	d, err := os.MkdirTemp("", "sprout-testgit-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir = d
	global := filepath.Join(dir, "gitconfig")
	system := filepath.Join(dir, "system-gitconfig")
	if err := os.WriteFile(global, []byte(config), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(system, nil, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("GIT_CONFIG_GLOBAL", global)
	os.Setenv("GIT_CONFIG_SYSTEM", system)
}

// Main runs m.Run() under hermetic git configuration and removes the
// throwaway config directory afterwards. Use it as the whole TestMain in
// packages that do not already have one.
func Main(m *testing.M) {
	Configure()
	code := m.Run()
	if dir != "" {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}
