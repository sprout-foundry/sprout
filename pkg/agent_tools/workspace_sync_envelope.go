// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// This file defines the WebSocket envelope types for the browser-primary workspace
// sync protocol. The envelope is the wire format for all workspace sync messages
// between the browser and container.
// See roadmap/SP-046-workspace-sync-model.md for the full specification.

package tools

// WebSocket envelope type constants for workspace sync protocol.
const (
	// EnvelopeTypePatchIn is the type for browser→container patches (user edits).
	// The browser sends this when the user makes an edit in the OPFS-backed editor.
	EnvelopeTypePatchIn = "workspace.patch_in"

	// EnvelopeTypePatchOut is the type for container→browser patches (agent writes).
	// The container sends this after every tool-call file write to keep the browser
	// in sync.
	EnvelopeTypePatchOut = "workspace.patch_out"

	// EnvelopeTypeHeartbeat is the bidirectional keep-alive ping. Sent by the
	// browser every 15 seconds; the container responds with its own heartbeat.
	EnvelopeTypeHeartbeat = "workspace.heartbeat"
)

// SyncEnvelope wraps a workspace sync message for transport over WebSocket.
//
// @ts-generated — consumed by the frontend to generate a TypeScript interface.
type SyncEnvelope struct {
	// Type is one of the EnvelopeType* constants.
	Type string `json:"type"`

	// Seq is a monotonic sequence number for this direction. The browser and
	// container each maintain their own counters; the counter increments with
	// every envelope sent.
	Seq int64 `json:"seq"`

	// Payload is the structured payload, whose shape depends on Type. For
	// patch_in, it is a PatchInPayload. For patch_out, it is a PatchEvent.
	// For heartbeat, it may be nil or a HeartbeatPayload.
	Payload any `json:"payload"`

	// Error is non-empty when the envelope carries an error response.
	Error string `json:"error,omitempty"`
}

// PatchInPayload carries the data for a browser→container patch.
//
// @ts-generated — consumed by the frontend to generate a TypeScript interface.
type PatchInPayload struct {
	// Path is the workspace-relative file path (e.g. "pkg/foo/bar.go").
	Path string `json:"path"`

	// Content is the full file content after the browser edit.
	Content string `json:"content"`

	// BrowserSeq is the browser's sequence number after this edit.
	BrowserSeq int64 `json:"browser_seq"`

	// LastSyncedContainer is the last container_seq the browser has seen for
	// this file. Used for staleness detection on the server side.
	LastSyncedContainer int64 `json:"last_synced_container"`
}

// HeartbeatPayload carries the data for a heartbeat envelope.
//
// @ts-generated — consumed by the frontend to generate a TypeScript interface.
type HeartbeatPayload struct {
	// Timestamp is the server-side or client-side time of the heartbeat.
	Timestamp string `json:"timestamp"`
}

// NewPatchInEnvelope creates a new patch-in envelope for a browser→container
// sync operation.
func NewPatchInEnvelope(content, path string, browserSeq int64) *SyncEnvelope {
	return &SyncEnvelope{
		Type: EnvelopeTypePatchIn,
		Payload: PatchInPayload{
			Path:       path,
			Content:    content,
			BrowserSeq: browserSeq,
		},
	}
}

// NewPatchOutEnvelope creates a new patch-out envelope for a container→browser
// sync operation, wrapping a PatchEvent.
func NewPatchOutEnvelope(event *PatchEvent) *SyncEnvelope {
	return &SyncEnvelope{
		Type:    EnvelopeTypePatchOut,
		Payload: event,
	}
}

// NewHeartbeatEnvelope creates a new heartbeat envelope for keep-alive
// communication.
func NewHeartbeatEnvelope() *SyncEnvelope {
	return &SyncEnvelope{
		Type: EnvelopeTypeHeartbeat,
	}
}
