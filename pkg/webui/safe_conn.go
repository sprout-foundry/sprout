//go:build !js

package webui

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// SafeConn wraps a WebSocket connection with write mutex and panic recovery
type SafeConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	closed  atomic.Bool
}

// Conn returns the underlying *websocket.Conn. Useful when callers need
// to register the connection in a map keyed by pointer identity (e.g.
// the chatSubscribers registry) without losing the SafeConn write
// serialization for the actual JSON traffic.
func (sc *SafeConn) Conn() *websocket.Conn {
	return sc.conn
}

// NewSafeConn creates a new safe connection wrapper
func NewSafeConn(conn *websocket.Conn) *SafeConn {
	return &SafeConn{
		conn: conn,
	}
}

// ErrOutboundDropped is returned by WriteJSON when the outbound
// allowlist (websocket_outbound_registry.go) rejected the message. The
// payload was NOT sent. Callers should check errors.Is(err, ErrOutboundDropped)
// before logging "successfully sent" — otherwise a missing allowlist
// entry produces silent drops that masquerade as successful writes (see
// terminal_websocket.go: pre-fix, every `session_created` and `output`
// frame was dropped while the handler logged successful sends, leaving
// the React terminal stuck on "Loading terminal…").
//
// Transport-level failures (connection closed, network error) still
// surface via the underlying error, distinct from this sentinel.
var ErrOutboundDropped = errors.New("webui: outbound message type not in allowlist; dropped")

// WriteJSON safely writes JSON to the WebSocket connection.
//
// SP-034-5d: outbound payloads carrying a `type` field are validated
// against the registry in websocket_outbound_registry.go. Unknown types
// panic in dev (`SPROUT_DEV=1`) so typos surface immediately, and in
// prod are dropped after logging — WriteJSON returns ErrOutboundDropped
// so the caller can tell the difference between "actually delivered"
// and "silently filtered by the registry".
func (sc *SafeConn) WriteJSON(v interface{}) error {
	if sc.closed.Load() {
		return nil // Silently ignore writes to closed connections
	}

	if msgType, ok := extractOutboundMessageType(v); ok {
		if !validateOutboundMessageType(msgType) {
			// Drop and signal — validateOutboundMessageType already
			// logged (or panicked in dev). Returning a typed sentinel
			// lets callers like the terminal session handler suppress
			// their "successfully sent" log path on a drop.
			return ErrOutboundDropped
		}
	}

	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()

	if sc.closed.Load() {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			webuiLogger.Error("WebSocket write panicked", slog.Any("panic", r))
			sc.closed.Store(true)
		}
	}()

	return sc.conn.WriteJSON(v)
}

// extractOutboundMessageType peeks at the value being written and
// pulls out a `type` field when the shape is one of the two patterns
// used across the codebase: a `map[string]interface{}` envelope, or
// the `events.UIEvent` struct (which has an exported Type field).
// Returns (typeString, true) when a type was found, ("", false)
// otherwise. The false case is permissive — the caller still writes.
func extractOutboundMessageType(v interface{}) (string, bool) {
	switch x := v.(type) {
	case map[string]interface{}:
		if t, ok := x["type"].(string); ok {
			return t, true
		}
	default:
		// Use a non-reflection path for the common UIEvent shape.
		if ev, ok := v.(interface{ GetType() string }); ok {
			return ev.GetType(), true
		}
		if ev, ok := v.(typedEvent); ok {
			return ev.Type, true
		}
	}
	return "", false
}

// typedEvent matches the events.UIEvent shape without importing it,
// avoiding a cycle while still catching the common-path writes.
type typedEvent struct {
	Type string
}

// writeDirectJSONLocked writes JSON directly to the underlying WebSocket connection,
// bypassing the closed check. Used only during panic recovery to ensure
// termination events reach the client even after the connection is marked closed.
// Has its own panic recovery since it may be called from recover() paths.
//
// Preconditions: sc.writeMu must be held by caller.
//
// Race tolerance: Close() releases sc.writeMu before calling sc.conn.Close(),
// so WritePanicError can grab the mutex after closed=true is set but while
// the underlying TCP close is in flight. gorilla/websocket's WriteJSON may
// then nil-deref on internal state. That race is recovered here silently
// (the connection is going away anyway, the panic event has nowhere useful
// to land). Panics on still-open connections — real bugs — log loudly.
func (sc *SafeConn) writeDirectJSONLocked(v interface{}) {
	wasClosed := sc.closed.Load()
	defer func() {
		if r := recover(); r != nil {
			if !wasClosed {
				webuiLogger.Error("direct WebSocket JSON write panicked", slog.Any("panic", r))
			}
		}
	}()
	if sc.conn == nil {
		return
	}
	_ = sc.conn.WriteJSON(v)
}

// Close closes the underlying connection
func (sc *SafeConn) Close() error {
	sc.writeMu.Lock()
	sc.closed.Store(true)
	sc.writeMu.Unlock()
	return sc.conn.Close()
}

// Underlying returns the underlying websocket.Conn for read operations (still need to be careful)
func (sc *SafeConn) Underlying() *websocket.Conn {
	return sc.conn
}

// WritePanicError sends a panic error event to the client. Called only from
// deferred recover() blocks — never during normal flow. The full panic value
// is logged server-side but not sent to the client to avoid leaking internal
// state (stack traces, memory addresses, struct internals).
func (sc *SafeConn) WritePanicError(sessionID, location string, r interface{}) {
	webuiLogger.Error("WebSocket panicked", slog.String("location", location), slog.String("session_id", sessionID), slog.Any("panic", r))
	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()
	sc.closed.Store(true)

	// Send error event directly so the client knows what happened.
	sc.writeDirectJSONLocked(map[string]interface{}{
		"type": "error",
		"data": map[string]string{
			"message":    fmt.Sprintf("Internal error in %s", location),
			"code":       "internal_panic",
			"session_id": sessionID,
		},
	})

	// Also send session_terminated directly so the client can tear down its UI
	// state (e.g. hide the "running" spinner). The normal write path (WriteJSON)
	// silently drops events on closed connections, and since we just set
	// sc.closed = true, only a direct write guarantees delivery.
	sc.writeDirectJSONLocked(map[string]interface{}{
		"type": "session_terminated",
		"data": map[string]interface{}{
			"session_id": sessionID,
			"status":     "error",
			"code":       "internal_panic",
			"message":    "Session terminated due to internal error",
		},
	})
}
