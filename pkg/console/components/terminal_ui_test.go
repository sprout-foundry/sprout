package components

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
)

// MockTerminal captures terminal output and cursor movements for testing
type MockTerminal struct {
	width, height    int
	buffer           [][]rune
	cursorX, cursorY int
	scrollTop        int
	scrollBottom     int
	rawMode          bool
	output           bytes.Buffer
	commands         []string // Track terminal commands
}

func NewMockTerminal(width, height int) *MockTerminal {
	mt := &MockTerminal{
		width:  width,
		height: height,
		buffer: make([][]rune, height),
	}

	for i := range mt.buffer {
		mt.buffer[i] = make([]rune, width)
		for j := range mt.buffer[i] {
			mt.buffer[i][j] = ' '
		}
	}

	mt.cursorX = 1
	mt.cursorY = 1
	mt.scrollTop = 1
	mt.scrollBottom = height

	return mt
}

func (mt *MockTerminal) GetSize() (int, int, error) {
	return mt.width, mt.height, nil
}

func (mt *MockTerminal) SetSize(width, height int) {
	mt.commands = append(mt.commands, fmt.Sprintf("SetSize(%d,%d)", width, height))

	// Update dimensions
	oldBuffer := mt.buffer
	oldWidth := mt.width
	oldHeight := mt.height

	mt.width = width
	mt.height = height

	// Create new buffer
	mt.buffer = make([][]rune, height)
	for i := range mt.buffer {
		mt.buffer[i] = make([]rune, width)
		for j := range mt.buffer[i] {
			mt.buffer[i][j] = ' '
		}
	}

	// Copy old content if applicable
	if oldBuffer != nil {
		copyHeight := min(oldHeight, height)
		copyWidth := min(oldWidth, width)

		for i := 0; i < copyHeight; i++ {
			for j := 0; j < copyWidth; j++ {
				mt.buffer[i][j] = oldBuffer[i][j]
			}
		}
	}

	// Adjust cursor position if needed
	if mt.cursorX > width {
		mt.cursorX = width
	}
	if mt.cursorY > height {
		mt.cursorY = height
	}

	// Reset scroll region
	mt.scrollTop = 1
	mt.scrollBottom = height
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (mt *MockTerminal) IsRawMode() bool {
	return mt.rawMode
}

func (mt *MockTerminal) SetRawMode(enabled bool) error {
	mt.rawMode = enabled
	return nil
}

func (mt *MockTerminal) MoveCursor(x, y int) error {
	mt.commands = append(mt.commands, fmt.Sprintf("MoveCursor(%d,%d)", x, y))
	mt.cursorX = x
	mt.cursorY = y
	return nil
}

func (mt *MockTerminal) SaveCursor() error {
	mt.commands = append(mt.commands, "SaveCursor()")
	return nil
}

func (mt *MockTerminal) RestoreCursor() error {
	mt.commands = append(mt.commands, "RestoreCursor()")
	return nil
}

func (mt *MockTerminal) ClearScreen() error {
	mt.commands = append(mt.commands, "ClearScreen()")
	for i := range mt.buffer {
		for j := range mt.buffer[i] {
			mt.buffer[i][j] = ' '
		}
	}
	return nil
}

func (mt *MockTerminal) ClearLine() error {
	mt.commands = append(mt.commands, fmt.Sprintf("ClearLine() at y=%d", mt.cursorY))
	if mt.cursorY >= 1 && mt.cursorY <= mt.height {
		for j := range mt.buffer[mt.cursorY-1] {
			mt.buffer[mt.cursorY-1][j] = ' '
		}
	}
	return nil
}

func (mt *MockTerminal) ClearToEndOfLine() error {
	mt.commands = append(mt.commands, fmt.Sprintf("ClearToEndOfLine() at y=%d, x=%d", mt.cursorY, mt.cursorX))
	if mt.cursorY >= 1 && mt.cursorY <= mt.height {
		for j := mt.cursorX - 1; j < mt.width; j++ {
			if j >= 0 {
				mt.buffer[mt.cursorY-1][j] = ' '
			}
		}
	}
	return nil
}

func (mt *MockTerminal) ClearToEndOfScreen() error {
	mt.commands = append(mt.commands, fmt.Sprintf("ClearToEndOfScreen() at y=%d, x=%d", mt.cursorY, mt.cursorX))
	// Clear from current position to end of screen
	for y := mt.cursorY; y <= mt.height; y++ {
		if y == mt.cursorY {
			// Clear from current X to end of line
			for j := mt.cursorX - 1; j < mt.width; j++ {
				if j >= 0 {
					mt.buffer[y-1][j] = ' '
				}
			}
		} else {
			// Clear entire line
			for j := range mt.buffer[y-1] {
				mt.buffer[y-1][j] = ' '
			}
		}
	}
	return nil
}

func (mt *MockTerminal) SetScrollRegion(top, bottom int) error {
	mt.commands = append(mt.commands, fmt.Sprintf("SetScrollRegion(%d,%d)", top, bottom))
	mt.scrollTop = top
	mt.scrollBottom = bottom
	return nil
}

func (mt *MockTerminal) ResetScrollRegion() error {
	mt.commands = append(mt.commands, "ResetScrollRegion()")
	mt.scrollTop = 1
	mt.scrollBottom = mt.height
	return nil
}

func (mt *MockTerminal) Cleanup() error {
	mt.commands = append(mt.commands, "Cleanup()")
	return nil
}

func (mt *MockTerminal) Flush() error {
	mt.commands = append(mt.commands, "Flush()")
	return nil
}

func (mt *MockTerminal) HideCursor() error {
	mt.commands = append(mt.commands, "HideCursor()")
	return nil
}

func (mt *MockTerminal) ShowCursor() error {
	mt.commands = append(mt.commands, "ShowCursor()")
	return nil
}

func (mt *MockTerminal) Init() error {
	mt.commands = append(mt.commands, "Init()")
	return nil
}

func (mt *MockTerminal) OnResize(callback func(int, int)) {
	mt.commands = append(mt.commands, "OnResize(callback_registered)")
	// In a real test, we could trigger the callback with test dimensions
}

func (mt *MockTerminal) ScrollDown(lines int) error {
	mt.commands = append(mt.commands, fmt.Sprintf("ScrollDown(%d)", lines))
	return nil
}

func (mt *MockTerminal) ScrollUp(lines int) error {
	mt.commands = append(mt.commands, fmt.Sprintf("ScrollUp(%d)", lines))
	return nil
}

func (mt *MockTerminal) WriteAt(x, y int, data []byte) error {
	text := string(data)
	mt.commands = append(mt.commands, fmt.Sprintf("WriteAt(%d,%d,\"%s\")", x, y, text))
	// Position cursor and write text
	mt.cursorX = x
	mt.cursorY = y
	for _, char := range text {
		if mt.cursorY >= 1 && mt.cursorY <= mt.height && mt.cursorX >= 1 && mt.cursorX <= mt.width {
			mt.buffer[mt.cursorY-1][mt.cursorX-1] = char
			mt.cursorX++
		}
	}
	return nil
}

func (mt *MockTerminal) WriteText(text string) (int, error) {
	mt.commands = append(mt.commands, fmt.Sprintf("WriteText(\"%s\")", text))
	return mt.Write([]byte(text))
}

// handleNewline properly handles newline advancement with scroll region support
func (mt *MockTerminal) handleNewline() {
	mt.cursorX = 1

	// Check if we're at the bottom of the scroll region
	if mt.cursorY >= mt.scrollBottom {
		// We're at or past the bottom of the scroll region
		// In a real terminal, this would cause the scroll region to scroll up
		// and the cursor would stay at the bottom of the scroll region
		mt.scrollContentUp()
		mt.cursorY = mt.scrollBottom
	} else {
		// We're within the scroll region, just advance normally
		mt.cursorY++
	}
}

// scrollContentUp scrolls content within the scroll region up by one line
func (mt *MockTerminal) scrollContentUp() {
	// Move each line up within the scroll region
	for y := mt.scrollTop; y < mt.scrollBottom; y++ {
		if y < len(mt.buffer) && y+1 < len(mt.buffer) {
			copy(mt.buffer[y-1], mt.buffer[y])
		}
	}

	// Clear the bottom line of the scroll region
	if mt.scrollBottom-1 < len(mt.buffer) {
		for x := range mt.buffer[mt.scrollBottom-1] {
			mt.buffer[mt.scrollBottom-1][x] = ' '
		}
	}

	mt.commands = append(mt.commands, fmt.Sprintf("ScrollUp() within region %d-%d", mt.scrollTop, mt.scrollBottom))
}

func (mt *MockTerminal) Write(p []byte) (int, error) {
	text := string(p)
	mt.output.Write(p)

	// Parse text and place in buffer at current cursor position
	// Handle escape sequences and special characters
	i := 0
	for i < len(text) {
		char := rune(text[i])

		// Handle escape sequences
		if char == '\033' && i+1 < len(text) && text[i+1] == '[' {
			// Find end of escape sequence
			j := i + 2
			for j < len(text) && text[j] >= '0' && text[j] <= '9' || text[j] == ';' {
				j++
			}
			if j < len(text) {
				escapeChar := text[j]
				switch escapeChar {
				case 'K': // Clear from cursor to end of line
					mt.ClearToEndOfLine()
				case 'J': // Clear screen variations
					// Handle different J variations if needed
				}
				i = j + 1
				continue
			}
		}

		// Handle special characters
		if char == '\n' {
			mt.handleNewline()
		} else if char == '\r' {
			mt.cursorX = 1 // Carriage return
		} else {
			// Regular character
			if mt.cursorY >= 1 && mt.cursorY <= mt.height && mt.cursorX >= 1 && mt.cursorX <= mt.width {
				mt.buffer[mt.cursorY-1][mt.cursorX-1] = char
				mt.cursorX++
				if mt.cursorX > mt.width {
					mt.cursorX = 1
					mt.handleNewline()
				}
			}
		}
		i++
	}

	return len(p), nil
}

// GetBufferContent returns the visible content in the terminal buffer
func (mt *MockTerminal) GetBufferContent() []string {
	lines := make([]string, mt.height)
	for i, row := range mt.buffer {
		lines[i] = strings.TrimRight(string(row), " ")
	}
	return lines
}

// GetContentInRegion returns content within a specific region
func (mt *MockTerminal) GetContentInRegion(top, bottom int) []string {
	if top < 1 || bottom > mt.height || top > bottom {
		return []string{}
	}

	lines := make([]string, bottom-top+1)
	for i := top - 1; i < bottom; i++ {
		if i >= 0 && i < len(mt.buffer) {
			lines[i-(top-1)] = strings.TrimRight(string(mt.buffer[i]), " ")
		}
	}
	return lines
}

// GetCommands returns all terminal commands executed
func (mt *MockTerminal) GetCommands() []string {
	return mt.commands
}

// EnterAltScreen enters alternate screen buffer (mock implementation)
func (mt *MockTerminal) EnterAltScreen() error {
	mt.commands = append(mt.commands, "EnterAltScreen()")
	return nil
}

// ExitAltScreen exits alternate screen buffer (mock implementation)
func (mt *MockTerminal) ExitAltScreen() error {
	mt.commands = append(mt.commands, "ExitAltScreen()")
	return nil
}

// PrintDebugInfo prints detailed information about terminal state
func (mt *MockTerminal) PrintDebugInfo() {
	fmt.Printf("=== Mock Terminal Debug Info ===\n")
	fmt.Printf("Size: %dx%d\n", mt.width, mt.height)
	fmt.Printf("Cursor: (%d, %d)\n", mt.cursorX, mt.cursorY)
	fmt.Printf("Scroll Region: %d-%d\n", mt.scrollTop, mt.scrollBottom)
	fmt.Printf("Raw Mode: %t\n", mt.rawMode)

	fmt.Printf("\nCommands executed:\n")
	for i, cmd := range mt.commands {
		fmt.Printf("  %d: %s\n", i, cmd)
	}

	fmt.Printf("\nBuffer content:\n")
	lines := mt.GetBufferContent()
	for i, line := range lines {
		fmt.Printf("  %2d: '%s'\n", i+1, line)
	}

	fmt.Printf("\nRaw output: %q\n", mt.output.String())
}

// TestAgentConsolePositioning tests the terminal UI positioning with a mock terminal
func TestAgentConsolePositioning(t *testing.T) {
	// Create mock terminal (80x24 standard size)
	mockTerminal := NewMockTerminal(80, 24)

	// Create mock agent
	mockAgent := &agent.Agent{} // We don't need a real agent for positioning tests

	// Create agent console
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	// Create mock dependencies
	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager, // Footer needs layout manager
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

	// Set up terminal manually (skip full setupTerminal() which requires terminal)
	// Just set up scroll region for testing - assume footer height of 4
	ac.autoLayoutManager.SetComponentHeight("footer", 4)
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)
	mockTerminal.MoveCursor(1, top)

	// Reset current content line tracking
	ac.currentContentLine = 0

	// Print debug info to see what happened during initialization
	t.Logf("After initialization:")
	mockTerminal.PrintDebugInfo()

	// Test: Welcome message positioning
	t.Run("WelcomeMessagePositioning", func(t *testing.T) {
		// The welcome message should appear in the content area (not at the bottom)
		lines := mockTerminal.GetBufferContent()

		// Find where "Welcome to Ledit Agent!" appears
		welcomeLineNum := -1
		for i, line := range lines {
			if strings.Contains(line, "Welcome to Ledit Agent!") {
				welcomeLineNum = i + 1 // Convert to 1-based
				break
			}
		}

		if welcomeLineNum == -1 {
			t.Error("Welcome message not found in terminal buffer")
			return
		}

		t.Logf("Welcome message found on line %d", welcomeLineNum)

		// The welcome message should NOT be at the very bottom
		// It should be in the content area (which starts after any headers and before footer)
		if welcomeLineNum > 20 { // Assuming footer takes up bottom 4 lines
			t.Errorf("Welcome message appears too low (line %d), should be in content area", welcomeLineNum)
		}

		// It should also not be at line 1 (there might be some setup)
		if welcomeLineNum < 2 {
			t.Errorf("Welcome message appears too high (line %d), might be overlapping with other UI", welcomeLineNum)
		}
	})

	// Test: Multiple outputs positioning
	t.Run("MultipleOutputsPositioning", func(t *testing.T) {
		// Clear and reset for clean test
		mockTerminal.ClearScreen()

		// Simulate multiple safePrint calls
		ac.safePrint("First line of output\n")
		t.Logf("After first safePrint - cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

		ac.safePrint("Second line of output\n")
		t.Logf("After second safePrint - cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

		ac.safePrint("Third line of output\n")
		t.Logf("After third safePrint - cursor: (%d,%d)", mockTerminal.cursorX, mockTerminal.cursorY)

		lines := mockTerminal.GetBufferContent()
		t.Logf("Buffer after all safePrint calls:")
		for i, line := range lines {
			if line != "" {
				t.Logf("  %d: '%s'", i+1, line)
			}
		}

		// Find the lines
		firstLineNum := -1
		secondLineNum := -1
		thirdLineNum := -1

		for i, line := range lines {
			if strings.Contains(line, "First line of output") {
				firstLineNum = i + 1
			}
			if strings.Contains(line, "Second line of output") {
				secondLineNum = i + 1
			}
			if strings.Contains(line, "Third line of output") {
				thirdLineNum = i + 1
			}
		}

		t.Logf("Line positions: First=%d, Second=%d, Third=%d", firstLineNum, secondLineNum, thirdLineNum)

		// Verify they appear in sequence
		if firstLineNum == -1 || secondLineNum == -1 || thirdLineNum == -1 {
			t.Error("Not all output lines found in buffer")
			mockTerminal.PrintDebugInfo()
			return
		}

		if secondLineNum != firstLineNum+1 {
			t.Errorf("Second line (%d) should immediately follow first line (%d)", secondLineNum, firstLineNum)
		}

		if thirdLineNum != secondLineNum+1 {
			t.Errorf("Third line (%d) should immediately follow second line (%d)", thirdLineNum, secondLineNum)
		}

		// They should all be in the content area (not at bottom)
		if firstLineNum > 20 {
			t.Errorf("First line appears too low (line %d)", firstLineNum)
		}
	})
}

// TestScrollRegionBehavior tests scroll region setup
func TestScrollRegionBehavior(t *testing.T) {
	mockTerminal := NewMockTerminal(80, 24)
	mockAgent := &agent.Agent{}
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager, // Footer needs layout manager
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

	// Set up scroll region
	ac.autoLayoutManager.SetComponentHeight("footer", 4) // Assume footer height
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	mockTerminal.SetScrollRegion(top, bottom)

	// Check that scroll region was set up
	commands := mockTerminal.GetCommands()
	scrollRegionSet := false
	var scrollTop, scrollBottom int

	for _, cmd := range commands {
		if strings.HasPrefix(cmd, "SetScrollRegion(") {
			scrollRegionSet = true
			// Extract the values
			re := regexp.MustCompile(`SetScrollRegion\((\d+),(\d+)\)`)
			matches := re.FindStringSubmatch(cmd)
			if len(matches) == 3 {
				fmt.Sscanf(matches[1], "%d", &scrollTop)
				fmt.Sscanf(matches[2], "%d", &scrollBottom)
			}
			break
		}
	}

	if !scrollRegionSet {
		t.Error("Scroll region was not set during initialization")
		t.Logf("Commands executed: %v", commands)
		return
	}

	t.Logf("Scroll region set to %d-%d", scrollTop, scrollBottom)

	// Verify reasonable scroll region
	if scrollTop < 1 || scrollTop >= scrollBottom {
		t.Errorf("Invalid scroll region top: %d", scrollTop)
	}

	if scrollBottom > 24 || scrollBottom <= scrollTop {
		t.Errorf("Invalid scroll region bottom: %d", scrollBottom)
	}

	// Should leave space for footer
	expectedContentLines := 24 - 4 // Assuming 4 lines for footer
	actualContentLines := scrollBottom - scrollTop + 1

	if actualContentLines < expectedContentLines-2 || actualContentLines > expectedContentLines+2 {
		t.Errorf("Scroll region size seems wrong: got %d lines, expected around %d", actualContentLines, expectedContentLines)
	}
}

// TestCursorPositioningFlow tests the cursor positioning during content output
func TestCursorPositioningFlow(t *testing.T) {
	mockTerminal := NewMockTerminal(80, 24)
	mockAgent := &agent.Agent{}
	config := DefaultAgentConsoleConfig()
	ac := NewAgentConsole(mockAgent, config)

	deps := console.Dependencies{
		Terminal: mockTerminal,
		Layout:   ac.autoLayoutManager, // Footer needs layout manager
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

	// Clear commands to focus on our test
	mockTerminal.commands = []string{}

	// Reset currentContentLine to test first output positioning
	ac.currentContentLine = 0

	// First safePrint should position cursor in content area
	ac.safePrint("Test content\n")

	commands := mockTerminal.GetCommands()
	t.Logf("Commands after first safePrint: %v", commands)

	// Should have MoveCursor command to position in content area
	hasMoveCursor := false
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, "MoveCursor(") {
			hasMoveCursor = true
			t.Logf("Found cursor positioning: %s", cmd)
			break
		}
	}

	if !hasMoveCursor {
		t.Error("First safePrint should position cursor in content area")
	}

	// Clear commands and test second output
	mockTerminal.commands = []string{}

	// Second safePrint should NOT reposition cursor
	ac.safePrint("More content\n")

	commands = mockTerminal.GetCommands()
	t.Logf("Commands after second safePrint: %v", commands)

	// Should NOT have MoveCursor command
	hasMoveCursor = false
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, "MoveCursor(") {
			hasMoveCursor = true
			break
		}
	}

	if hasMoveCursor {
		t.Error("Second safePrint should not reposition cursor, should continue from current position")
	}
}
