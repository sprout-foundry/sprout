package console

import (
	"testing"
)

func TestAutoLayoutManager_ComponentRegistrationOrder(t *testing.T) {
	// Test that components can be registered before initialization
	// and that content region is properly created
	layout := NewAutoLayoutManager()

	// Register components first (this should work after our fix)
	layout.RegisterComponent("footer", &ComponentInfo{
		Name:     "footer",
		Position: "bottom",
		Height:   4,
		Priority: 10,
		Visible:  true,
		ZOrder:   100,
	})

	layout.RegisterComponent("input", &ComponentInfo{
		Name:     "input",
		Position: "bottom",
		Height:   1,
		Priority: 20,
		Visible:  true,
		ZOrder:   90,
	})

	// Initialize for testing with fake terminal size
	layout.InitializeForTest(80, 24)

	// Test if content region is now available
	contentRegion, err := layout.GetContentRegion()
	if err != nil {
		t.Fatalf("GetContentRegion failed after fix: %v", err)
	}

	// Verify the content region has expected properties
	if contentRegion.Y != 0 {
		t.Errorf("Expected content region Y=0, got Y=%d", contentRegion.Y)
	}

    // With 24 lines total: footer (4) + input (1) + gaps (2) = 7 lines at bottom
    // Content should be 24 - 7 = 17 lines
    expectedHeight := 24 - 4 - 1 - 2 // terminal - footer - input - gaps
	if contentRegion.Height != expectedHeight {
		t.Errorf("Expected content region height=%d, got height=%d", expectedHeight, contentRegion.Height)
	}

	// Test scroll region calculation
	top, bottom := layout.GetScrollRegion()
    expectedTop := 1     // 1-based
    expectedBottom := 17 // content region height with gaps

	if top != expectedTop {
		t.Errorf("Expected scroll region top=%d, got top=%d", expectedTop, top)
	}

	if bottom != expectedBottom {
		t.Errorf("Expected scroll region bottom=%d, got bottom=%d", expectedBottom, bottom)
	}
}

func TestAutoLayoutManager_EmptyComponentsInitialization(t *testing.T) {
	// Test that initialization without components doesn't crash
	layout := NewAutoLayoutManager()

	// Initialize without any components
	layout.InitializeForTest(80, 24)

	// Should still have a content region that spans the full terminal
	contentRegion, err := layout.GetContentRegion()
	if err != nil {
		t.Fatalf("GetContentRegion failed with no components: %v", err)
	}

	// With no other components, content should span full terminal
	if contentRegion.Height != 24 {
		t.Errorf("Expected full terminal height=24, got height=%d", contentRegion.Height)
	}
}
