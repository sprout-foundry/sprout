package events

import (
	"testing"
)

func TestDriftDetectedEvent(t *testing.T) {
	similarity := float32(0.55)
	threshold := float32(0.60)
	turnNumber := 5

	event := DriftDetectedEvent(similarity, threshold, turnNumber, "")

	// Verify the event has the correct fields
	if event["similarity"] != float64(similarity) {
		t.Errorf("Expected similarity %f, got %v", float64(similarity), event["similarity"])
	}

	if event["threshold"] != float64(threshold) {
		t.Errorf("Expected threshold %f, got %v", float64(threshold), event["threshold"])
	}

	if event["turn_number"] != turnNumber {
		t.Errorf("Expected turn_number %d, got %v", turnNumber, event["turn_number"])
	}
}

func TestDriftDetectedEventHighSimilarity(t *testing.T) {
	similarity := float32(0.95)
	threshold := float32(0.60)
	turnNumber := 10

	event := DriftDetectedEvent(similarity, threshold, turnNumber, "")

	// Should still include similarity even if it's above threshold
	if event["similarity"] != float64(similarity) {
		t.Errorf("Expected similarity %f, got %v", float64(similarity), event["similarity"])
	}
}

func TestDriftDetectedEventZeroTurn(t *testing.T) {
	similarity := float32(0.55)
	threshold := float32(0.60)
	turnNumber := 0

	event := DriftDetectedEvent(similarity, threshold, turnNumber, "")

	// Should handle turn 0
	if event["turn_number"] != turnNumber {
		t.Errorf("Expected turn_number %d, got %v", turnNumber, event["turn_number"])
	}
}

func TestDriftDetectedEventWithChatID(t *testing.T) {
	similarity := float32(0.55)
	threshold := float32(0.60)
	turnNumber := 5

	event := DriftDetectedEvent(similarity, threshold, turnNumber, "chat-123")

	// Should include chat_id when provided
	if event["chat_id"] != "chat-123" {
		t.Errorf("Expected chat_id 'chat-123', got %v", event["chat_id"])
	}

	// Should still have similarity
	if event["similarity"] != float64(similarity) {
		t.Errorf("Expected similarity %f, got %v", float64(similarity), event["similarity"])
	}
}

func TestDriftDetectedEventEmptyChatID(t *testing.T) {
	similarity := float32(0.55)
	threshold := float32(0.60)
	turnNumber := 5

	event := DriftDetectedEvent(similarity, threshold, turnNumber, "")

	// Should NOT include chat_id when empty
	if _, ok := event["chat_id"]; ok {
		t.Errorf("Expected chat_id to be absent when empty, got %v", event["chat_id"])
	}
}