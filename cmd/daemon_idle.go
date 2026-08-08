package cmd

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/webui"
)

// idleCheckInterval is the poll cadence for reapIdleDaemon. A package var
// (not a const) so tests can shorten it; 0 means the production default.
var idleCheckInterval = 5 * time.Second

// idleServer is the subset of ReactWebServer that reapIdleDaemon needs,
// allowing tests to substitute a fake with controllable activity.
type idleServer interface {
	ActiveClientCount(staleAfter time.Duration) int
	ActiveQueryCount() int
}

// reapIdleDaemon watches the daemon's web server and terminates it once the
// web UI has had no active clients and no active queries for idleTimeout.
//
// SP-136 P2: auto-started daemons should not outlive their usefulness. The
// CLI spawns the daemon with SPROUT_DAEMON_IDLE_TIMEOUT set (see
// maybeAutoStartDaemon); this goroutine then cancels the daemon's root
// context after the idle window, which unblocks the daemon's serve loop
// (`<-ctx.Done()`) and runs the normal graceful-shutdown path.
//
// Explicitly-started daemons (`sprout agent -d`) are unaffected unless the
// operator sets the env var themselves.
func reapIdleDaemon(ctx context.Context, cancel context.CancelFunc, ws idleServer, idleTimeout time.Duration) {
	// Poll faster than the idle window so we notice the last disconnect
	// promptly, but not so fast that we burn CPU on an idle daemon.
	interval := idleCheckInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var idleSince time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Active clients OR an in-flight query counts as "in use".
			if ws.ActiveClientCount(idleTimeout) > 0 || ws.ActiveQueryCount() > 0 {
				idleSince = time.Time{}
				continue
			}

			if idleSince.IsZero() {
				idleSince = time.Now()
				continue
			}
			if time.Since(idleSince) >= idleTimeout {
				console.GlyphDim.Printf("Daemon idle for %s — shutting down", idleTimeout)
				cancel()
				return
			}
		}
	}
}

// compile-time assertion: ReactWebServer satisfies idleServer.
var _ idleServer = (*webui.ReactWebServer)(nil)
