// Package sandbox confines a runner workspace's processes in native mode
// (SP-159 §159c): host toolchains under the OS sandbox — Seatbelt on macOS,
// bubblewrap on Linux. Windows has no native sandbox yet and reports itself
// unavailable.
//
// Known limits: on macOS, Mach services (securityd, nsurlsessiond) stay
// reachable, so the system keychain and indirect network access are not
// blocked; and a sandboxed process cannot start another Seatbelt sandbox, so
// self-sandboxing tools (SwiftPM manifests, Xcode user-script sandboxing)
// must be told to skip theirs.
package sandbox

import (
	"context"
	"os/exec"
)

// Capability describes the isolation available on this machine.
type Capability struct {
	// Name identifies the mechanism ("seatbelt", "bwrap", "landlock+bwrap",
	// "restricted-token", "none"); the runner reports it in heartbeats.
	Name string
	// Available is false when native mode cannot be sandboxed here.
	Available bool
	// Weak marks isolation that is not a real boundary (Windows), shown to
	// the user wherever the mode is selectable.
	Weak bool
	// Detail explains a missing or weak capability in user-facing words.
	Detail string
}

// Policy is what a sandboxed workspace process may touch.
type Policy struct {
	// WorkDir is the workspace clone: readable and writable.
	WorkDir string
	// TempDir is the per-workspace temp dir: readable and writable.
	TempDir string
	// Writable are extra writable paths the user allowlisted (toolchain
	// caches such as Xcode DerivedData).
	Writable []string
	// DenyRead are paths that must not be readable even though the rest of
	// the host is (credential stores, keychains, browser profiles). Use
	// DefaultDenyRead for the standard set.
	DenyRead []string
	// AllowNetwork permits outbound network access. Package managers need
	// it, so the runner enables it by default.
	AllowNetwork bool
}

// Detect reports the isolation available on this machine.
func Detect() Capability { return detect() }

// CommandContext returns a command that runs name/args confined by p. The
// caller sets Dir, Env, Stdout/Stderr on the result as for exec.Cmd. It
// returns an error when Detect().Available is false.
func CommandContext(ctx context.Context, p Policy, name string, args ...string) (*exec.Cmd, error) {
	return commandContext(ctx, p, name, args...)
}

// DefaultDenyRead returns the credential and private-data paths under home
// that a native workspace must never read.
func DefaultDenyRead(home string) []string { return defaultDenyRead(home) }
