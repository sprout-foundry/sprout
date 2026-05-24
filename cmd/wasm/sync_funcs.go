//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// SP-046 workspace-sync transport scaffold. The actual WebSocket / HTTP
// transport lives in the platform repo (`../platform`); this file exposes
// the hooks the platform-side code calls into from the browser. When no
// sync endpoint is configured (free-tier WASM), all the entries here are
// safe no-ops and the agent operates as a single-replica system.
//
// See roadmap/SP-046-workspace-sync-model.md for the full design.

// syncState holds the WASM-side sync configuration. Protected by mu so
// callers can mutate it from any goroutine (JS callback context).
var (
	syncMu                sync.Mutex
	syncEndpoint          string
	syncSessionMovedCB    js.Value
	syncHeartbeatActive   bool
	syncHeartbeatStop     chan struct{}
	syncHeartbeatInterval = 15 * time.Second
)

// Op queue for browser → container sync. The browser queues outbound ops
// (edits the user made) and flushes them via HTTP POST when the WebSocket
// is up.
var (
	syncOpQueue []agent.SyncOp
)

func syncJSFuncs() map[string]interface{} {
	m := map[string]interface{}{
		"setSyncEndpoint":              js.FuncOf(setSyncEndpointFunc),
		"getSyncEndpoint":              js.FuncOf(getSyncEndpointFunc),
		"applyFileMetadata":            js.FuncOf(applyFileMetadataFunc),
		"onSessionMoved":               js.FuncOf(onSessionMovedFunc),
		"sessionMoved":                 js.FuncOf(sessionMovedFunc),
		"startHeartbeat":               js.FuncOf(startHeartbeatFunc),
		"stopHeartbeat":                js.FuncOf(stopHeartbeatFunc),
		"queueSyncOp":                  js.FuncOf(queueSyncOpFunc),
		"flushSyncOps":                 js.FuncOf(flushSyncOpsFunc),
		"getSyncOpQueue":               js.FuncOf(getSyncOpQueueFunc),
		"handleWorkspacePatchConflict": js.FuncOf(handleWorkspacePatchConflictFunc),
	}
	// Include OPFS replica functions.
	for name, fn := range opfsReplicaJSFuncs() {
		m[name] = fn
	}
	return m
}

// setSyncEndpointFunc records the WebSocket URL the host page wants the
// platform-side sync transport to use. Free-tier WASM never calls this and
// the agent stays single-replica.
//
// Signature: setSyncEndpoint(url: string): {ok: true, url: string}
func setSyncEndpointFunc(_ js.Value, args []js.Value) interface{} {
	url := argString(args, 0, "")
	if url == "" {
		// Allow clearing — pass "" to disable sync.
		syncMu.Lock()
		syncEndpoint = ""
		syncMu.Unlock()
		return map[string]interface{}{"ok": true, "url": ""}
	}
	syncMu.Lock()
	syncEndpoint = url
	syncMu.Unlock()
	return map[string]interface{}{"ok": true, "url": url}
}

func getSyncEndpointFunc(_ js.Value, _ []js.Value) interface{} {
	syncMu.Lock()
	defer syncMu.Unlock()
	return syncEndpoint
}

// applyFileMetadataFunc lets the platform-side sync layer push a
// WorkspaceFileMetadata update into the running agent. The Go-side
// staleness rule consults this on every write_file call (see
// pkg/agent/workspace_sync.go).
//
// Signature: applyFileMetadata(path: string, metadataJSON: string): {ok: true}
//
// We take the metadata as a JSON string rather than a JS object so the
// transport layer can shuttle the same payload it received on the wire
// without re-stringifying it.
func applyFileMetadataFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	raw := argString(args, 1, "")
	if path == "" {
		return map[string]interface{}{"error": "path is required"}
	}
	if raw == "" {
		return map[string]interface{}{"error": "metadata JSON is required (second arg)"}
	}
	var md agent.WorkspaceFileMetadata
	if err := json.Unmarshal([]byte(raw), &md); err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("parse metadata: %v", err)}
	}
	// The agent isn't necessarily reachable from this WASM entry point
	// today — the cmd/wasm currently doesn't construct an Agent. When the
	// agent is wired up (Tier 2b), this will route through. For now we
	// keep a process-level metadata snapshot so test harnesses + future
	// agent paths can read it.
	stashFileMetadata(path, md)
	return map[string]interface{}{"ok": true, "path": path}
}

// onSessionMovedFunc registers a JS callback that fires when the platform
// notifies this browser that the user opened sprout on another device and
// took over the session. The host page typically renders an overlay
// ("Session moved to another device") and disables UI interactivity.
//
// Signature: onSessionMoved(handler: () => void): {ok: true}
//
// Calling again replaces the previous handler.
func onSessionMovedFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) == 0 || args[0].Type() != js.TypeFunction {
		return map[string]interface{}{"error": "first arg must be a function"}
	}
	syncMu.Lock()
	syncSessionMovedCB = args[0]
	syncMu.Unlock()
	return map[string]interface{}{"ok": true}
}

// sessionMovedFunc is the platform-driven counterpart to onSessionMoved:
// the WS layer calls this when it receives the "session moved" control
// message from the server. The Go side then invokes the host-registered
// callback so the page UI can respond.
//
// Signature: sessionMoved(): {ok: true} | {error: "no handler registered"}
func sessionMovedFunc(_ js.Value, _ []js.Value) interface{} {
	syncMu.Lock()
	cb := syncSessionMovedCB
	syncMu.Unlock()
	if cb.IsUndefined() || cb.IsNull() {
		return map[string]interface{}{"error": "no session-moved handler registered"}
	}
	cb.Invoke()
	return map[string]interface{}{"ok": true}
}

// startHeartbeatFunc spins up a goroutine that pings the platform every
// syncHeartbeatInterval (15s) via the JS-registered callback, which the
// platform's WS layer then forwards. The container kills long-running
// jobs after 60s of missed heartbeats (SP-046 §4).
//
// Signature: startHeartbeat(pingFn: () => void): {ok: true}
//
// Calling while already running is a no-op (the existing ticker keeps
// going); stopHeartbeat must be called first to swap the ping function.
func startHeartbeatFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) == 0 || args[0].Type() != js.TypeFunction {
		return map[string]interface{}{"error": "first arg must be a function"}
	}
	pingFn := args[0]

	syncMu.Lock()
	if syncHeartbeatActive {
		syncMu.Unlock()
		return map[string]interface{}{"ok": true, "already_running": true}
	}
	syncHeartbeatActive = true
	syncHeartbeatStop = make(chan struct{})
	stop := syncHeartbeatStop
	interval := syncHeartbeatInterval
	syncMu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				pingFn.Invoke()
			}
		}
	}()

	return map[string]interface{}{"ok": true, "interval_ms": interval.Milliseconds()}
}

func stopHeartbeatFunc(_ js.Value, _ []js.Value) interface{} {
	syncMu.Lock()
	defer syncMu.Unlock()
	if !syncHeartbeatActive {
		return map[string]interface{}{"ok": true, "was_running": false}
	}
	if syncHeartbeatStop != nil {
		close(syncHeartbeatStop)
		syncHeartbeatStop = nil
	}
	syncHeartbeatActive = false
	return map[string]interface{}{"ok": true, "was_running": true}
}

// ─── Theirs content stash ─────────────────────────────────────────
// When a workspace_patch carries conflict metadata, the browser stores
// the container's content under a .theirs path so the UI can render
// a git-style diff. This is a process-level snapshot for free-tier WASM;
// when the agent is wired up (Tier 2b), it routes through the agent.

var (
	theirsStashMu sync.RWMutex
	theirsStash   = map[string]string{}
)

// stashTheirsContent stores the "theirs" content for a conflict path.
// callers MUST pass a normalized absolute path that matches the format
// produced by the container-side CheckPatchConflict (see
// pkg/agent/workspace_sync.go). Passing relative or unnormalized paths
// will cause lookups in the UI layer to fail silently.
func stashTheirsContent(theirsPath, content string) {
	theirsStashMu.Lock()
	defer theirsStashMu.Unlock()
	theirsStash[theirsPath] = content
}

// peekTheirsContent is used by the test harness to verify a conflict
// stash landed correctly. Not exported to JS — internal only.
func peekTheirsContent(theirsPath string) (string, bool) {
	theirsStashMu.RLock()
	defer theirsStashMu.RUnlock()
	content, ok := theirsStash[theirsPath]
	return content, ok
}

// handleWorkspacePatchConflictFunc processes an incoming workspace_patch
// that carries conflict metadata. When conflict is true, it writes the
// container's content to the .theirs sibling file in the virtual
// filesystem so the browser can show a git-style diff/conflict UI.
//
// IMPORTANT: the `theirsPath` parameter must be a normalized absolute
// path that matches the format produced by the container-side
// CheckPatchConflict (see pkg/agent/workspace_sync.go). This ensures
// the browser can locate the file in the virtual filesystem for the
// diff UI. Passing a relative or unnormalized path will cause the
// conflict UI to fail silently.
//
// Signature: handleWorkspacePatchConflict(path: string, content: string, theirsPath: string): {ok: true}
func handleWorkspacePatchConflictFunc(_ js.Value, args []js.Value) interface{} {
	path := argString(args, 0, "")
	content := argString(args, 1, "")
	theirsPath := argString(args, 2, "")
	if path == "" || theirsPath == "" {
		return map[string]interface{}{"error": "path and theirsPath are required"}
	}
	// Store the theirs content so the UI can retrieve it for a git-style diff
	stashFileMetadata(theirsPath, agent.WorkspaceFileMetadata{
		ContainerSeq: 0, // marker: this is a theirs file, not a synced container write
	})
	stashTheirsContent(theirsPath, content)
	return map[string]interface{}{"ok": true, "theirs_path": theirsPath}
}

// ─── Metadata stash ──────────────────────────────────────────────
// The cmd/wasm process doesn't currently own a long-lived Agent
// instance (the embedding manager runs without one in this build). When
// the WASM build gains a real agent loop (Tier 2b), applyFileMetadata
// will route directly into Agent.SetFileMetadata. Until then we keep
// the metadata in a process-level snapshot so it's not lost.

var (
	stashMu sync.RWMutex
	stash   = map[string]agent.WorkspaceFileMetadata{}
)

func stashFileMetadata(path string, md agent.WorkspaceFileMetadata) {
	stashMu.Lock()
	defer stashMu.Unlock()
	stash[path] = md
}

// peekFileMetadata is used by the test harness to verify applyFileMetadata
// landed correctly. Not exported to JS — internal only.
func peekFileMetadata(path string) (agent.WorkspaceFileMetadata, bool) {
	stashMu.RLock()
	defer stashMu.RUnlock()
	md, ok := stash[path]
	return md, ok
}

// ApplyAllStashedMetadata is called by the agent-init path once an Agent
// exists, to replay every WorkspaceFileMetadata the platform-side sync
// pushed in before the agent was ready. Keeps free-tier WASM (no agent)
// from dropping metadata pushes silently.
func ApplyAllStashedMetadata(a *agent.Agent) {
	if a == nil {
		return
	}
	stashMu.RLock()
	defer stashMu.RUnlock()
	for path, md := range stash {
		a.SetFileMetadata(path, md)
	}
}

// ─── Sync op queue ───────────────────────────────────────────────
// The browser queues outbound ops (edits the user made) and flushes
// them via HTTP POST when the WebSocket connects.

// queueSyncOpFunc lets the platform-side sync layer push a SyncOp into the
// outbound queue. The op will be flushed to the container when
// flushSyncOps is called (typically when the WebSocket connects).
//
// Signature: queueSyncOp(opJSON: string): {ok: true, queued: number}
//
// opJSON is a JSON-encoded SyncOp object.
func queueSyncOpFunc(_ js.Value, args []js.Value) interface{} {
	raw := argString(args, 0, "")
	if raw == "" {
		return map[string]interface{}{"error": "op JSON is required"}
	}
	var op agent.SyncOp
	if err := json.Unmarshal([]byte(raw), &op); err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("parse op: %v", err)}
	}
	if op.Path == "" {
		return map[string]interface{}{"error": "op.path is required"}
	}
	syncMu.Lock()
	syncOpQueue = append(syncOpQueue, op)
	count := len(syncOpQueue)
	syncMu.Unlock()
	return map[string]interface{}{"ok": true, "queued": count}
}

// flushSyncOpsFunc POSTs all queued SyncOps to the server's /api/sync/batch
// endpoint. This is called when the WebSocket connection is established.
// After a successful flush, the queue is cleared.
//
// Signature: flushSyncOps(): Promise<{ok: true, flushed: number}>
//
// In WASM, we use js.Global().Get("fetch") to make the HTTP request since
// net/http isn't available in the WASM environment.
func flushSyncOpsFunc(_ js.Value, _ []js.Value) interface{} {
	syncMu.Lock()
	ops := make([]agent.SyncOp, len(syncOpQueue))
	copy(ops, syncOpQueue)
	syncOpQueue = nil
	endpoint := syncEndpoint
	syncMu.Unlock()

	if len(ops) == 0 {
		return js.Global().Get("Promise").Call("resolve", map[string]interface{}{
			"ok":      true,
			"flushed": 0,
		})
	}

	// If no endpoint is configured, ops are silently dropped (free-tier mode).
	if endpoint == "" {
		return js.Global().Get("Promise").Call("resolve", map[string]interface{}{
			"ok":      true,
			"flushed": len(ops),
			"note":    "no endpoint configured, ops dropped",
		})
	}

	// Encode ops as JSON
	payload, err := json.Marshal(map[string]interface{}{"ops": ops})
	if err != nil {
		// Put ops back in the queue on error
		syncMu.Lock()
		syncOpQueue = append(ops, syncOpQueue...)
		syncMu.Unlock()
		return js.Global().Get("Promise").Call("reject", fmt.Sprintf("marshal ops: %v", err))
	}

	// Build the sync batch URL from the endpoint base
	batchURL := endpoint + "/api/sync/batch"

	// Use the Fetch API via JS interop
	handler := js.FuncOf(func(this js.Value, pArgs []js.Value) interface{} {
		defer handler.Release()
		resolve := pArgs[0]
		reject := pArgs[1]

		// Create fetch request
		opts := js.Global().Get("Object").New()
		opts.Set("method", "POST")
		opts.Set("body", string(payload))
		headers := js.Global().Get("Object").New()
		headers.Set("Content-Type", "application/json")
		opts.Set("headers", headers)

		fetchPromise := js.Global().Call("fetch", batchURL, opts)

		thenHandler := js.FuncOf(func(this js.Value, responseArgs []js.Value) interface{} {
			defer thenHandler.Release()
			response := responseArgs[0]
			// Check if response is ok
			if !response.Get("ok").Bool() {
				// Put ops back on error
				syncMu.Lock()
				syncOpQueue = append(ops, syncOpQueue...)
				syncMu.Unlock()
				reject.Invoke(fmt.Sprintf("HTTP %d", response.Get("status").Int()))
				return nil
			}
			// Parse response JSON
			jsonPromise := response.Call("json")
			jsonThenHandler := js.FuncOf(func(this js.Value, jsonArgs []js.Value) interface{} {
				defer jsonThenHandler.Release()
				resolve.Invoke(map[string]interface{}{
					"ok":      true,
					"flushed": len(ops),
				})
				return nil
			})
			jsonPromise.Call("then", jsonThenHandler)
			return nil
		})

		catchHandler := js.FuncOf(func(this js.Value, catchArgs []js.Value) interface{} {
			defer catchHandler.Release()
			syncMu.Lock()
			syncOpQueue = append(ops, syncOpQueue...)
			syncMu.Unlock()
			reject.Invoke(catchArgs[0].Get("message").String())
			return nil
		})

		fetchPromise.Call("then", thenHandler).Call("catch", catchHandler)
		return nil
	})

	return js.Global().Get("Promise").New(handler)
}

// getSyncOpQueueFunc returns the current number of ops in the queue.
// Signature: getSyncOpQueue(): {count: number}
func getSyncOpQueueFunc(_ js.Value, _ []js.Value) interface{} {
	syncMu.Lock()
	defer syncMu.Unlock()
	return map[string]interface{}{"count": len(syncOpQueue)}
}

// Compile-time interface check — keeps the public surface stable so the
// platform repo can rely on it. (Currently unused but useful as
// documentation.)
var _ = context.TODO
