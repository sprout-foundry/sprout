package agent

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// drift_detection_test.go additions — edge cases
// ---------------------------------------------------------------------------

func TestCheckDrift_NilCurrentEmbedding_DriftDetected(t *testing.T) {
	d := NewDriftDetector(0.60, 5)
	intent := []float32{1, 0, 0, 0}

	// Nil current embedding yields similarity 0, which is below threshold → drift
	isDrift, sim := d.CheckDrift(intent, nil)
	if !isDrift {
		t.Error("isDrift = false, want true (nil current embedding → similarity 0 < threshold)")
	}
	if sim != 0 {
		t.Errorf("similarity = %v, want 0 (nil current should yield 0)", sim)
	}
}

func TestCheckDrift_EmptyCurrentEmbedding_DriftDetected(t *testing.T) {
	d := NewDriftDetector(0.60, 5)
	intent := []float32{1, 0, 0, 0}

	// Empty current embedding yields similarity 0, which is below threshold → drift
	isDrift, sim := d.CheckDrift(intent, []float32{})
	if !isDrift {
		t.Error("isDrift = false, want true (empty current embedding → similarity 0 < threshold)")
	}
	if sim != 0 {
		t.Errorf("similarity = %v, want 0", sim)
	}
}

func TestCheckDrift_BothNil_NoOp(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	isDrift, sim := d.CheckDrift(nil, nil)
	if isDrift {
		t.Error("isDrift = true, want false")
	}
	if sim != 0 {
		t.Errorf("similarity = %v, want 0", sim)
	}
}

func TestCheckDrift_PartiallySimilarVectors(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	// 45-degree angle vectors → cosine similarity = cos(45°) ≈ 0.707
	a := []float32{1, 0}
	b := []float32{1, 1} // not normalized, but CosineSimilarity normalizes

	isDrift, sim := d.CheckDrift(a, b)

	// sim should be close to 0.707, which is > 0.60, so no drift
	if isDrift {
		t.Errorf("isDrift = true, want false (similarity ≈ %.4f >= 0.60)", sim)
	}
	if sim < 0.69 || sim > 0.72 {
		t.Errorf("similarity = %.4f, expected ≈ 0.707", sim)
	}
}

func TestCheckDrift_ExactThreshold(t *testing.T) {
	// When similarity equals the threshold exactly, it is NOT drift
	// (drift is similarity < threshold, not <=)
	d := NewDriftDetector(0.50, 5)

	// Identical vectors → similarity = 1.0, well above threshold
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}

	isDrift, sim := d.CheckDrift(a, b)
	if isDrift {
		t.Errorf("isDrift = true when similarity = %.4f equals or exceeds threshold", sim)
	}
}

func TestShouldCheck_ZeroTurn(t *testing.T) {
	d := NewDriftDetector(0.60, 5)
	if d.ShouldCheck(0) {
		t.Error("ShouldCheck(0) = true, want false")
	}
}

func TestShouldCheck_NegativeTurn(t *testing.T) {
	d := NewDriftDetector(0.60, 5)
	if d.ShouldCheck(-1) {
		t.Error("ShouldCheck(-1) = true, want false")
	}
}

func TestRecordAcceptance_BeforeAnyRejection(t *testing.T) {
	d := NewDriftDetector(0.60, 5)

	// Calling acceptance when no rejections have occurred should be safe
	d.RecordAcceptance()
	if d.RejectionCount() != 0 {
		t.Errorf("RejectionCount() = %d, want 0", d.RejectionCount())
	}
	if d.IsSuppressed() {
		t.Error("should not be suppressed")
	}
}

// ---------------------------------------------------------------------------
// drift_notification_test.go additions — expanded coverage
// ---------------------------------------------------------------------------

func TestDriftNotification_NotifyDrift_DataFields(t *testing.T) {
	detector := NewDriftDetector(0.60, 5)
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	n := NewDriftNotification(detector, bus, "session-abc")

	data := n.NotifyDrift(0.25, 0.60)

	// Verify returned data map
	if data["similarity"] != 0.25 {
		t.Errorf("expected similarity 0.25, got %v", data["similarity"])
	}
	if data["threshold"] != 0.60 {
		t.Errorf("expected threshold 0.60, got %v", data["threshold"])
	}
	if data["sessionId"] != "session-abc" {
		t.Errorf("expected sessionId 'session-abc', got %v", data["sessionId"])
	}

	// Verify event fields
	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeDriftDetected {
			t.Errorf("expected event type %s, got %s", events.EventTypeDriftDetected, evt.Type)
		}
		payload, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event data should be map[string]interface{}, got %T", evt.Data)
		}
		if payload["similarity"] != 0.25 {
			t.Errorf("event similarity = %v, want 0.25", payload["similarity"])
		}
		if payload["threshold"] != 0.60 {
			t.Errorf("event threshold = %v, want 0.60", payload["threshold"])
		}
		if payload["sessionId"] != "session-abc" {
			t.Errorf("event sessionId = %v, want session-abc", payload["sessionId"])
		}
		// Verify timestamp is present and valid RFC3339
		ts, ok := payload["timestamp"].(string)
		if !ok {
			t.Error("event timestamp should be a string")
		} else if ts == "" {
			t.Error("event timestamp should not be empty")
		}
		// Verify options
		options, ok := payload["options"].([]string)
		if !ok {
			t.Error("event options should be []string")
		} else {
			if len(options) != 2 || options[0] != "continue" || options[1] != "new_chat" {
				t.Errorf("event options = %v, want [continue, new_chat]", options)
			}
		}
	default:
		t.Error("expected event to be published on event bus")
	}
}

func TestDriftNotification_NotifyDrift_NoEventBus_ReturnsFallbackData(t *testing.T) {
	detector := NewDriftDetector(0.60, 5)
	n := NewDriftNotification(detector, nil, "no-bus-session")

	data := n.NotifyDrift(0.40, 0.65)

	// Should still return basic data without event bus
	if data["similarity"] != 0.40 {
		t.Errorf("expected similarity 0.40, got %v", data["similarity"])
	}
	if data["threshold"] != 0.65 {
		t.Errorf("expected threshold 0.65, got %v", data["threshold"])
	}
	if data["sessionId"] != "no-bus-session" {
		t.Errorf("expected sessionId 'no-bus-session', got %v", data["sessionId"])
	}
	// Should NOT have timestamp when no event bus (raw map doesn't include it)
	if _, hasTs := data["timestamp"]; hasTs {
		t.Error("fallback data should not have timestamp field")
	}
}

func TestDriftNotification_NotifyDrift_IncrementsDriftCount(t *testing.T) {
	detector := NewDriftDetector(0.60, 5)
	n := NewDriftNotification(detector, nil, "test")

	for i := 1; i <= 5; i++ {
		n.NotifyDrift(0.30, 0.60)
		if detector.DriftCount() != i {
			t.Errorf("after %d calls, DriftCount() = %d, want %d", i, detector.DriftCount(), i)
		}
	}
}

func TestFormatCLIMessage_ContainsValues(t *testing.T) {
	msg := FormatCLIMessage(0.35, 0.60)

	if !strings.Contains(msg, "0.35") {
		t.Error("message should contain similarity value")
	}
	if !strings.Contains(msg, "0.60") {
		t.Error("message should contain threshold value")
	}
	if !strings.Contains(msg, "drift") {
		t.Error("message should contain 'drift'")
	}
}

func TestFormatCLIMessage_DifferentValues(t *testing.T) {
	msg := FormatCLIMessage(0.10, 0.90)
	if !strings.Contains(msg, "0.10") {
		t.Error("message should contain similarity 0.10")
	}
	if !strings.Contains(msg, "0.90") {
		t.Error("message should contain threshold 0.90")
	}
}

// ---------------------------------------------------------------------------
// DriftDetectedEvent tests (pkg/events)
// ---------------------------------------------------------------------------

func TestDriftDetectedEvent_Fields(t *testing.T) {
	data := events.DriftDetectedEvent(0.42, 0.60, "sess-123")

	if data["similarity"] != 0.42 {
		t.Errorf("similarity = %v, want 0.42", data["similarity"])
	}
	if data["threshold"] != 0.60 {
		t.Errorf("threshold = %v, want 0.60", data["threshold"])
	}
	if data["sessionId"] != "sess-123" {
		t.Errorf("sessionId = %v, want sess-123", data["sessionId"])
	}
	if data["timestamp"] == nil || data["timestamp"] == "" {
		t.Error("timestamp should be non-empty")
	}
	options, ok := data["options"].([]string)
	if !ok {
		t.Error("options should be []string")
	} else if len(options) != 2 {
		t.Errorf("options length = %d, want 2", len(options))
	}
}
