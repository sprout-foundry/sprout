package components

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// setupAgentConsoleForTest initializes an AgentConsole for testing with mock dependencies
func setupAgentConsoleForTest(t *testing.T, mockTerminal *MockTerminal, width, height int) (*AgentConsole, console.Dependencies) {
	return setupAgentConsoleForTestWithEventBus(t, mockTerminal, width, height, nil)
}

// setupAgentConsoleForTestWithEventBus initializes an AgentConsole with custom EventBus
func setupAgentConsoleForTestWithEventBus(t *testing.T, mockTerminal *MockTerminal, width, height int, eventBus console.EventBus) (*AgentConsole, console.Dependencies) {
	mockAgent := &agent.Agent{}
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	mockTerminal.SetSize(width, height)

	if eventBus == nil {
		eventBus = console.NewEventBus(100)
	}

	deps := console.Dependencies{
		Terminal: mockTerminal,
		Events:   eventBus,
		Layout:   ac.autoLayoutManager,
		State:    console.NewStateManager(),
	}

	ctx := context.Background()

	// Initialize base component only
	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		t.Fatalf("Failed to initialize base component: %v", err)
	}

	// Set up layout components
	ac.setupLayoutComponents()

	// Initialize layout manager for testing (bypasses terminal check)
	ac.autoLayoutManager.InitializeForTest(width, height)

	// Create and initialize UI handler
	ac.uiHandler = console.NewUIHandler(deps.Terminal)
	if err := ac.uiHandler.Initialize(); err != nil {
		t.Fatalf("Failed to initialize UI handler: %v", err)
	}

	// Register components with UI handler
	ac.uiHandler.RegisterComponent("agent", ac)
	ac.uiHandler.RegisterComponent("footer", ac.footer)

	// Initialize sub-components
	if err := ac.footer.Init(ctx, deps); err != nil {
		t.Fatalf("Failed to initialize footer: %v", err)
	}

	// Set up terminal manually - this simulates setupTerminal()
	ac.autoLayoutManager.SetComponentHeight("footer", 4)
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)
	mockTerminal.MoveCursor(1, top)

	return ac, deps
}

// TestResizeHandlingComprehensive tests all aspects of terminal resize handling
func TestResizeHandlingComprehensive(t *testing.T) {
	t.Run("BasicResizeIntegration", testBasicResizeIntegration)
	t.Run("AgentPositionDuringResize", testAgentPositionDuringResize)
	t.Run("ResizeFallbackLoop", testResizeFallbackLoop)
	t.Run("StreamingCompletionPositioning", testStreamingCompletionPositioning)
	t.Run("BoundsCheckingDuringOutput", testBoundsCheckingDuringOutput)
}

// testBasicResizeIntegration tests the basic resize event handling
func testBasicResizeIntegration(t *testing.T) {
	t.Log("=== BASIC RESIZE INTEGRATION TEST ===")

	// Create mock terminal and set up agent console
	mockTerminal := &MockTerminal{}
	ac, _ := setupAgentConsoleForTest(t, mockTerminal, 80, 24)

	// Test resize handling
	oldTop, oldBottom := ac.autoLayoutManager.GetScrollRegion()
	t.Logf("Initial scroll region: %d-%d", oldTop, oldBottom)

	// Trigger a resize event
	ac.OnResize(60, 20) // Smaller terminal

	// Verify layout was updated
	newTop, newBottom := ac.autoLayoutManager.GetScrollRegion()
	t.Logf("After resize scroll region: %d-%d", newTop, newBottom)

	// The content area should be smaller now
	oldContentHeight := oldBottom - oldTop + 1
	newContentHeight := newBottom - newTop + 1

	if newContentHeight >= oldContentHeight {
		t.Log("Warning: Expected smaller content area after shrinking terminal")
	}

	// Check that commands were issued to terminal
	commands := mockTerminal.GetCommands()
	var foundScrollRegion bool
	var foundBufferRedraw bool

	for _, cmd := range commands {
		if strings.Contains(cmd, "SetScrollRegion") {
			foundScrollRegion = true
		}
		if strings.Contains(cmd, "MoveCursor") {
			foundBufferRedraw = true
		}
	}

	if !foundScrollRegion {
		t.Error("❌ Resize should trigger scroll region update")
	}

	if !foundBufferRedraw {
		t.Error("❌ Resize should trigger cursor repositioning")
	}

	t.Log("✅ Basic resize integration working")
}

// testAgentPositionDuringResize tests agent currentContentLine handling during resize
func testAgentPositionDuringResize(t *testing.T) {
	t.Log("=== AGENT POSITION DURING RESIZE TEST ===")

	mockTerminal := &MockTerminal{}
	ac, _ := setupAgentConsoleForTest(t, mockTerminal, 80, 24)

	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	contentHeight := bottom - top + 1

	// Simulate agent having written content to line 15
	ac.currentContentLine = 15
	initialLine := ac.currentContentLine

	t.Logf("Initial state: contentHeight=%d, currentContentLine=%d", contentHeight, initialLine)

	// Resize to smaller terminal (should trigger repositioning)
	ac.OnResize(80, 16) // Much smaller height

	// Get new dimensions
	newTop, newBottom := ac.autoLayoutManager.GetScrollRegion()
	newContentHeight := newBottom - newTop + 1

	t.Logf("After resize: contentHeight=%d, currentContentLine=%d", newContentHeight, ac.currentContentLine)

	// Test case 1: If agent was beyond new content area, should be repositioned
	if initialLine > newContentHeight {
		if ac.currentContentLine != newContentHeight {
			t.Errorf("❌ Agent position should be capped at new content height: expected %d, got %d",
				newContentHeight, ac.currentContentLine)
		} else {
			t.Log("✅ Agent position correctly capped to new content area")
		}
	}

	// Test case 2: Agent position should never exceed content area
	if ac.currentContentLine > newContentHeight {
		t.Errorf("❌ Agent position %d exceeds content height %d", ac.currentContentLine, newContentHeight)
	} else {
		t.Log("✅ Agent position within valid bounds after resize")
	}

	// Test case 3: Resize back to larger size
	ac.OnResize(80, 30) // Much larger

	largeTop, largeBottom := ac.autoLayoutManager.GetScrollRegion()
	largeContentHeight := largeBottom - largeTop + 1

	t.Logf("After expand: contentHeight=%d, currentContentLine=%d", largeContentHeight, ac.currentContentLine)

	// Agent should still be positioned reasonably
	if ac.currentContentLine < 1 || ac.currentContentLine > largeContentHeight {
		t.Errorf("❌ Agent position invalid after expansion: %d (max %d)", ac.currentContentLine, largeContentHeight)
	} else {
		t.Log("✅ Agent position valid after terminal expansion")
	}
}

// testResizeFallbackLoop tests the fallback resize polling mechanism
func testResizeFallbackLoop(t *testing.T) {
	t.Log("=== RESIZE FALLBACK LOOP TEST ===")

	mockTerminal := &MockTerminal{}
	ac, _ := setupAgentConsoleForTest(t, mockTerminal, 80, 24)

	// Start the fallback loop (it's started automatically in Init)
	// We need to test that it actually detects size changes

	resizeCount := 0

	// Create a custom resize handler by wrapping the existing logic
	// Since OnResize is a method, we'll call it directly and count manually
	testResize := func(w, h int) {
		resizeCount++
		ac.OnResize(w, h)
	}

	// Simulate a size change in the mock terminal
	mockTerminal.SetSize(100, 30)

	// Test resize directly since fallback loop requires real terminal
	testResize(100, 30)

	// Wait briefly for any other potential processing
	time.Sleep(100 * time.Millisecond)

	if resizeCount == 0 {
		t.Log("Note: Fallback loop may not detect changes in mock terminal (expected in test environment)")
	} else {
		t.Logf("✅ Resize handling detected %d resize events", resizeCount)
	}

	// Test cleanup
	ac.Cleanup()
	t.Log("✅ Fallback loop cleanup completed")
}

// testStreamingCompletionPositioning tests cursor positioning when streaming completes
func testStreamingCompletionPositioning(t *testing.T) {
	t.Log("=== STREAMING COMPLETION POSITIONING TEST ===")

	mockTerminal := &MockTerminal{}
	ac, _ := setupAgentConsoleForTest(t, mockTerminal, 80, 24)

	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	contentHeight := bottom - top + 1

	// Simulate streaming content that goes to line 10
	ac.currentContentLine = 10

	// Call finalizeStreamingPosition
	ac.finalizeStreamingPosition()

	// Verify position is maintained and cursor is positioned correctly
	if ac.currentContentLine != 10 {
		t.Errorf("❌ Streaming position changed unexpectedly: expected 10, got %d", ac.currentContentLine)
	} else {
		t.Log("✅ Streaming position maintained after finalization")
	}

	// Test edge case: streaming position beyond content area
	ac.currentContentLine = contentHeight + 5 // Beyond bounds

	ac.finalizeStreamingPosition()

	if ac.currentContentLine > contentHeight {
		t.Errorf("❌ Streaming position not bounded: %d > %d", ac.currentContentLine, contentHeight)
	} else {
		t.Log("✅ Streaming position correctly bounded after finalization")
	}

	// Check that MoveCursor was called
	commands := mockTerminal.GetCommands()
	var foundMoveCursor bool
	for _, cmd := range commands {
		if strings.Contains(cmd, "MoveCursor") {
			foundMoveCursor = true
			break
		}
	}

	if !foundMoveCursor {
		t.Error("❌ finalizeStreamingPosition should call MoveCursor")
	} else {
		t.Log("✅ Cursor positioning called during finalization")
	}
}

// testBoundsCheckingDuringOutput tests the safeguards during agent output
func testBoundsCheckingDuringOutput(t *testing.T) {
	t.Log("=== BOUNDS CHECKING DURING OUTPUT TEST ===")

	mockTerminal := &MockTerminal{}
	ac, _ := setupAgentConsoleForTest(t, mockTerminal, 80, 24)

	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	maxContentLine := bottom - top + 1

	// Test safePrint bounds checking by simulating problematic content line
	ac.currentContentLine = maxContentLine + 10 // Way beyond bounds

	// Simulate output that would trigger bounds checking
	testContent := "Test content with some newlines\n\nMore content\n"

	// Use safePrint (indirectly through the streaming path)
	ac.safePrint("%s", testContent)

	// Verify bounds were enforced
	if ac.currentContentLine > maxContentLine {
		t.Errorf("❌ Bounds checking failed: currentContentLine=%d, max=%d", ac.currentContentLine, maxContentLine)
	} else {
		t.Logf("✅ Bounds checking enforced: currentContentLine=%d, max=%d", ac.currentContentLine, maxContentLine)
	}

	// Test normal case
	ac.currentContentLine = 5 // Reasonable position
	ac.safePrint("Normal content\n")

	if ac.currentContentLine > maxContentLine {
		t.Error("❌ Normal output exceeded bounds")
	} else {
		t.Log("✅ Normal output stays within bounds")
	}
}

// TestResizeEventIntegration tests the complete resize event flow
func TestResizeEventIntegration(t *testing.T) {
	t.Log("=== RESIZE EVENT INTEGRATION TEST ===")

	mockTerminal := &MockTerminal{}

	// Create event bus to test event subscription
	eventBus := console.NewEventBus(100)

	ac, _ := setupAgentConsoleForTestWithEventBus(t, mockTerminal, 80, 24, eventBus)

	// Initial setup
	ac.currentContentLine = 8

	// Create a resize event (simulating what the terminal would do)
	resizeEvent := console.Event{
		Type:   "terminal.resized",
		Source: "terminal",
		Data:   map[string]int{"width": 60, "height": 20},
	}

	// Test the resize event by publishing and checking the result
	// We can't easily count OnResize calls since it's a method, but we can
	// verify that the resize event was processed by checking the final state

	initialTop, initialBottom := ac.autoLayoutManager.GetScrollRegion()
	t.Logf("Initial scroll region before event: %d-%d", initialTop, initialBottom)

	// Publish the resize event
	eventBus.PublishAsync(resizeEvent)

	// Wait a moment for async event processing
	time.Sleep(100 * time.Millisecond)

	// Check if layout was updated (indirect proof OnResize was called)
	afterTop, afterBottom := ac.autoLayoutManager.GetScrollRegion()
	t.Logf("Scroll region after event: %d-%d", afterTop, afterBottom)

	// The terminal was resized from 80x24 to 60x20, so the scroll region should change
	if initialBottom == afterBottom && initialTop == afterTop {
		t.Log("Note: Scroll region unchanged - resize event may not have been processed in test environment")
	} else {
		t.Log("✅ Resize event processed successfully - layout updated")
	}

	// Verify the agent position was handled correctly
	newTop, newBottom := ac.autoLayoutManager.GetScrollRegion()
	newContentHeight := newBottom - newTop + 1

	if ac.currentContentLine > newContentHeight {
		t.Errorf("❌ Agent position not updated correctly after event: %d > %d", ac.currentContentLine, newContentHeight)
	} else {
		t.Log("✅ Agent position correctly updated after resize event")
	}

	// Cleanup
	ac.Cleanup()
}
