//go:build !js

package cmd

import (
	"os"

	"github.com/sprout-foundry/sprout/pkg/console"
	daemonpkg "github.com/sprout-foundry/sprout/pkg/daemon"
)

// preferOOMVictimFn is a test seam over daemonpkg.PreferOOMVictim so cmd
// tests can observe the wiring decision without touching /proc.
var preferOOMVictimFn = daemonpkg.PreferOOMVictim

// maybePreferOOMVictim marks an auto-started daemon as a preferred OOM
// victim. Only auto-started daemons (SPROUT_DAEMON_AUTOSTARTED=1, set by
// cmd/daemon_autostart.go on the spawned child) opt in: an explicitly
// started daemon (sprout agent -d, service) is a deliberate start and
// keeps the default oom_score_adj. The daemonMode guard also stops a stray
// env var from sacrificing an ordinary user-facing process. Failure is
// non-fatal — the daemon runs either way.
func maybePreferOOMVictim(daemonMode bool) {
	if !daemonMode || os.Getenv("SPROUT_DAEMON_AUTOSTARTED") != "1" {
		return
	}
	if err := preferOOMVictimFn(); err != nil {
		console.GlyphDim.Printf("daemon OOM victim preference not applied: %v", err)
	}
}
