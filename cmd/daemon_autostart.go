package cmd

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	daemonpkg "github.com/sprout-foundry/sprout/pkg/daemon"
)

// defaultDaemonIdleTimeout is applied to daemons spawned by the CLI so they
// reap themselves after the WebUI has been idle this long (SP-136 P2).
const defaultDaemonIdleTimeout = 60 * time.Second

// maybeAutoStartDaemon implements SP-136 P2's lazy daemon auto-start for the
// CLI: when a sprout command starts and no healthy daemon is detected, spawn
// one in the background (best-effort, asynchronous) so later phases (P3/P4)
// can route embedding and agent work through it.
//
// It returns a cleanup func (no-op in P2; later phases register a lifecycle
// connection here). It NEVER blocks the caller: startup proceeds in-process
// regardless of what the daemon does — the daemon is a background resource.
//
// Skipped entirely when:
//   - SPROUT_DAEMON=0 (explicit escape hatch → force in-process)
//   - SPROUT_DAEMON=1 (we ARE the daemon process)
//   - daemonMode is active (same as above)
func maybeAutoStartDaemon(ctx context.Context, daemonMode bool) func() {
	if v, ok := os.LookupEnv("SPROUT_DAEMON"); ok && (v == "0" || v == "1") {
		if v == "0" {
			console.GlyphDim.Printf("SPROUT_DAEMON=0 — running in-process (daemon auto-start disabled)")
		}
		return func() {}
	}
	if daemonMode {
		return func() {}
	}

	go func() {
		spec := daemonpkg.DefaultDaemonSpec()
		// Auto-started daemons reap themselves after the idle window.
		spec.Env = append(spec.Env, "SPROUT_DAEMON_IDLE_TIMEOUT="+defaultDaemonIdleTimeout.String())

		already, err := daemonpkg.EnsureDaemon(ctx, spec)
		if err != nil {
			if errors.Is(err, daemonpkg.ErrDaemonDisabled) {
				return // escape hatch — silent by design
			}
			// Fallback: continue in-process. The daemon is an optimization,
			// not a requirement; sprout must always work.
			console.GlyphWarning.Fprintf(os.Stderr,
				"daemon auto-start failed (%v); continuing in-process\n", err)
			return
		}
		if already {
			console.GlyphDim.Printf("daemon already running at %s", spec.DaemonURL)
		} else {
			console.GlyphDim.Printf("background daemon started at %s (idle timeout %s)",
				spec.DaemonURL, defaultDaemonIdleTimeout)
		}
	}()

	// P2 has no connection to register; P3/P4 will Add/Remove on the
	// lifecycle as the CLI opens/closes daemon sessions.
	return func() {}
}
