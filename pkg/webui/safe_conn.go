package webui

import (
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// SafeConn wraps a WebSocket connection with write mutex and panic recovery
type SafeConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
	closed  bool
}

// NewSafeConn creates a new safe connection wrapper
func NewSafeConn(conn *websocket.Conn) *SafeConn {
	return &SafeConn{
		conn:   conn,
		closed: false,
	}
}

// WriteJSON safely writes JSON to the WebSocket connection
func (sc *SafeConn) WriteJSON(v interface{}) error {
	if sc.closed {
		return nil // Silently ignore writes to closed connections
	}

	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()

	if sc.closed {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebSocket write panic recovered: %v", r)
			sc.closed = true
		}
	}()

	return sc.conn.WriteJSON(v)
}

// writeDirectJSON writes JSON directly to the underlying WebSocket connection,
// bypassing the closed check. Used only during panic recovery to ensure
// termination events reach the client even after the connection is marked closed.
// Has its own panic recovery since it may be called from recover() paths.
//
// Preconditions: sc.writeMu must be held by caller.
func (sc *SafeConn) writeDirectJSON(v interface{}) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("WebSocket writeDirectJSON panicked: %v", r)
		}
	}()
	_ = sc.conn.WriteJSON(v)
}

// Close closes the underlying connection
func (sc *SafeConn) Close() error {
	sc.writeMu.Lock()
	sc.closed = true
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
	log.Printf("WebSocket panic in %s (session %s): %v", location, sessionID, r)
	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()
	sc.closed = true

	// Send error event directly so the client knows what happened.
	sc.writeDirectJSON(map[string]interface{}{
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
	sc.writeDirectJSON(map[string]interface{}{
		"type": "session_terminated",
		"data": map[string]interface{}{
			"session_id": sessionID,
			"status":     "error",
			"code":       "internal_panic",
			"message":    "Session terminated due to internal error",
		},
	})
}
