//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/txn"
)

// ETH-2 "transactional escalation": the daemon's container-side execution
// surface. The platform drives a three-phase transaction against these
// routes — POST /api/txn/push (browser→container deltas), POST /api/txn/run
// (execute a command), POST /api/txn/pull (container→browser deltas) — with
// GET /api/txn/status as the read-only preflight. Every body is a pinned
// JSON contract (docs/txn-protocol.md).
//
// The method IS the security boundary (see authTokenMiddleware): GET/HEAD is
// status-only and reachable unauthenticated; the three POSTs mutate the
// workspace and sit behind the Bearer boundary whenever SPROUT_AUTH_TOKEN is
// configured. With no token configured (localhost/dev) the middleware is a
// no-op and everything stays open — intended.
//
// Status codes follow the sync precedent: 200 for every REPORTABLE state —
// a partial apply, a failed command, a timeout, a not-a-repo directory —
// because the platform's next move depends on the payload, not the code.
// 400 for a body that is not the contract at all, 413 for an oversized one,
// 500 only for catastrophic failure.

// handleAPITxnPush applies a push manifest to the request's workspace.
func (ws *ReactWebServer) handleAPITxnPush(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var manifest txn.DeltaManifest
	if !decodeTxnBody(w, r, &manifest) {
		return
	}

	// A half-applied delta must survive the client hanging up mid-write —
	// cancelling r.Context() would abort ApplyDelta between files and leave
	// the tree in a state no later phase can reason about. Detached from
	// the request cancellation, the operation is bounded by its own caps.
	result, err := txn.ApplyDelta(context.WithoutCancel(r.Context()), ws.getWorkspaceRootForRequest(r), manifest)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "txn_push_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPITxnRun executes a command in the request's workspace.
func (ws *ReactWebServer) handleAPITxnRun(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var request txn.RunRequest
	if !decodeTxnBody(w, r, &request) {
		return
	}

	// WithoutCancel for the same reason as push, plus one more: the command
	// runs in its own process group and its own timeout, and neither may be
	// short-circuited by a browser tab closing mid-build. The timeout is
	// the only canceller.
	result, err := txn.RunCommand(context.WithoutCancel(r.Context()), ws.getWorkspaceRootForRequest(r), request)
	if err != nil {
		// An unusable workdir is the one thing that is not reportable in
		// the run shape — there is no subprocess to speak of.
		writeJSONErr(w, http.StatusBadRequest, "txn_run_invalid_workdir", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPITxnStatus serves the read-only working-tree preflight. It is the
// GET half of the /api/sync precedent: status only, never a mutation, so it
// stays on the request context and dies with the client.
func (ws *ReactWebServer) handleAPITxnStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethods(w, r, http.MethodGet, http.MethodHead) {
		return
	}

	status, err := txn.BuildStatus(r.Context(), ws.getWorkspaceRootForRequest(r))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "txn_status_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleAPITxnPull builds the pull manifest for the request's workspace.
func (ws *ReactWebServer) handleAPITxnPull(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Read-only against the tree, but the response body is assembled from
	// many file reads; a hangup halfway must not tear the encoding down.
	manifest, err := txn.BuildPull(context.WithoutCancel(r.Context()), ws.getWorkspaceRootForRequest(r))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "txn_pull_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, manifest)
}

// decodeTxnBody reads one pinned JSON body under the contract's 100 MiB
// request cap. An oversized body is a 413, a malformed one a 400 — neither
// is reportable in any response shape, so they surface as transport errors.
//
// Unknown fields are ignored rather than rejected (the sync precedent): the
// contract is pinned by the platform's parser, and refusing a field the
// container has not heard of yet would turn a forward-compatible platform
// release into a hard outage of the whole transaction surface.
func decodeTxnBody(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	body := http.MaxBytesReader(w, r.Body, txn.MaxRequestBytes)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONErr(w, http.StatusRequestEntityTooLarge, "body_too_large",
				"request exceeds the 100 MiB delta cap")
			return false
		}
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	// Reject a second JSON document: the contract is exactly one object.
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "expected a single JSON object")
		return false
	}
	return true
}
