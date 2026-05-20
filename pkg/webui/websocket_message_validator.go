//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
)

// parseAndValidateMessage parses raw JSON into a validated WebSocketMessage.
// Returns an error if the message is malformed, too large, or contains invalid fields.
func parseAndValidateMessage(raw []byte) (*WebSocketMessage, error) {
	if len(raw) > maxWebSocketMessageSize {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", len(raw), maxWebSocketMessageSize)
	}

	var msg WebSocketMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if err := msg.Validate(); err != nil {
		return nil, err
	}

	return &msg, nil
}

// parseAndValidateData unmarshals and validates the embedded data payload.
// T must be a pointer to a struct type with a Validate() method.
func parseAndValidateData[T any](raw json.RawMessage, validate func(*T) error) (*T, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("missing data payload")
	}

	var data T
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid data payload: %w", err)
	}

	if err := validate(&data); err != nil {
		return nil, err
	}

	return &data, nil
}
