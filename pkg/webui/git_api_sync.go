//go:build !js

package webui

import (
	"context"
	"net/http"
	"strconv"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

// handleAPISync serves the ETH-1 sync-on-resume reconciliation report for
// the request's workspace: the pinned git.SyncReport JSON contract (same
// object `sprout sync` prints) so the platform can probe a resumed
// container over the daemon's HTTP API instead of shelling into it.
//
// The method IS the security boundary (see authTokenMiddleware):
//
//	GET / HEAD → status ONLY, never a pull. The pull query parameter is
//	             deliberately ignored on read requests: the auth
//	             middleware passes every GET/HEAD through
//	             unauthenticated, so honoring pull=1 here would let an
//	             unauthenticated caller fetch from the network and write
//	             the working tree whenever SPROUT_AUTH_TOKEN is set.
//	POST       → pull allowed. Mutating /api/* methods sit behind the
//	             Bearer boundary when SPROUT_AUTH_TOKEN is configured;
//	             with no token configured (localhost/dev) the middleware
//	             is a no-op and both methods stay open — intended.
//
// Query parameters (POST only, for symmetry with `sprout sync --pull`):
//
//	pull=0 | pull=false  → degrade to status only, never touch the repo
//
// A POST with no pull parameter pulls (that is what the method means);
// an unparsable value is left at that default.
//
// Status codes: 200 for every reportable state — including not-a-repo and a
// refused pull (reported as pull.result="error") — and 500 only for
// catastrophic failure (unreadable workspace root, broken git).
func (ws *ReactWebServer) handleAPISync(w http.ResponseWriter, r *http.Request) {
	// HEAD keeps proxies/health probes happy without a body copy.
	if !requireMethods(w, r, http.MethodGet, http.MethodHead, http.MethodPost) {
		return
	}

	attemptPull := r.Method == http.MethodPost
	if attemptPull {
		if raw := r.URL.Query().Get("pull"); raw != "" {
			if parsed, err := strconv.ParseBool(raw); err == nil {
				attemptPull = parsed
			}
		}
	}

	// A pull is a network fetch plus a working-tree write. It must survive
	// the client hanging up mid-pull — cancelling r.Context() SIGKILLs git
	// and can strand .git/index.lock — so it runs detached from the request
	// cancellation, still bounded per git invocation by git.SyncGitTimeout
	// (same pattern as computeBootstrapSync's budget). Status-only reads
	// stay on the request context and die with the client.
	syncCtx := r.Context()
	if attemptPull {
		syncCtx = context.WithoutCancel(r.Context())
	}

	report, err := gitops.RunSync(syncCtx, ws.getWorkspaceRootForRequest(r), attemptPull)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "sync_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, report)
}
