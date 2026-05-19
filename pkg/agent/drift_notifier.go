package agent

import (
	"context"
	"log"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

const maxDriftRejections = 3

// DriftNotifier handles the notification side of drift detection — publishing
// events for WebUI consumption and providing CLI prompt support.
type DriftNotifier struct {
	agent *Agent
}

// newDriftNotifier creates a DriftNotifier bound to the given agent.
func newDriftNotifier(a *Agent) *DriftNotifier {
	return &DriftNotifier{agent: a}
}

// CheckAndNotify runs drift detection for a completed turn and, if drift is
// detected, publishes a notification. It returns true if drift was detected.
//
// The notification is non-blocking:
//   - WebUI: publishes a drift_detected event via the event bus
//   - CLI: the caller should check GetAndClearDriftDetected() after the turn
//     completes
//
// Suppression: after maxDriftRejections (3) consecutive rejections in a
// session, detection is silently disabled for the remainder of that session.
func (n *DriftNotifier) CheckAndNotify(ctx context.Context, prompt string, turnNumber int) bool {
	if n.agent == nil {
		return false
	}

	// Check suppression: if the user has rejected drift 3+ times, stop checking
	if n.agent.state.GetDriftRejectionCount() >= maxDriftRejections {
		return false
	}

	// Resolve drift config from the agent
	config := DefaultDriftConfig()

	// Run the drift check
	result, err := CheckDrift(ctx, n.agent.embeddingMgr, n.agent.state, prompt, turnNumber, config)
	if err != nil {
		log.Printf("[drift-notifier] check failed: %v", err)
		return false
	}
	if result == nil || !result.Drifted {
		return false
	}

	// Drift detected — publish event for WebUI consumption
	if n.agent.eventBus != nil {
		n.agent.publishEvent(events.EventTypeDriftDetected, events.DriftDetectedEvent(
			result.Similarity, config.Threshold, result.TurnNumber, n.agent.GetEventChatID(),
		))
	}

	// Set the drift detected flag for CLI to poll
	n.agent.SetDriftDetected()

	return true
}

// RecordUserResponse records the user's response to a drift notification.
// If the user chose to continue (rejected the new-chat suggestion), the
// rejection counter is incremented. If the user chose to start a new chat,
// the rejection counter is reset.
func (n *DriftNotifier) RecordUserResponse(startedNewChat bool) {
	if n.agent == nil {
		return
	}
	if startedNewChat {
		n.agent.state.ResetDriftRejectionCount()
	} else {
		n.agent.state.IncrementDriftRejectionCount()
	}
}

// ShouldSuppressDrift returns true if drift notification should be suppressed
// for this session (3+ consecutive rejections).
func (n *DriftNotifier) ShouldSuppressDrift() bool {
	if n.agent == nil {
		return true
	}
	return n.agent.state.GetDriftRejectionCount() >= maxDriftRejections
}

// checkDriftAsync runs drift detection in a goroutine and publishes the
// notification event if drift is detected. It returns a channel that is
// closed when the check completes, allowing callers to wait if needed.
func (a *Agent) checkDriftAsync(prompt string, turnNumber int) <-chan struct{} {
	done := make(chan struct{}, 1)
	if a == nil || a.state == nil {
		close(done)
		return done
	}

	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		notifier := newDriftNotifier(a)
		notifier.CheckAndNotify(ctx, prompt, turnNumber)
	}()
	return done
}

// RecordDriftUserResponse records the user's response to a drift notification.
// This is the public API for both CLI and WebUI to call back into.
func (a *Agent) RecordDriftUserResponse(startedNewChat bool) {
	if a == nil {
		return
	}
	notifier := newDriftNotifier(a)
	notifier.RecordUserResponse(startedNewChat)
}

// ShouldSuppressDriftDetection returns true if drift notification should be
// suppressed for this session due to repeated rejections.
func (a *Agent) ShouldSuppressDriftDetection() bool {
	if a == nil || a.state == nil {
		return true
	}
	return a.state.GetDriftRejectionCount() >= maxDriftRejections
}

// GetDriftRejectionCount returns the current drift rejection count.
func (a *Agent) GetDriftRejectionCount() int {
	if a == nil || a.state == nil {
		return 0
	}
	return a.state.GetDriftRejectionCount()
}