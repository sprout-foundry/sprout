package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// SP-065-2e: Automate event subscription opt-in tests
// ---------------------------------------------------------------------------

// TestShouldForwardEvent_AutomateBlockedByDefault verifies that automate
// events are NOT forwarded to connections that haven't opted in.
func TestShouldForwardEvent_AutomateBlockedByDefault(t *testing.T) {
	ws := &ReactWebServer{}

	connInfo := &ConnectionInfo{
		subscribedChannels: make(map[string]bool),
	}

	automateTypes := []string{
		events.EventTypeAutomateSessionStarted,
		events.EventTypeAutomateBudgetUpdate,
		events.EventTypeAutomateOutputChunk,
		events.EventTypeAutomateSessionEnded,
	}

	for _, et := range automateTypes {
		event := events.UIEvent{Type: et, Data: map[string]interface{}{}}
		assert.False(t, ws.shouldForwardEventToConnection(event, connInfo),
			"%s should be blocked without subscription", et)
	}
}

// TestShouldForwardEvent_AutomateAllowedAfterOptIn verifies that automate
// events ARE forwarded after the connection subscribes to the "automate" channel.
func TestShouldForwardEvent_AutomateAllowedAfterOptIn(t *testing.T) {
	ws := &ReactWebServer{}

	connInfo := &ConnectionInfo{
		subscribedChannels: map[string]bool{"automate": true},
	}

	automateTypes := []string{
		events.EventTypeAutomateSessionStarted,
		events.EventTypeAutomateBudgetUpdate,
		events.EventTypeAutomateOutputChunk,
		events.EventTypeAutomateSessionEnded,
	}

	for _, et := range automateTypes {
		event := events.UIEvent{Type: et, Data: map[string]interface{}{}}
		assert.True(t, ws.shouldForwardEventToConnection(event, connInfo),
			"%s should be allowed after opt-in", et)
	}
}

// TestShouldForwardEvent_NonAutomateUnaffected verifies that non-automate
// events are not affected by the channel subscription mechanism.
func TestShouldForwardEvent_NonAutomateUnaffected(t *testing.T) {
	ws := &ReactWebServer{}

	// Connection without any channel subscription
	connInfo := &ConnectionInfo{
		subscribedChannels: make(map[string]bool),
	}

	// These global events should still work (they go through the switch
	// at the end of shouldForwardEventToConnection for events without
	// client_id or chat_id).
	globalTypes := []string{
		events.EventTypeMetricsUpdate,
		events.EventTypeFileContentChanged,
		events.EventTypeDriftDetected,
	}

	for _, et := range globalTypes {
		event := events.UIEvent{Type: et, Data: map[string]interface{}{}}
		assert.True(t, ws.shouldForwardEventToConnection(event, connInfo),
			"%s should still be forwarded without subscription", et)
	}
}

// TestSubscribeData_ChannelField validates the Channel field in SubscribeData.
func TestSubscribeData_ChannelField(t *testing.T) {
	t.Run("valid channel", func(t *testing.T) {
		d := &SubscribeData{Channel: "automate"}
		assert.NoError(t, d.Validate())
		assert.Equal(t, "automate", d.Channel)
	})

	t.Run("empty channel is fine", func(t *testing.T) {
		d := &SubscribeData{Channel: ""}
		assert.NoError(t, d.Validate())
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		d := &SubscribeData{Channel: "  automate  "}
		assert.NoError(t, d.Validate())
		assert.Equal(t, "automate", d.Channel)
	})
}

// TestConnectionInfo_SubscribedChannelsDefault verifies that a nil
// subscribedChannels map safely denies all channel access.
func TestConnectionInfo_SubscribedChannelsDefault(t *testing.T) {
	ws := &ReactWebServer{}

	connInfo := &ConnectionInfo{
		subscribedChannels: nil,
	}

	event := events.UIEvent{Type: events.EventTypeAutomateSessionStarted, Data: map[string]interface{}{}}
	assert.False(t, ws.shouldForwardEventToConnection(event, connInfo),
		"automate events should be blocked when subscribedChannels is nil")
}
