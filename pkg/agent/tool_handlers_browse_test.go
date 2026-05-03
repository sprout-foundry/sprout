package agent

import (
	"testing"
)

func TestParseBrowseSteps_Empty(t *testing.T) {
	t.Parallel()
	steps, err := parseBrowseSteps([]interface{}{})
	if err != nil {
		t.Fatalf("expected no error for empty steps, got %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("expected 0 steps, got %d", len(steps))
	}
}

func TestParseBrowseSteps_NilSlice(t *testing.T) {
	t.Parallel()
	steps, err := parseBrowseSteps(nil)
	if err != nil {
		t.Fatalf("expected no error for nil steps, got %v", err)
	}
	if steps == nil || len(steps) != 0 {
		t.Errorf("expected empty steps, got %v", steps)
	}
}

func TestParseBrowseSteps_ValidSteps(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"action":   "click",
			"selector": "#submit-btn",
		},
		map[string]interface{}{
			"action":   "type",
			"selector": "#username",
			"value":    "admin",
		},
		map[string]interface{}{
			"action": "wait",
			"millis": float64(5000),
		},
	}

	steps, err := parseBrowseSteps(rawSteps)
	if err != nil {
		t.Fatalf("expected no error for valid steps, got %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}

	if steps[0].Action != "click" {
		t.Errorf("steps[0].Action = %q, expected 'click'", steps[0].Action)
	}
	if steps[0].Selector != "#submit-btn" {
		t.Errorf("steps[0].Selector = %q, expected '#submit-btn'", steps[0].Selector)
	}

	if steps[1].Action != "type" {
		t.Errorf("steps[1].Action = %q, expected 'type'", steps[1].Action)
	}
	if steps[1].Value != "admin" {
		t.Errorf("steps[1].Value = %q, expected 'admin'", steps[1].Value)
	}

	if steps[2].Action != "wait" {
		t.Errorf("steps[2].Action = %q, expected 'wait'", steps[2].Action)
	}
	if steps[2].Millis != 5000 {
		t.Errorf("steps[2].Millis = %d, expected 5000", steps[2].Millis)
	}
}

func TestParseBrowseSteps_MissingAction(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"selector": "#btn",
		},
	}

	_, err := parseBrowseSteps(rawSteps)
	if err == nil {
		t.Fatal("expected error for missing action")
	}
	if err.Error() != "browse_url steps[0] requires action" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseBrowseSteps_EmptyAction(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"action":   "",
			"selector": "#btn",
		},
	}

	_, err := parseBrowseSteps(rawSteps)
	if err == nil {
		t.Fatal("expected error for empty action")
	}
}

func TestParseBrowseSteps_WhitespaceOnlyAction(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"action":   "   ",
			"selector": "#btn",
		},
	}

	_, err := parseBrowseSteps(rawSteps)
	if err == nil {
		t.Fatal("expected error for whitespace-only action")
	}
}

func TestParseBrowseSteps_NonMapStep(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		"not a map",
	}

	_, err := parseBrowseSteps(rawSteps)
	if err == nil {
		t.Fatal("expected error for non-map step")
	}
}

func TestParseBrowseSteps_MixedInvalidAndValid(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"selector": "#btn", // missing action
		},
		map[string]interface{}{
			"action":   "click",
			"selector": "#valid",
		},
	}

	_, err := parseBrowseSteps(rawSteps)
	if err == nil {
		t.Fatal("expected error for first step missing action")
	}
}

func TestParseBrowseSteps_NumbersAndBooleans(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"action":   "click",
			"selector": "#btn",
			"count":    float64(3),
		},
	}

	steps, err := parseBrowseSteps(rawSteps)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Action != "click" {
		t.Errorf("expected action 'click', got %q", steps[0].Action)
	}
}

func TestParseBrowseSteps_UnmarshalableStep(t *testing.T) {
	t.Parallel()
	// Use a channel which cannot be marshaled to JSON
	rawSteps := []interface{}{
		map[string]interface{}{
			"action": "click",
			"bad":    make(chan int),
		},
	}

	_, err := parseBrowseSteps(rawSteps)
	if err == nil {
		t.Fatal("expected error for unmarshalable step")
	}
}

func TestParseBrowseSteps_BrowseStepTypeFields(t *testing.T) {
	t.Parallel()
	rawSteps := []interface{}{
		map[string]interface{}{
			"action":   "screenshot",
			"selector": "#image",
		},
	}

	steps, err := parseBrowseSteps(rawSteps)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Action != "screenshot" {
		t.Errorf("expected action 'screenshot', got %q", steps[0].Action)
	}
	if steps[0].Selector != "#image" {
		t.Errorf("expected selector '#image', got %q", steps[0].Selector)
	}
}
