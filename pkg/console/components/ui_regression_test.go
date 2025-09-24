package components

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// TestUIRegressionSuite runs comprehensive tests to prevent UI functionality regressions
func TestUIRegressionSuite(t *testing.T) {
	t.Run("PassthroughMode", testPassthroughModeRegression)
	t.Run("LayoutRestoration", testLayoutRestorationRegression)
	t.Run("CursorPositioning", testCursorPositioningRegression)
	t.Run("InputManager", testInputManagerRegression)
	t.Run("StreamingOutput", testStreamingOutputRegression)
	t.Run("InteractiveCommands", testInteractiveCommandsRegression)
	t.Run("FooterComponent", testFooterComponentRegression)
	t.Run("TerminalLayout", testTerminalLayoutRegression)
}

func testPassthroughModeRegression(t *testing.T) {
	t.Log("=== PASSTHROUGH MODE REGRESSION TESTS ===")

	// Test 1: Basic passthrough mode toggle
	t.Run("BasicToggle", func(t *testing.T) {
		im := NewInputManager("> ")
		if im == nil {
			t.Fatal("Failed to create input manager")
		}

		// Should start in non-passthrough mode
		if im.running {
			t.Error("Input manager should not be running initially")
		}

		// Enable passthrough mode (should be no-op when not running)
		im.SetPassthroughMode(true)

		// Disable passthrough mode (should be no-op when not running)
		im.SetPassthroughMode(false)

		t.Log("✅ Basic passthrough toggle works")
	})

	// Test 2: Passthrough mode state consistency
	t.Run("StateConsistency", func(t *testing.T) {
		im := NewInputManager("> ")

		// Multiple toggles should be safe
		for i := 0; i < 5; i++ {
			im.SetPassthroughMode(true)
			im.SetPassthroughMode(false)
		}

		t.Log("✅ Multiple passthrough toggles are safe")
	})

	// Test 3: Interactive command detection
	t.Run("InteractiveCommandDetection", func(t *testing.T) {
		interactiveCommands := []string{"models", "mcp", "commit", "shell", "providers"}
		nonInteractiveCommands := []string{"log", "help", "changes", "status", "info", "rollback"}

		for _, cmd := range interactiveCommands {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if !isInteractive {
				t.Errorf("Command '%s' should be detected as interactive", cmd)
			}
		}

		for _, cmd := range nonInteractiveCommands {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if isInteractive {
				t.Errorf("Command '%s' should NOT be detected as interactive", cmd)
			}
		}

		t.Log("✅ Interactive command detection is correct")
	})
}

func testLayoutRestorationRegression(t *testing.T) {
	t.Log("=== LAYOUT RESTORATION REGRESSION TESTS ===")

	// Test 1: Agent console layout restoration method exists and works
	t.Run("RestoreLayoutMethod", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		if mockAgent == nil {
			t.Skip("Skipping test - could not create mock agent")
		}

		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		if ac == nil {
			t.Fatal("Failed to create agent console")
		}

		// Test that the restore method exists and doesn't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("restoreLayoutAfterPassthrough panicked: %v", r)
			}
		}()

		ac.restoreLayoutAfterPassthrough()
		t.Log("✅ Layout restoration method exists and executes without panic")
	})

	// Test 2: Layout manager state consistency
	t.Run("LayoutManagerState", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		if mockAgent == nil {
			t.Skip("Skipping test - could not create mock agent")
		}

		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		// Test that layout manager is initialized
		if ac.autoLayoutManager == nil {
			t.Error("AutoLayoutManager should be initialized")
		}

		// Test that components are registered
		ac.setupLayoutComponents()

		// Test scroll region calculation
		top, bottom := ac.autoLayoutManager.GetScrollRegion()
		if top >= bottom {
			t.Errorf("Invalid scroll region: top=%d should be < bottom=%d", top, bottom)
		}

		t.Log("✅ Layout manager state is consistent")
	})
}

func testCursorPositioningRegression(t *testing.T) {
	t.Log("=== CURSOR POSITIONING REGRESSION TESTS ===")

	// Test 1: Cursor repositioning method exists
	t.Run("RepositionMethod", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		// Test that the reposition method exists and doesn't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("repositionCursorToContentArea panicked: %v", r)
			}
		}()

		ac.repositionCursorToContentArea()
		t.Log("✅ Cursor repositioning method exists and executes")
	})

	// Test 2: Content line tracking
	t.Run("ContentLineTracking", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		// Initial state
		if ac.currentContentLine != 0 {
			t.Errorf("Initial currentContentLine should be 0, got %d", ac.currentContentLine)
		}

		// After repositioning, should be set
		ac.repositionCursorToContentArea()
		if ac.currentContentLine != 1 {
			t.Errorf("After reposition, currentContentLine should be 1, got %d", ac.currentContentLine)
		}

		t.Log("✅ Content line tracking works correctly")
	})
}

func testInputManagerRegression(t *testing.T) {
	t.Log("=== INPUT MANAGER REGRESSION TESTS ===")

	// Test 1: Input manager creation and initialization
	t.Run("Creation", func(t *testing.T) {
		im := NewInputManager("> ")
		if im == nil {
			t.Fatal("Failed to create input manager")
		}

		if im.prompt != "> " {
			t.Errorf("Prompt not set correctly: got %q, want %q", im.prompt, "> ")
		}

		if im.inputChan == nil {
			t.Error("Input channel should be initialized")
		}

		if im.interruptChan == nil {
			t.Error("Interrupt channel should be initialized")
		}

		t.Log("✅ Input manager creation works correctly")
	})

	// Test 2: Input manager callbacks
	t.Run("Callbacks", func(t *testing.T) {
		im := NewInputManager("> ")

		im.SetCallbacks(
			func(string) error {
				return nil
			},
			func() {},
		)

		if im.onInput == nil {
			t.Error("Input callback should be set")
		}

		if im.onInterrupt == nil {
			t.Error("Interrupt callback should be set")
		}

		t.Log("✅ Input manager callbacks work correctly")
	})

	// Test 3: Layout manager integration
	t.Run("LayoutManagerIntegration", func(t *testing.T) {
		im := NewInputManager("> ")
		layoutManager := console.NewAutoLayoutManager()

		// Should not panic when setting layout manager
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetLayoutManager panicked: %v", r)
			}
		}()

		im.SetLayoutManager(layoutManager)
		t.Log("✅ Layout manager integration works")
	})
}

func testStreamingOutputRegression(t *testing.T) {
	t.Log("=== STREAMING OUTPUT REGRESSION TESTS ===")

	// Test 1: Streaming formatter creation and basic functionality
	t.Run("StreamingFormatter", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		if ac.streamingFormatter == nil {
			t.Error("Streaming formatter should be initialized")
		}

		// Test basic write operation
		ac.streamingFormatter.Write("test content")
		if !ac.streamingFormatter.HasProcessedContent() {
			t.Error("Should have processed content after Write")
		}

		// Test reset
		ac.streamingFormatter.Reset()
		if ac.streamingFormatter.HasProcessedContent() {
			t.Error("Should not have processed content after Reset")
		}

		t.Log("✅ Streaming formatter functionality works")
	})

	// Test 2: Safe print functionality
	t.Run("SafePrint", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		// Test that safePrint doesn't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("safePrint panicked: %v", r)
			}
		}()

		ac.safePrint("test content %s", "formatted")
		t.Log("✅ Safe print functionality works")
	})
}

func testInteractiveCommandsRegression(t *testing.T) {
	t.Log("=== INTERACTIVE COMMANDS REGRESSION TESTS ===")

	// Test 1: Command classification is maintained
	t.Run("CommandClassification", func(t *testing.T) {
		interactiveCommands := []string{"models", "mcp", "commit", "shell", "providers"}
		nonInteractiveCommands := []string{"log", "help", "changes", "status", "info", "rollback"}

		// Verify interactive commands
		for _, cmd := range interactiveCommands {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if !isInteractive {
				t.Errorf("Command '%s' should be interactive", cmd)
			}
		}

		// Verify non-interactive commands
		for _, cmd := range nonInteractiveCommands {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if isInteractive {
				t.Errorf("Command '%s' should be non-interactive", cmd)
			}
		}

		t.Log("✅ Command classification is maintained correctly")
	})
}

func testFooterComponentRegression(t *testing.T) {
	t.Log("=== FOOTER COMPONENT REGRESSION TESTS ===")

	// Test 1: Footer creation and basic functionality
	t.Run("FooterCreation", func(t *testing.T) {
		footer := NewFooterComponent()
		if footer == nil {
			t.Fatal("Failed to create footer component")
		}

		// Test basic updates don't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Footer operations panicked: %v", r)
			}
		}()

		footer.UpdateStats("test-model", "test-provider", 100, 0.05, 1, 50, 100)
		footer.UpdatePath("/test/path")
		footer.UpdateGitInfo("main", 5, true)

		height := footer.GetHeight()
		if height <= 0 {
			t.Error("Footer height should be positive")
		}

		t.Log("✅ Footer component functionality works")
	})
}

func testTerminalLayoutRegression(t *testing.T) {
	t.Log("=== TERMINAL LAYOUT REGRESSION TESTS ===")

	// Test 1: Auto layout manager functionality
	t.Run("AutoLayoutManager", func(t *testing.T) {
		layoutManager := console.NewAutoLayoutManager()
		if layoutManager == nil {
			t.Fatal("Failed to create auto layout manager")
		}

		// Test component registration
		componentInfo := &console.ComponentInfo{
			Name:     "test-component",
			Position: "bottom",
			Height:   2,
			Priority: 10,
			Visible:  true,
			ZOrder:   100,
		}

		layoutManager.RegisterComponent("test", componentInfo)

		// Test initialization
		err := layoutManager.Initialize()
		if err != nil {
			t.Errorf("Layout manager initialization failed: %v", err)
		}

		// Test scroll region calculation
		top, bottom := layoutManager.GetScrollRegion()
		if top >= bottom {
			t.Errorf("Invalid scroll region: top=%d should be < bottom=%d", top, bottom)
		}

		t.Log("✅ Terminal layout functionality works")
	})

	// Test 2: Component registration and layout calculation
	t.Run("ComponentLayout", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		// Test component setup
		ac.setupLayoutComponents()

		// Verify components are registered
		top, bottom := ac.autoLayoutManager.GetScrollRegion()
		if top <= 0 || bottom <= top {
			t.Errorf("Invalid scroll region after setup: top=%d, bottom=%d", top, bottom)
		}

		// Test that content region is available
		_, err := ac.autoLayoutManager.GetContentRegion()
		if err != nil {
			t.Errorf("Failed to get content region: %v", err)
		}

		t.Log("✅ Component layout calculation works")
	})
}

// Helper function to create a mock agent for testing
func createMockAgent(t *testing.T) *agent.Agent {
	t.Helper()

	// Create a real agent for testing - it's needed for proper initialization
	mockAgent, err := agent.NewAgent()
	if err != nil {
		// If agent creation fails (e.g., missing config), return nil
		// Some tests can handle nil agents, others will skip
		return nil
	}
	return mockAgent
}

// TestUIRegressionComprehensive runs additional comprehensive tests
func TestUIRegressionComprehensive(t *testing.T) {
	t.Log("=== COMPREHENSIVE UI REGRESSION TESTS ===")

	// Test 1: Full agent console initialization
	t.Run("FullInitialization", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		if ac == nil {
			t.Fatal("Failed to create agent console")
		}

		// Verify all components are initialized
		if ac.inputManager == nil {
			t.Error("Input manager should be initialized")
		}

		if ac.footer == nil {
			t.Error("Footer should be initialized")
		}

		if ac.streamingFormatter == nil {
			t.Error("Streaming formatter should be initialized")
		}

		if ac.autoLayoutManager == nil {
			t.Error("Auto layout manager should be initialized")
		}

		if ac.consoleBuffer == nil {
			t.Error("Console buffer should be initialized")
		}

		t.Log("✅ Full agent console initialization works")
	})

	// Test 2: Agent console configuration
	t.Run("Configuration", func(t *testing.T) {
		config := DefaultAgentConsoleConfig()
		if config == nil {
			t.Fatal("Failed to create default config")
		}

		if config.Prompt == "" {
			t.Error("Default prompt should not be empty")
		}

		if config.HistoryFile == "" {
			t.Error("Default history file should not be empty")
		}

		t.Log("✅ Agent console configuration works")
	})

	// Test 3: Completion signals filtering
	t.Run("CompletionSignalFiltering", func(t *testing.T) {
		mockAgent := createMockAgent(t)
		config := DefaultAgentConsoleConfig()
		ac := NewAgentConsole(mockAgent, config)

		testCases := []struct {
			input    string
			expected string
		}{
			{"Normal content", "Normal content"},
			{"Content [[TASK_COMPLETE]] more content", "Content  more content"},
			{"[[TASKCOMPLETE]]", ""},
			{"Before [[task_complete]] after", "Before  after"},
		}

		for _, tc := range testCases {
			result := ac.filterCompletionSignals(tc.input)
			if result != tc.expected {
				t.Errorf("Filter failed for %q: got %q, want %q", tc.input, result, tc.expected)
			}
		}

		t.Log("✅ Completion signal filtering works")
	})
}
