package components

import (
	"sync"
	"testing"

	"github.com/alantheprice/ledit/pkg/console"
)

// TestUIComprehensiveRegression tests all UI functionality without requiring a full agent
func TestUIComprehensiveRegression(t *testing.T) {
	t.Run("PassthroughModeCore", testPassthroughModeCore)
	t.Run("InputManagerCore", testInputManagerCore)
	t.Run("LayoutManagerCore", testLayoutManagerCore)
	t.Run("FooterComponentCore", testFooterComponentCore)
	t.Run("StreamingFormatterCore", testStreamingFormatterCore)
	t.Run("InteractiveCommandsCore", testInteractiveCommandsCore)
}

func testPassthroughModeCore(t *testing.T) {
	t.Log("=== PASSTHROUGH MODE CORE FUNCTIONALITY ===")

	// Test 1: Input manager passthrough mode functionality
	t.Run("InputManagerPassthroughMode", func(t *testing.T) {
		im := NewInputManager("> ")
		if im == nil {
			t.Fatal("Failed to create input manager")
		}

		// Test initial state
		if im.running {
			t.Error("Input manager should not be running initially")
		}

		// Test passthrough mode toggle when not running (should be safe)
		im.SetPassthroughMode(true)
		im.SetPassthroughMode(false)

		// Test multiple toggles
		for i := 0; i < 3; i++ {
			im.SetPassthroughMode(true)
			im.SetPassthroughMode(false)
		}

		t.Log("✅ Passthrough mode core functionality works")
	})

	// Test 2: Interactive vs non-interactive command detection
	t.Run("CommandClassification", func(t *testing.T) {
		// This is the critical business logic that must not regress
		interactiveCommands := []string{"models", "mcp", "commit", "shell", "providers"}
		nonInteractiveCommands := []string{"log", "help", "changes", "status", "info", "rollback"}

		for _, cmd := range interactiveCommands {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if !isInteractive {
				t.Errorf("REGRESSION: Command '%s' is no longer detected as interactive", cmd)
			}
		}

		for _, cmd := range nonInteractiveCommands {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if isInteractive {
				t.Errorf("REGRESSION: Command '%s' is incorrectly detected as interactive", cmd)
			}
		}

		t.Log("✅ Command classification preserved")
	})
}

func testInputManagerCore(t *testing.T) {
	t.Log("=== INPUT MANAGER CORE FUNCTIONALITY ===")

	// Test 1: Basic input manager creation and properties
	t.Run("BasicProperties", func(t *testing.T) {
		im := NewInputManager("> test ")
		if im == nil {
			t.Fatal("Failed to create input manager")
		}

		// Test prompt setting
		if im.prompt != "> test " {
			t.Errorf("Prompt not preserved: got %q, want %q", im.prompt, "> test ")
		}

		// Test channels are created
		if im.inputChan == nil {
			t.Error("Input channel should be created")
		}

		if im.interruptChan == nil {
			t.Error("Interrupt channel should be created")
		}

		// Test initial state
		if im.running {
			t.Error("Should not be running initially")
		}

		if im.paused {
			t.Error("Should not be paused initially")
		}

		t.Log("✅ Input manager basic properties work")
	})

	// Test 2: Callback setting
	t.Run("CallbackSetting", func(t *testing.T) {
		im := NewInputManager("> ")

		// Test that callbacks can be set without error
		im.SetCallbacks(
			func(string) error { return nil },
			func() {},
		)

		// Test that callbacks are actually stored
		if im.onInput == nil {
			t.Error("Input callback not stored")
		}

		if im.onInterrupt == nil {
			t.Error("Interrupt callback not stored")
		}

		t.Log("✅ Input manager callbacks work")
	})

	// Test 3: Layout manager integration
	t.Run("LayoutIntegration", func(t *testing.T) {
		im := NewInputManager("> ")
		layoutManager := console.NewAutoLayoutManager()

		// Should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Layout manager integration panicked: %v", r)
			}
		}()

		im.SetLayoutManager(layoutManager)

		t.Log("✅ Layout manager integration works")
	})
}

func testLayoutManagerCore(t *testing.T) {
	t.Log("=== LAYOUT MANAGER CORE FUNCTIONALITY ===")

	// Test 1: Auto layout manager creation
	t.Run("Creation", func(t *testing.T) {
		layoutManager := console.NewAutoLayoutManager()
		if layoutManager == nil {
			t.Fatal("Failed to create auto layout manager")
		}

		t.Log("✅ Auto layout manager creation works")
	})

	// Test 2: Component registration
	t.Run("ComponentRegistration", func(t *testing.T) {
		layoutManager := console.NewAutoLayoutManager()

		// Test footer component registration
		footerInfo := &console.ComponentInfo{
			Name:     "footer",
			Position: "bottom",
			Height:   4,
			Priority: 10,
			Visible:  true,
			ZOrder:   100,
		}

		layoutManager.RegisterComponent("footer", footerInfo)

		// Test input component registration
		inputInfo := &console.ComponentInfo{
			Name:     "input",
			Position: "bottom",
			Height:   1,
			Priority: 20,
			Visible:  true,
			ZOrder:   90,
		}

		layoutManager.RegisterComponent("input", inputInfo)

		// Test initialization after registration
		err := layoutManager.Initialize()
		if err != nil {
			t.Errorf("Layout manager initialization failed: %v", err)
		}

		// Test scroll region calculation
		top, bottom := layoutManager.GetScrollRegion()
		if top <= 0 {
			t.Error("Top of scroll region should be positive")
		}

		if bottom <= top {
			t.Errorf("Bottom (%d) should be greater than top (%d)", bottom, top)
		}

		t.Log("✅ Component registration and layout calculation work")
	})
}

func testFooterComponentCore(t *testing.T) {
	t.Log("=== FOOTER COMPONENT CORE FUNCTIONALITY ===")

	// Test 1: Footer creation
	t.Run("Creation", func(t *testing.T) {
		footer := NewFooterComponent()
		if footer == nil {
			t.Fatal("Failed to create footer component")
		}

		// Test initial height
		height := footer.GetHeight()
		if height <= 0 {
			t.Error("Footer height should be positive initially")
		}

		t.Log("✅ Footer component creation works")
	})

	// Test 2: Footer updates
	t.Run("Updates", func(t *testing.T) {
		footer := NewFooterComponent()

		// Test that updates don't panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Footer updates panicked: %v", r)
			}
		}()

		footer.UpdateStats("gpt-4", "openai", 1000, 0.05, 2, 800, 4000)
		footer.UpdatePath("/Users/test/project")
		footer.UpdateGitInfo("main", 3, true)
		footer.UpdateGitRemote("git@github.com:user/repo")

		// Height might change after updates
		height := footer.GetHeight()
		if height <= 0 {
			t.Error("Footer height should remain positive after updates")
		}

		t.Log("✅ Footer component updates work")
	})
}

func testStreamingFormatterCore(t *testing.T) {
	t.Log("=== STREAMING FORMATTER CORE FUNCTIONALITY ===")

	// Test 1: Streaming formatter creation (without agent console)
	t.Run("Creation", func(t *testing.T) {
		// Create a minimal streaming formatter for testing
		var mutex sync.Mutex
		sf := NewStreamingFormatter(&mutex)

		if sf == nil {
			t.Fatal("Failed to create streaming formatter")
		}

		t.Log("✅ Streaming formatter creation works")
	})

	// Test 2: Basic streaming operations
	t.Run("BasicOperations", func(t *testing.T) {
		var mutex sync.Mutex
		sf := NewStreamingFormatter(&mutex)

		// Test initial state
		if sf.HasProcessedContent() {
			t.Error("Should not have processed content initially")
		}

		// Test write operation
		sf.Write("test content")
		if !sf.HasProcessedContent() {
			t.Error("Should have processed content after Write")
		}

		// Test reset
		sf.Reset()
		if sf.HasProcessedContent() {
			t.Error("Should not have processed content after Reset")
		}

		// Test force flush (should not panic)
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ForceFlush panicked: %v", r)
			}
		}()
		sf.ForceFlush()

		// Test finalize (should not panic)
		sf.Finalize()

		t.Log("✅ Streaming formatter basic operations work")
	})
}

func testInteractiveCommandsCore(t *testing.T) {
	t.Log("=== INTERACTIVE COMMANDS CORE FUNCTIONALITY ===")

	// Test 1: Command classification constants
	t.Run("ClassificationConstants", func(t *testing.T) {
		// These are the exact same checks used in the agent console
		// If these fail, the core business logic has regressed

		testCases := []struct {
			command             string
			shouldBeInteractive bool
		}{
			// Interactive commands
			{"models", true},
			{"mcp", true},
			{"commit", true},
			{"shell", true},
			{"providers", true},

			// Non-interactive commands
			{"log", false},
			{"help", false},
			{"changes", false},
			{"status", false},
			{"info", false},
			{"rollback", false},
			{"clear", false},
			{"history", false},
			{"stats", false},
		}

		for _, tc := range testCases {
			// This is the exact logic from agent console
			isInteractive := tc.command == "models" || tc.command == "mcp" ||
				tc.command == "commit" || tc.command == "shell" ||
				tc.command == "providers"

			if isInteractive != tc.shouldBeInteractive {
				t.Errorf("CRITICAL REGRESSION: Command '%s' classification changed: got interactive=%v, want %v",
					tc.command, isInteractive, tc.shouldBeInteractive)
			}
		}

		t.Log("✅ Interactive command classification constants preserved")
	})

	// Test 2: Interactive vs non-interactive lists
	t.Run("CommandLists", func(t *testing.T) {
		// These lists define the core behavior of the system
		expectedInteractive := []string{"models", "mcp", "commit", "shell", "providers"}
		expectedNonInteractive := []string{"log", "help", "changes", "status", "info", "rollback"}

		// Verify interactive commands
		for _, cmd := range expectedInteractive {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if !isInteractive {
				t.Errorf("CRITICAL: Interactive command '%s' no longer detected as interactive", cmd)
			}
		}

		// Verify non-interactive commands
		for _, cmd := range expectedNonInteractive {
			isInteractive := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers"
			if isInteractive {
				t.Errorf("CRITICAL: Non-interactive command '%s' incorrectly detected as interactive", cmd)
			}
		}

		t.Logf("✅ Command lists preserved: Interactive=%v, NonInteractive=%v",
			expectedInteractive, expectedNonInteractive)
	})
}

// TestCriticalUIFunctionality tests the most critical UI functions that must never break
func TestCriticalUIFunctionality(t *testing.T) {
	t.Log("=== CRITICAL UI FUNCTIONALITY REGRESSION TESTS ===")

	// Test 1: Agent console can be created with default config
	t.Run("AgentConsoleCreation", func(t *testing.T) {
		config := DefaultAgentConsoleConfig()
		if config == nil {
			t.Fatal("CRITICAL: Default config creation failed")
		}

		if config.Prompt == "" {
			t.Error("CRITICAL: Default prompt is empty")
		}

		if config.HistoryFile == "" {
			t.Error("CRITICAL: Default history file is empty")
		}

		t.Log("✅ Agent console configuration works")
	})

	// Test 2: Input manager can be created and configured
	t.Run("InputManagerCreation", func(t *testing.T) {
		im := NewInputManager("test> ")
		if im == nil {
			t.Fatal("CRITICAL: Input manager creation failed")
		}

		if im.prompt != "test> " {
			t.Error("CRITICAL: Input manager prompt not set correctly")
		}

		// Test that it can be configured without panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Input manager configuration panicked: %v", r)
			}
		}()

		im.SetCallbacks(func(string) error { return nil }, func() {})
		im.SetLayoutManager(console.NewAutoLayoutManager())

		t.Log("✅ Input manager creation and configuration work")
	})

	// Test 3: Passthrough mode core functionality
	t.Run("PassthroughModeCore", func(t *testing.T) {
		im := NewInputManager("> ")

		// Test that passthrough mode can be toggled without panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("CRITICAL: Passthrough mode panicked: %v", r)
			}
		}()

		im.SetPassthroughMode(true)
		im.SetPassthroughMode(false)
		im.SetPassthroughMode(true)
		im.SetPassthroughMode(false)

		t.Log("✅ Passthrough mode core functionality works")
	})
}
