//go:build !js

package webui

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionInfo stores metadata about a WebSocket connection
type ConnectionInfo struct {
	SessionID   string          // Unique session ID for this connection
	ClientID    string          // WebUI client/window identifier
	ChatID      string          // Chat session identifier (optional)
	Type        string          // "webui" or "terminal"
	UserID      string          // User ID extracted from trusted header (service mode)
	ConnectedAt time.Time       // When the connection was established
	Conn        *websocket.Conn // Underlying conn for registry lookups (SP-034-3c). Never written to directly; SafeConn owns the write path.
}

type contextKey string

const userIDContextKey contextKey = "userID"

// UserIDFromContext retrieves the user ID from a context.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDContextKey).(string); ok {
		return v
	}
	return ""
}

// validUserIDRegex validates user ID format: alphanumeric plus underscores, dots, @, hyphens, and colons
var validUserIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_.@:-]+$`)

// isValidUserID checks if a user ID is valid based on length and character set.
// User IDs must be non-empty, max 256 characters, and contain only safe characters.
func isValidUserID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	return validUserIDRegex.MatchString(id)
}

// ExtractUserID reads the trusted user header from the request when
// running in service mode. In local mode, it always returns an empty
// string to prevent header spoofing.
func (ws *ReactWebServer) ExtractUserID(r *http.Request) string {
	if !ws.serviceMode || ws.trustedUserHeader == "" {
		return ""
	}
	userID := strings.TrimSpace(r.Header.Get(ws.trustedUserHeader))
	if !isValidUserID(userID) {
		return ""
	}
	return userID
}

// contextWithUserID returns a new context with the user ID attached.
func (ws *ReactWebServer) contextWithUserID(ctx context.Context, r *http.Request) context.Context {
	if userID := ws.ExtractUserID(r); userID != "" {
		return context.WithValue(ctx, userIDContextKey, userID)
	}
	return ctx
}
