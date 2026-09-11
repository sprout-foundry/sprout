//go:build !js

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/buildinfo"
	"github.com/sprout-foundry/sprout/pkg/updatecheck"
) // maybeStartUpdateCheck kicks off the passive release check in the
// background when running interactive agent mode. Never blocks, never
// prints, never fails the session: any problem degrades to "no notice
// today". The rendered notice uses the cached state, so a fresh install
// sees nothing until the next start.
func maybeStartUpdateCheck(a *agent.Agent) {
	if updateCheckSkipped(a) {
		return
	}
	go updatecheck.RefreshMaybe(context.Background(), buildinfo.Version, time.Now())
}

// maybeRenderUpdateNotice prints the one-line update notice to stderr at
// most once per notice interval per newer version, drawing on the cache
// the background check maintains. Runs before the input reader exists so
// it can't interleave with prompt output. A nil agent or nil config
// doesn't suppress the notice — the notice path makes no network call.
func maybeRenderUpdateNotice(a *agent.Agent) {
	disabled := false
	if a != nil {
		if cfg := a.GetConfig(); cfg != nil {
			disabled = cfg.DisableUpdateCheck
		}
	}
	if updatecheck.Skipped(buildinfo.Version, disabled) {
		return
	}
	latest, ok := updatecheck.NoticeFor(buildinfo.Version, time.Now())
	if !ok {
		return
	}
	writeUpdateNotice(os.Stderr, latest, upgradeHint())
}

// maybeRenderUpdateNoticeTo is maybeRenderUpdateNotice with an
// injectable sink for tests.
func maybeRenderUpdateNoticeTo(w io.Writer) {
	latest, ok := updatecheck.NoticeFor(buildinfo.Version, time.Now())
	if !ok {
		return
	}
	writeUpdateNotice(w, latest, upgradeHint())
}

func writeUpdateNotice(w io.Writer, latest, hint string) {
	fmt.Fprintf(w, "sprout %s available — run `%s`\n", latest, hint)
}

// updateCheckSkipped resolves skip conditions for the CLI's check. A
// missing agent config fails closed: no config, no network.
func updateCheckSkipped(a *agent.Agent) bool {
	disabled := true
	if a != nil {
		if cfg := a.GetConfig(); cfg != nil {
			disabled = cfg.DisableUpdateCheck
		}
	}
	return updatecheck.Skipped(buildinfo.Version, disabled)
}

// upgradeHint names the right upgrade command for how this binary was
// installed. An in-place `sprout upgrade` under Homebrew leaves the
// Cellar entry stale and the next `brew upgrade` may clobber or
// downgrade it, so brew-managed installs get the brew command instead.
func upgradeHint() string {
	if brewManagedBinary() {
		return "brew upgrade sprout"
	}
	return "sprout upgrade"
}

func brewManagedBinary() bool {
	execPath, err := os.Executable()
	if err != nil {
		return false
	}
	return isBrewPath(execPath)
}

func isBrewPath(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	return strings.Contains(filepath.ToSlash(resolved), "/Cellar/")
}
