package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/console"
)

// Test that the footer separator line contains no control hints
func TestFooterSeparatorNoHints(t *testing.T) {
	mockTerminal := NewMockTerminal(80, 24)
	alm := console.NewAutoLayoutManager()
	deps := console.Dependencies{Terminal: mockTerminal, Layout: alm, Events: console.NewEventBus(10), State: console.NewStateManager()}

	// Initialize layout regions
	alm.InitializeForTest(80, 24)

	fc := NewFooterComponent()
	if err := fc.Init(context.Background(), deps); err != nil {
		t.Fatalf("footer init failed: %v", err)
	}

	if err := fc.Render(); err != nil {
		t.Fatalf("footer render failed: %v", err)
	}

	// The separator is rendered with lineOffset=1 inside the footer region.
	// Get region to compute absolute Y
	region, err := fc.Layout().GetRegion("footer")
	if err != nil {
		t.Fatalf("region not found: %v", err)
	}
	sepY := region.Y + 1 // first line inside footer region is separator
	if sepY < 1 || sepY > mockTerminal.height {
		t.Fatalf("separator Y out of bounds: %d", sepY)
	}

	// Fetch the separator line content
	line := strings.TrimRight(string(mockTerminal.buffer[sepY-1]), " ")

	// Ensure no control hint tokens appear
	forbidden := []string{"Focus:", "Tab:", "Esc:", "toggle", "interrupt"}
	for _, sub := range forbidden {
		if strings.Contains(line, sub) {
			t.Fatalf("separator line should not contain %q, got: %q", sub, line)
		}
	}
}
