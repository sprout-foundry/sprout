package agent

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func TestDriftNotification_NotifyDrift_NoEventBus(t *testing.T) {
	detector := NewDriftDetector(0.60, 5)
	n := NewDriftNotification(detector, nil, "test-session")

	data := n.NotifyDrift(0.35, 0.60)

	if data["similarity"] != 0.35 {
		t.Errorf("expected similarity 0.35, got %v", data["similarity"])
	}
	if detector.DriftCount() != 1 {
		t.Errorf("expected drift count 1, got %d", detector.DriftCount())
	}
}

func TestDriftNotification_NotifyDrift_WithEventBus(t *testing.T) {
	detector := NewDriftDetector(0.60, 5)
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	n := NewDriftNotification(detector, bus, "test-session")

	data := n.NotifyDrift(0.35, 0.60)

	// Verify event was published
	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeDriftDetected {
			t.Errorf("expected event type %s, got %s", events.EventTypeDriftDetected, evt.Type)
		}
	default:
		t.Error("expected event to be published")
	}

	// Verify data returned
	if data["similarity"] != 0.35 {
		t.Errorf("expected similarity 0.35, got %v", data["similarity"])
	}
}

func TestFormatCLIMessage(t *testing.T) {
	msg := FormatCLIMessage(0.35, 0.60)
	if msg == "" {
		t.Error("expected non-empty message")
	}
}
