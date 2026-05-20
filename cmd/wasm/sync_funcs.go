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

func syncJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"setSyncEndpoint":   js.FuncOf(setSyncEndpointFunc),
		"getSyncEndpoint":   js.FuncOf(getSyncEndpointFunc),
		"applyFileMetadata": js.FuncOf(applyFileMetadataFunc),
		"onSessionMoved":    js.FuncOf(onSessionMovedFunc),
		"sessionMoved":      js.FuncOf(sessionMovedFunc),
		"startHeartbeat":    js.FuncOf(startHeartbeatFunc),
		"stopHeartbeat":     js.FuncOf(stopHeartbeatFunc),
	}
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

// Compile-time interface check — keeps the public surface stable so the
// platform repo can rely on it. (Currently unused but useful as
// documentation.)
var _ = context.TODO
