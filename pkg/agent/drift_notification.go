package agent

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// DriftNotification handles emitting drift notifications through the event bus.
// The notification is non-blocking — the agent continues processing after emission.
type DriftNotification struct {
	detector  *DriftDetector
	eventBus  *events.EventBus // may be nil if no WebUI
	sessionID string
}

// NewDriftNotification creates a new drift notification handler.
func NewDriftNotification(detector *DriftDetector, eventBus *events.EventBus, sessionID string) *DriftNotification {
	return &DriftNotification{
		detector:  detector,
		eventBus:  eventBus,
		sessionID: sessionID,
	}
}

// NotifyDrift emits a drift detection event via the EventBus (for WebUI).
// Returns the notification data so the CLI layer can use it for display.
// This is non-blocking — it does not wait for user response.
func (n *DriftNotification) NotifyDrift(similarity float64, threshold float64) map[string]interface{} {
	n.detector.RecordDrift()

	if n.eventBus != nil {
		data := events.DriftDetectedEvent(similarity, threshold, n.sessionID)
		n.eventBus.Publish(events.EventTypeDriftDetected, data)
		return data
	}

	return map[string]interface{}{
		"similarity": similarity,
		"threshold":  threshold,
		"sessionId":  n.sessionID,
	}
}

// FormatCLIMessage returns a human-readable drift notification for CLI display.
func FormatCLIMessage(similarity float64, threshold float64) string {
	return fmt.Sprintf("\n📝 Conversation drift detected (similarity: %.2f < %.2f threshold).\n   Press Enter to continue, or 's' to start a new chat.", similarity, threshold)
}
