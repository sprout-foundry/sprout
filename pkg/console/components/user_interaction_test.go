package components

import (
	"context"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// TestUserInteractionFlow tests what happens during a typical user interaction
func TestUserInteractionFlow(t *testing.T) {
	// Create mock terminal (80x24 standard size)
	mockTerminal := NewMockTerminal(80, 24)

	// Create mock agent
	mockAgent := &agent.Agent{}

	// Create agent console
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	// Create mock dependencies
	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager,
	}

	// Initialize components manually for testing (bypass the terminal check)
	ctx := context.Background()

	// Initialize base component
	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		t.Fatalf("Failed to initialize base component: %v", err)
	}

	// Register components with layout manager
	ac.setupLayoutComponents()

	// Initialize layout manager for testing (bypasses terminal check)
	ac.autoLayoutManager.InitializeForTest(80, 24)

	// Set up terminal manually - this simulates setupTerminal()
	ac.autoLayoutManager.SetComponentHeight("footer", 4)
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)
	mockTerminal.MoveCursor(1, top)

	// Reset current content line tracking
	ac.currentContentLine = 0

	t.Logf("=== SETUP COMPLETE ===")
	t.Logf("Scroll region: %d-%d", top, bottom)
	t.Logf("Initial cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

	// Clear commands to focus on user interaction
	mockTerminal.commands = []string{}

	// === SCENARIO: User types "hello" and gets a simple response ===

	t.Logf("\n=== USER TYPES: 'hello' ===")

	// Step 1: Clear any existing content and show processing message (from processInput)
	ac.safePrint("\r\033[K\n") // This is what processInput does first
	ac.safePrint("🔄 Processing your request...\n")

	t.Logf("After processing message:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Step 2: Agent response (simulate a simple response)
	mockTerminal.commands = []string{} // Clear for this step

	ac.safePrint("Hello! How can I help you today?\n")
	ac.safePrint("I'm ready to assist with your coding tasks.\n")

	t.Logf("After agent response:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Step 3: Show final buffer state
	t.Logf("\n=== BUFFER STATE AFTER USER INTERACTION ===")
	lines := mockTerminal.GetBufferContent()
	for i, line := range lines {
		if line != "" {
			t.Logf("Line %2d: '%s'", i+1, line)
		}
	}

	// Analysis: Where did the content appear?
	processingLineNum := -1
	responseLineNum := -1

	for i, line := range lines {
		if strings.Contains(line, "Processing your request") {
			processingLineNum = i + 1
		}
		if strings.Contains(line, "Hello! How can I help") {
			responseLineNum = i + 1
		}
	}

	t.Logf("\n=== ANALYSIS ===")
	t.Logf("Processing message on line: %d", processingLineNum)
	t.Logf("Agent response starts on line: %d", responseLineNum)
	t.Logf("Content area is lines: %d-%d", top, bottom)

	// Check if content appeared in reasonable positions
	if processingLineNum > 0 && processingLineNum <= 3 {
		t.Logf("✅ Processing message appears near top of content area")
	} else if processingLineNum > bottom-4 {
		t.Errorf("❌ Processing message appears near bottom (line %d) - user might think cursor 'jumped'", processingLineNum)
	}

	if responseLineNum > 0 && responseLineNum <= processingLineNum+2 {
		t.Logf("✅ Agent response appears right after processing message")
	} else if responseLineNum > bottom-3 {
		t.Errorf("❌ Agent response appears at bottom (line %d) - might feel like cursor 'jumped'", responseLineNum)
	}
}

// TestEmptyStartInteraction tests what happens with a fresh start (no prior content)
func TestEmptyStartInteraction(t *testing.T) {
	// Create mock terminal (80x24 standard size)
	mockTerminal := NewMockTerminal(80, 24)

	// Create mock agent
	mockAgent := &agent.Agent{}

	// Create agent console
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	// Create mock dependencies
	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager,
	}

	// Initialize components manually
	ctx := context.Background()

	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		t.Fatalf("Failed to initialize base component: %v", err)
	}

	ac.setupLayoutComponents()
	ac.autoLayoutManager.InitializeForTest(80, 24)

	// Set up scroll region
	ac.autoLayoutManager.SetComponentHeight("footer", 4)
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)
	mockTerminal.MoveCursor(1, top)

	// Reset content tracking and clear any pre-rendered welcome/help content
	ac.currentContentLine = 0
	if ac.consoleBuffer != nil {
		ac.consoleBuffer.Clear()
	}
	// Clear visible content area lines in the mock terminal to simulate a fresh start
	for y := top; y <= bottom; y++ {
		mockTerminal.MoveCursor(1, y)
		mockTerminal.ClearLine()
	}
	// Reposition cursor to start of content area
	mockTerminal.MoveCursor(1, top)

	t.Logf("=== FRESH START TEST ===")
	t.Logf("Scroll region: %d-%d", top, bottom)

	// Clear commands
	mockTerminal.commands = []string{}

	// This is the scenario: user opens ledit agent and immediately types something
	// NO welcome message, NO initial help - just direct user input

	t.Logf("\n=== USER TYPES IMMEDIATELY: 'help me debug this code' ===")

	// Step 1: Processing message (first thing user sees)
	ac.safePrint("🔄 Processing your request...\n")

	t.Logf("After processing message:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Step 2: Agent response
	ac.safePrint("I'd be happy to help you debug your code!\n")
	ac.safePrint("Please share the code and describe the issue you're seeing.\n")

	t.Logf("After response:")
	t.Logf("Cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)
	t.Logf("Commands: %v", mockTerminal.GetCommands())

	// Analysis
	lines := mockTerminal.GetBufferContent()

	t.Logf("\n=== FRESH START BUFFER ===")
	for i, line := range lines {
		if line != "" {
			t.Logf("Line %2d: '%s'", i+1, line)
		}
	}

	// The key question: where does the first user interaction appear?
	processingLine := -1
	for i, line := range lines {
		if strings.Contains(line, "Processing your request") {
			processingLine = i + 1
			break
		}
	}

	t.Logf("\n=== FRESH START ANALYSIS ===")
	t.Logf("First content appears on line: %d", processingLine)
	t.Logf("Expected: should be line 1 (top of content area)")

	if processingLine == 1 {
		t.Logf("✅ Content appears at top - good user experience")
	} else {
		t.Errorf("❌ Content appears on line %d instead of line 1 - this feels like cursor 'jumping'", processingLine)
	}
}
