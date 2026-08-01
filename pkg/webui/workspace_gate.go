//go:build !js

package webui

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

// resolveHomeDir resolves the user's home directory using the same resolution
// chain as server.go's daemonRoot logic. os.UserHomeDir() is tried FIRST so
// that it reflects the current process environment ($HOME) at call time —
// this matters both for service-mode freshness and for test isolation
// (t.Setenv("HOME", ...) is honored only by os.UserHomeDir(), not by
// user.Current(), which may cache its result).
//
//   1. os.UserHomeDir() ($HOME)
//   2. user.Current().HomeDir (fallback for service mode when $HOME is stale)
//   3. "" if both fail
func resolveHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return ""
}

// isHomeWorkspace reports whether the workspace root resolves (after symlink
// evaluation) to the user's home directory. Symlink-safe: both sides are
// passed through filepath.EvalSymlinks before the comparison so that, e.g.,
// /var/folders vs /private/var/folders do not cause a false negative on macOS.
//
// Only an EXACT home match counts — a subdirectory of home (e.g. ~/dev) is a
// legitimate, scoped project root and must not be gated. Returns false if
// either path is empty or cannot be evaluated.
func isHomeWorkspace(workspaceRoot string) bool {
	if workspaceRoot == "" {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return false
	}
	home := resolveHomeDir()
	if home == "" {
		return false
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	return resolvedRoot == resolvedHome
}

// homeWorkspaceConsentFile is the on-disk JSON shape for home-as-workspace
// consent, stored at ~/.sprout/workspace_consent.json.
type homeWorkspaceConsentFile struct {
	HomeWorkspace struct {
		ConsentedAt time.Time `json:"consented_at"`
	} `json:"home_workspace"`
}

// homeConsentPath returns the path to the home-workspace consent file
// (~/.sprout/workspace_consent.json). Returns "" if the home directory cannot
// be resolved, in which case consent functions degrade to "no consent".
func homeConsentPath() string {
	home := resolveHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".sprout", "workspace_consent.json")
}

// hasHomeWorkspaceConsent reports whether the user has previously granted
// explicit consent to run with the home directory as the workspace. Returns
// false on any error (missing file, parse error, unresolvable home) — the gate
// fails closed.
func hasHomeWorkspaceConsent() bool {
	path := homeConsentPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var consent homeWorkspaceConsentFile
	if err := json.Unmarshal(data, &consent); err != nil {
		return false
	}
	return !consent.HomeWorkspace.ConsentedAt.IsZero()
}

// recordHomeWorkspaceConsent persists the user's explicit consent to use the
// home directory as the workspace. The parent directory is created (0700) and
// the file written with 0600. Errors are returned to the caller (the API
// handler logs but does not fail the request on error).
func recordHomeWorkspaceConsent() error {
	path := homeConsentPath()
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	var consent homeWorkspaceConsentFile
	consent.HomeWorkspace.ConsentedAt = time.Now()
	data, err := json.MarshalIndent(consent, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	webuiLogger.Info("home-workspace consent recorded",
		slog.String("consent_path", path))
	return nil
}
