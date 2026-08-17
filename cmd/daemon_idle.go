package cmd

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/daemon"
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

// reapIdleDaemon watches the daemon's web server and socket servers and
// terminates it once none of them have had activity for idleTimeout.
//
// SP-136 P2: auto-started daemons should not outlive their usefulness. The
// CLI spawns the daemon with SPROUT_DAEMON_IDLE_TIMEOUT set (see
// maybeAutoStartDaemon); this goroutine then cancels the daemon's root
// context after the idle window, which unblocks the daemon's serve loop
// (`<-ctx.Done()`) and runs the normal graceful-shutdown path.
//
// `sockets` carries the activity trackers of the daemon's other listeners
// (agent socket, embedding socket). The daemon's liveness is not the WebUI's
// liveness: an auto-started daemon actively serving socket requests must not
// be reaped, or CLI clients silently fall back to in-process execution and
// load their own model copy — the duplication SP-136 exists to prevent.
//
// Explicitly-started daemons (`sprout agent -d`) are unaffected unless the
// operator sets the env var themselves.
func reapIdleDaemon(ctx context.Context, cancel context.CancelFunc, web idleServer, sockets []*daemon.DaemonActivity, idleTimeout time.Duration) {
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
			// Active web clients, an in-flight web query, OR socket traffic
			// counts as "in use". Socket activity uses the same window as web
			// activity: SP-136 P2's idle timeout is a single knob for the
			// whole daemon, not per-listener.
			if web.ActiveClientCount(idleTimeout) > 0 || web.ActiveQueryCount() > 0 ||
				anySocketActive(sockets, time.Now(), idleTimeout) {
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

// anySocketActive reports whether a socket server has in-flight work or
// activity within the window.
func anySocketActive(activities []*daemon.DaemonActivity, now time.Time, window time.Duration) bool {
	for _, a := range activities {
		if a != nil && !a.Idle(now, window) {
			return true
		}
	}
	return false
}

// compile-time assertion: ReactWebServer satisfies idleServer.
var _ idleServer = (*webui.ReactWebServer)(nil)
