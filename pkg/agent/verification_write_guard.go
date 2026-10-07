// verification_write_guard.go — the verification write rail: while
// verification is enabled for a turn, the model may not write or edit the
// project's starter manifest. The manifest is the project's trusted source
// for the verification commands (build, test, dev, port, routes), so a
// mid-turn rewrite would change what "passing" means. The turn-start
// snapshot (pkg/verify) is the enforcement; this guard is the polite rail
// that refuses the write and tells the model to ask the user.
//
// The guard is a no-op when verification is disabled (the CLI default), so
// disabling it leaves file writes byte-for-byte unchanged.

package agent

import (
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/starterstore"
)

// refuseStarterManifestWrite reports an error when the model tries to write
// or edit the project's starter manifest during a turn in which
// verification is enabled. The manifest is the project's trusted source for
// the verification commands; a mid-turn rewrite would change what "passing"
// means. It returns nil — leaving the write to proceed
// unchanged — when verification is disabled (the default) or when the path
// does not resolve to the manifest. Call it at the top of the write and edit
// handlers, right after the path is extracted, so both the plain and the
// JSON routes (and both tools) are covered before any routing, read, or
// write.
func (a *Agent) refuseStarterManifestWrite(path string) error {
	if a == nil || a.configManager == nil {
		return nil
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || !cfg.VerificationEnabled() {
		return nil
	}
	if pathResolvesToStarterManifest(a, path) {
		return agenterrors.NewValidation(
			"the starter manifest (.sprout/starter.json) is the project's trusted source for its verification commands and cannot be modified during a turn; if it needs to change, tell the user and let them make the edit",
			map[string]any{"path": path})
	}
	return nil
}

// pathResolvesToStarterManifest reports whether a model-supplied path
// resolves to the project's starter manifest. The model's path may be
// relative, and the write path resolves relative paths against the
// workspace root (or the process cwd when the context carries no workspace
// root). We compare the cleaned absolute path against the manifest path
// under every base the write could use, so the guard never misses a manifest
// write because of a path-resolution mismatch (an absolute path is compared
// as-is).
func pathResolvesToStarterManifest(a *Agent, path string) bool {
	p := strings.TrimSpace(path)
	if p == "" {
		return false
	}

	// The bases a relative model path could be resolved against.
	bases := map[string]struct{}{}
	if root := a.GetWorkspaceRoot(); root != "" {
		bases[root] = struct{}{}
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		bases[cwd] = struct{}{}
	}
	if len(bases) == 0 {
		return false
	}

	// The manifest path under each base — the write's target.
	manifests := make(map[string]struct{}, len(bases))
	for base := range bases {
		if m, err := filepath.Abs(starterstore.StarterManifestPath(base)); err == nil {
			manifests[m] = struct{}{}
		}
	}
	if len(manifests) == 0 {
		return false
	}

	// The model path, resolved under each base (as-is when absolute).
	for base := range bases {
		var candidate string
		if filepath.IsAbs(p) {
			candidate = p
		} else {
			candidate = filepath.Join(base, p)
		}
		if resolved, err := filepath.Abs(candidate); err == nil {
			if _, ok := manifests[resolved]; ok {
				return true
			}
		}
	}
	return false
}
