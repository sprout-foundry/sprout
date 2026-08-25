//go:build !js

// Package webui provides the WebSocket handler for rate-limited events.
//
// The rate_limited event is published by the agent when a provider returns
// a RateLimitError. It flows through the eventBus → WebSocket subscription
// channel automatically. This file registers the event type in the outbound
// allow-list so the message is accepted by the outbound validator.
package webui

import (
	"github.com/sprout-foundry/sprout/pkg/events"
)

func init() {
	// Register the rate_limited event type in the outbound allow-list so
	// the WebSocket outbound validator accepts it. Without this registration,
	// the event would be silently dropped in production (or panic in dev).
	RegisterOutboundMessageType(events.EventTypeRateLimited)
	// Also register the rate_limited_status type used by the frontend banner.
	RegisterOutboundMessageType("rate_limited_status")
}
