//go:build !js

// huma_sync_txn.go holds the Huma operations and their thin handlers for the
// sync/txn family: the /api/txn/* (container-side git transaction) and
// /api/sync* (workspace sync) routes, plus the /api/lifecycle/* routes the
// platform drives alongside them (registered via registerSyncTxnHumaOperations
// from registerHumaOperations in huma_routes.go).
//
// Method semantics are load-bearing and are preserved by registering exactly
// the methods the plain handlers accept:
//   - /api/txn/status: the GET operation (a Go ServeMux "GET" pattern also
//     matches HEAD) is the read-only preflight; it is unauthenticated through
//     the auth middleware. The three /api/txn POSTs mutate or execute.
//   - /api/sync: the GET operation (plus HEAD) is status-only; the POST
//     operation is the only method that may pull.
//
// Each handler drives the existing plain handler through the live ResponseWriter
// and returns a no-op writtenResponseOutput, so the response bytes are unchanged.
package webui

import (
	"context"
	"net/http"

	huma "github.com/danielgtaylor/huma/v2"
)

// registerSyncTxnHumaOperations registers the sync/txn-family Huma operations.
func registerSyncTxnHumaOperations(api huma.API, ws *ReactWebServer) {
	huma.Register(api, huma.Operation{
		OperationID: "txnPush",
		Method:      http.MethodPost,
		Path:        "/api/txn/push",
		Summary:     "Apply a push delta manifest to the workspace.",
		Description: "Applies the browser→container delta manifest in the body to the request's workspace. A partial apply is still reported (200) with the per-file outcomes so the platform can reason about it.",
		Tags:        []string{"sync/txn"},
	}, ws.txnPushHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "txnRun",
		Method:      http.MethodPost,
		Path:        "/api/txn/run",
		Summary:     "Execute a command in the workspace.",
		Description: "Executes the command in the body in the request's workspace, in its own process group and bounded by its own timeout so a client hang-up cannot strand it.",
		Tags:        []string{"sync/txn"},
	}, ws.txnRunHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "txnStatus",
		Method:      http.MethodGet,
		Path:        "/api/txn/status",
		Summary:     "Report the workspace's working-tree preflight.",
		Description: "Returns the read-only working-tree preflight for the request's workspace. This is the read-only half of the transaction surface: it is reachable unauthenticated (GET/HEAD pass through the auth middleware) and never mutates.",
		Tags:        []string{"sync/txn"},
	}, ws.txnStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "txnPull",
		Method:      http.MethodPost,
		Path:        "/api/txn/pull",
		Summary:     "Build the pull delta manifest.",
		Description: "Builds the container→browser delta manifest for the request's workspace (a read of the tree that is detached from request cancellation so a client hang-up cannot tear the encoding down).",
		Tags:        []string{"sync/txn"},
	}, ws.txnPullHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "syncStatus",
		Method:      http.MethodGet,
		Path:        "/api/sync",
		Summary:     "Report the workspace git reconciliation status.",
		Description: "Returns the workspace git reconciliation report (the same JSON `sprout sync` prints). GET/HEAD are status-only and never pull: the pull query parameter is deliberately ignored on read requests because they pass through the auth middleware unauthenticated.",
		Tags:        []string{"sync/txn"},
	}, ws.syncStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "syncPull",
		Method:      http.MethodPost,
		Path:        "/api/sync",
		Summary:     "Pull and reconcile the workspace git state.",
		Description: "Returns the workspace git reconciliation report and, by default, pulls (POST is the only method that may pull). `pull=0` degrades the call to status-only. The pull runs detached from request cancellation so a client hang-up cannot strand git.",
		Tags:        []string{"sync/txn"},
	}, ws.syncPullHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "syncOp",
		Method:      http.MethodPost,
		Path:        "/api/sync/op",
		Summary:     "Apply a single file-sync operation.",
		Description: "Applies a single file-sync operation (the body's `op`) to the workspace, updating the agent's tracked file metadata. Conflicts are reported with 409.",
		Tags:        []string{"sync/txn"},
	}, ws.syncOpHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "syncBatch",
		Method:      http.MethodPost,
		Path:        "/api/sync/batch",
		Summary:     "Apply a batch of file-sync operations.",
		Description: "Applies a batch of file-sync operations (the body's `ops`) to the workspace and returns the per-operation results.",
		Tags:        []string{"sync/txn"},
	}, ws.syncBatchHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "syncAgentStatus",
		Method:      http.MethodGet,
		Path:        "/api/sync/status",
		Summary:     "Report the agent's file-sync state.",
		Description: "Returns the current file-sync state (the agent's tracked files) for the client's workspace.",
		Tags:        []string{"sync/txn"},
	}, ws.syncAgentStatusHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "lifecycleActivity",
		Method:      http.MethodGet,
		Path:        "/api/lifecycle/activity",
		Summary:     "Report whether the workspace is busy and when it was last used.",
		Description: "Returns busy (any in-flight query, running automation or background task, or a prepare-stop checkpoint in progress), the busy reasons, the active query count, the last user and agent activity timestamps (RFC 3339 UTC, null when none since start) and whether the daemon still accepts work. Read-only; authenticated like GET /api/sync.",
		Tags:        []string{"sync/txn"},
	}, ws.lifecycleActivityHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "lifecyclePrepareStop",
		Method:      http.MethodPost,
		Path:        "/api/lifecycle/prepare-stop",
		Summary:     "Drain the workspace before it stops.",
		Description: "Bounded by deadline_seconds (default 60): stops accepting new work, interrupts in-flight runs (cancelling any left at the deadline), persists chat sessions, checkpoints uncommitted work to refs/sprout/wip/<unix-ts> and refs/sprout/wip/latest without touching HEAD, the index or the working tree, and syncs the filesystem. Idempotent: later calls return the first call's result.",
		Tags:        []string{"sync/txn"},
	}, ws.lifecyclePrepareStopHumaHandler)

	huma.Register(api, huma.Operation{
		OperationID: "lifecycleNotice",
		Method:      http.MethodPost,
		Path:        "/api/lifecycle/notice",
		Summary:     "Warn connected clients that the workspace will stop.",
		Description: "Relays a workspace_stopping event ({reason, at, deadline_seconds}) to every connected client so the UI can warn before a timer-driven stop. Does not change daemon state.",
		Tags:        []string{"sync/txn"},
	}, ws.lifecycleNoticeHumaHandler)
}

// lifecycleActivityHumaHandler is the Huma handler for GET /api/lifecycle/activity.
func (ws *ReactWebServer) lifecycleActivityHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPILifecycleActivity(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// lifecyclePrepareStopHumaHandler is the Huma handler for POST /api/lifecycle/prepare-stop.
func (ws *ReactWebServer) lifecyclePrepareStopHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPILifecyclePrepareStop(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// lifecycleNoticeHumaHandler is the Huma handler for POST /api/lifecycle/notice.
func (ws *ReactWebServer) lifecycleNoticeHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPILifecycleNotice(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// txnPushHumaHandler is the Huma handler for POST /api/txn/push.
func (ws *ReactWebServer) txnPushHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPITxnPush(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// txnRunHumaHandler is the Huma handler for POST /api/txn/run.
func (ws *ReactWebServer) txnRunHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPITxnRun(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// txnStatusHumaHandler is the Huma handler for GET (and HEAD, via the Go
// ServeMux GET pattern) /api/txn/status.
func (ws *ReactWebServer) txnStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPITxnStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// txnPullHumaHandler is the Huma handler for POST /api/txn/pull.
func (ws *ReactWebServer) txnPullHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPITxnPull(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// syncStatusHumaHandler is the Huma handler for GET (and HEAD) /api/sync — the
// status-only half. The POST half is a separate operation (syncPullHumaHandler)
// because POST is the only method that may pull.
func (ws *ReactWebServer) syncStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISync(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// syncPullHumaHandler is the Huma handler for POST /api/sync — the only method
// that may pull.
func (ws *ReactWebServer) syncPullHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISync(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// syncOpHumaHandler is the Huma handler for POST /api/sync/op.
func (ws *ReactWebServer) syncOpHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISyncOp(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// syncBatchHumaHandler is the Huma handler for POST /api/sync/batch.
func (ws *ReactWebServer) syncBatchHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISyncBatch(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}

// syncAgentStatusHumaHandler is the Huma handler for GET /api/sync/status.
func (ws *ReactWebServer) syncAgentStatusHumaHandler(ctx context.Context, in *humaRequestInput) (*writtenResponseOutput, error) {
	ws.handleAPISyncStatus(in.Resp, in.Req)
	return &writtenResponseOutput{Body: noopWrittenResponse}, nil
}
