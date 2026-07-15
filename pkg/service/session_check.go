//go:build !js

package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// sessionCheckURL is the HTTP endpoint used to query active agent sessions.
// It is a package-level variable (not a const) so tests can override it
// with an httptest server URL.
var sessionCheckURL = serviceURL + "/api/terminal/agent-sessions"

// checkActiveSessions queries the running daemon's HTTP API to count active
// agent sessions.  Returns 0 if the daemon is not running or has no active
// sessions.  Returns 0, nil (not an error) if the daemon is unreachable.
func checkActiveSessions() (int, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(sessionCheckURL)
	if err != nil {
		// Daemon not running or unreachable — not an error for this check.
		return 0, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response: %w", err)
	}

	return parseSessionResponse(body)
}

// parseSessionResponse extracts the active session count from a JSON response
// body.  Expects a top-level "count" field (integer).  Returns 0, nil if the
// key is absent (treated as "no active sessions").
func parseSessionResponse(body []byte) (int, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	countVal, ok := data["count"]
	if !ok {
		return 0, nil
	}

	switch v := countVal.(type) {
	case float64:
		return int(v), nil
	case json.Number:
		// Note: json.Unmarshal into interface{} always produces float64 for
		// JSON numbers, so this branch is only reachable if a decoder with
		// UseNumber() is used elsewhere in the future.
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("failed to parse count: %w", err)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("unexpected type for count: %T", countVal)
	}
}
