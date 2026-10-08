//go:build !js

package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/daemon"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// This file holds the daemon-identity comparison and messaging that
// tryDaemonOneShot (agent_socket.go) uses to decide whether a running daemon
// is a compatible place to run a turn. It is split out only to keep
// agent_socket.go within the repo's file-size convention.
//
// A daemon is a long-lived process: its build and its config root were fixed
// at its start, and either can differ from the invoking CLI's (a newer
// binary; an --isolated-config run pointing the CLI at the workspace's
// ./.sprout while the daemon still uses the user's main config). Routing a
// turn to such a daemon would run it under the wrong binary or the wrong
// config, silently — so the CLI fetches the daemon's identity first and runs
// in-process on any mismatch.

// identityErrReason renders a short reason for an identity fetch that
// returned no usable identity, for the fall-back message.
func identityErrReason(err error) string {
	if err == nil {
		return "no identity"
	}
	return err.Error()
}

// formatBinaryIdentity renders a binary's version and (when present) its
// build commit, e.g. "v1.2.3 (a1b2c3d)" or "dev".
func formatBinaryIdentity(version, commit string) string {
	if version == "" {
		version = "unknown"
	}
	if commit == "" {
		return version
	}
	return fmt.Sprintf("%s (%s)", version, commit)
}

// displayConfigDir renders a config root for a message, naming the empty
// case explicitly rather than printing nothing.
func displayConfigDir(dir string) string {
	if dir == "" {
		return "(unresolved)"
	}
	return dir
}

// localConfigDirForComparison resolves the invoking CLI's config root for
// identity comparison. An unresolvable root is returned as "" so the
// comparison treats it as unknown (never as "equal" to the daemon's).
func localConfigDirForComparison() string {
	dir, err := envutil.ConfigDir()
	if err != nil {
		return ""
	}
	return dir
}

// daemonIdentityMismatch reports whether the daemon's identity is
// incompatible with the invoking CLI, and a human-readable reason naming
// both sides. It compares two things:
//
//   - Build: the daemon must be the same build as this binary. The version
//     must match, and when both sides know their build commit, the commits
//     must match too. Source builds both report version "dev", and "dev" ==
//     "dev" must compare EQUAL — otherwise every source-build run would fall
//     back and the daemon path would be untestable. But "dev" alone can't
//     distinguish two binaries built from different revisions (the exact
//     stale-daemon case this guards against), so a non-empty commit on both
//     sides that differs IS a mismatch; "dev" vs "v1.2.3" is a mismatch; an
//     empty version on either side is unknown → mismatch.
//
//   - Config root: both paths are resolved to absolute, cleaned form
//     (filepath.Abs + filepath.Clean) before comparison, because the
//     daemon's value and the CLI's value can differ in relative vs absolute
//     form while naming the same directory. An empty root on either side is
//     a mismatch: an unknown config is never assumed equal to a known one.
func daemonIdentityMismatch(ident *daemon.DaemonIdentity, localConfigDir string) (bool, string) {
	if ident.Version != buildinfo.Version {
		return true, fmt.Sprintf("daemon binary is %s but this is %s (version mismatch)",
			formatBinaryIdentity(ident.Version, ident.Commit),
			formatBinaryIdentity(buildinfo.Version, buildinfo.Commit))
	}
	// Same version — but "dev" is shared by every source build, so when both
	// sides know their commit, require it to match. An empty commit on
	// either side is treated as unknown and not compared (a from-source run
	// with no injected commit must still be able to use a daemon from the
	// same source).
	if ident.Commit != "" && buildinfo.Commit != "" && ident.Commit != buildinfo.Commit {
		return true, fmt.Sprintf("daemon binary is %s but this is %s (build commit mismatch)",
			formatBinaryIdentity(ident.Version, ident.Commit),
			formatBinaryIdentity(buildinfo.Version, buildinfo.Commit))
	}

	daemonDir := normalizeConfigDir(ident.ConfigDir)
	localDir := normalizeConfigDir(localConfigDir)
	if daemonDir == "" || localDir == "" || daemonDir != localDir {
		return true, fmt.Sprintf("daemon config is %s but this run's config is %s",
			displayConfigDir(ident.ConfigDir), displayConfigDir(localConfigDir))
	}
	return false, ""
}

// normalizeConfigDir resolves a config root to a comparable absolute,
// cleaned path. Relative and absolute spellings of the same directory
// collapse to the same value; symlinks are NOT resolved. An empty input
// stays empty (unknown), never "." — filepath.Abs of "" would otherwise
// resolve to the cwd and falsely match a daemon running from the same
// directory.
func normalizeConfigDir(dir string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return filepath.Clean(abs)
}

// printInProcessIdentity names the binary and config that will run an
// in-process turn, so the user can always tell where a turn ran — the
// in-process counterpart to the "Running via daemon …" line.
func printInProcessIdentity() {
	console.GlyphDim.Printf("Running in-process: %s", formatBinaryIdentity(buildinfo.Version, buildinfo.Commit))
	console.GlyphDim.Printf("config: %s", displayConfigDir(localConfigDirForComparison()))
}

// newSharedAgentIdentity captures the daemon's build/config identity at
// daemon start. The config root is resolved the same way the daemon's
// per-call agents resolve it (envutil.ConfigDir honors SPROUT_CONFIG), so
// the value reported here is exactly the config a routed turn would run
// under. It is deliberately not re-resolved per call: the daemon's config
// root is fixed by its environment at startup, and a live lookup could
// report a config the daemon never actually used. An unresolvable config
// root is reported as empty rather than failing — the client treats an
// empty value as "unknown" and refuses to route on it.
func newSharedAgentIdentity() daemon.DaemonIdentity {
	ident := daemon.DaemonIdentity{Version: buildinfo.Version, Commit: buildinfo.Commit}
	if dir, err := envutil.ConfigDir(); err == nil {
		ident.ConfigDir = dir
	}
	return ident
}

// Identity implements daemon.AgentService: the daemon's captured identity.
func (s *SharedAgentService) Identity(context.Context) (*daemon.DaemonIdentity, error) {
	ident := s.identity
	return &ident, nil
}
