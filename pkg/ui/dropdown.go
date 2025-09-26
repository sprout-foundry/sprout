package ui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// DropdownItem represents an item that can be displayed in the dropdown
type DropdownItem interface {
	// Display returns the string to show in the dropdown
	Display() string
	// SearchText returns the text used for searching (can be same as Display)
	SearchText() string
	// Value returns the actual value when selected
	Value() interface{}
}

// DropdownOptions configures the dropdown behavior
type DropdownOptions struct {
	// Prompt shown above the items
	Prompt string
	// SearchPrompt shown at the bottom (default: "Search: ")
	SearchPrompt string
	// MaxHeight limits the number of items shown (0 = auto based on terminal)
	MaxHeight int
	// ShowCounts shows item counts in scroll indicators
	ShowCounts bool
}

// Dropdown provides an interactive dropdown selector with search
type Dropdown struct {
	items         []DropdownItem
	filteredItems []DropdownItem
	selectedIndex int
	windowStart   int
	searchText    string
	options       DropdownOptions
	oldState      *term.State
}

// NewDropdown creates a new dropdown instance
func NewDropdown(items []DropdownItem, options DropdownOptions) *Dropdown {
	if options.SearchPrompt == "" {
		options.SearchPrompt = "Search: "
	}
	if options.ShowCounts {
		options.ShowCounts = true // default to true
	}

	return &Dropdown{
		items:         items,
		filteredItems: items,
		selectedIndex: 0,
		windowStart:   0,
		options:       options,
	}
}

// Show displays the dropdown and returns the selected item
func (d *Dropdown) Show() (DropdownItem, error) {
	// Check if we're running inside the agent console
	inAgentConsole := os.Getenv("LEDIT_AGENT_CONSOLE") == "1"

	// Only set raw mode if not already in agent console
	if !inAgentConsole {
		// Save terminal state
		var err error
		d.oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return nil, fmt.Errorf("failed to set raw mode: %w", err)
		}
		defer d.restore()

		// Switch to alternate screen buffer (like vim/less)
		fmt.Print("\033[?1049h")
	} else {
		// In agent console, just ensure cleanup happens
		defer d.restoreInAgentConsole()
	}

	// Clear screen
	fmt.Print("\033[2J")
	// Hide cursor initially for cleaner display
	fmt.Print("\033[?25l")

	// Initial display
	d.updateDisplay()

	// Handle input
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}

		switch buf[0] {
		case 27: // ESC or arrow keys
			// Read next bytes for arrow keys
			n, _ = os.Stdin.Read(buf)
			if n > 0 && buf[0] == '[' {
				n, _ = os.Stdin.Read(buf)
				if n > 0 {
					switch buf[0] {
					case 'A': // Up arrow
						d.moveSelection(-1)
					case 'B': // Down arrow
						d.moveSelection(1)
					}
				}
			} else {
				// Just ESC - cancel
				return nil, fmt.Errorf("cancelled")
			}

		case 13: // Enter
			if len(d.filteredItems) > 0 {
				return d.filteredItems[d.selectedIndex], nil
			}

		case 127, 8: // Backspace
			if len(d.searchText) > 0 {
				d.searchText = d.searchText[:len(d.searchText)-1]
				d.filterItems()
				d.updateDisplay()
			}

		case 3: // Ctrl+C
			return nil, fmt.Errorf("interrupted")

		default:
			// Regular character
			if buf[0] >= 32 && buf[0] < 127 {
				d.searchText += string(buf[0])
				d.filterItems()
				d.updateDisplay()
			}
		}
	}
}

func (d *Dropdown) moveSelection(delta int) {
	if len(d.filteredItems) == 0 {
		return
	}

	d.selectedIndex += delta
	if d.selectedIndex < 0 {
		d.selectedIndex = 0
	} else if d.selectedIndex >= len(d.filteredItems) {
		d.selectedIndex = len(d.filteredItems) - 1
	}

	d.updateDisplay()
}

func (d *Dropdown) filterItems() {
	if d.searchText == "" {
		d.filteredItems = d.items
		d.selectedIndex = 0
		d.windowStart = 0
		return
	}

	searchLower := strings.ToLower(d.searchText)
	d.filteredItems = make([]DropdownItem, 0)

	for _, item := range d.items {
		searchableText := strings.ToLower(item.SearchText())
		displayText := strings.ToLower(item.Display())

		// Search in both searchable text and display text
		if strings.Contains(searchableText, searchLower) || strings.Contains(displayText, searchLower) {
			d.filteredItems = append(d.filteredItems, item)
		}
	}

	// Reset selection and window
	d.selectedIndex = 0
	d.windowStart = 0

	// Update display immediately after filtering
	d.updateDisplay()
}

func (d *Dropdown) updateDisplay() {
	// Hide cursor during redraw
	fmt.Print("\033[?25l")

	// Clear screen and move to home
	fmt.Print("\033[2J\033[H")

	// Calculate display window
	termWidth, termHeight, _ := term.GetSize(int(os.Stdin.Fd()))
	if termWidth == 0 {
		termWidth = 80
	}
	if termHeight == 0 {
		termHeight = 24
	}

	// Reserve space for UI elements:
	// - prompt (1-2 lines if present)
	// - scroll indicators (2 lines max - above and below)
	// - spacing (1 line)
	// - search info (1 line)
	// - search box (1 line)
	reservedLines := 6 // Base: 2 scroll indicators + spacing + search info + search box
	if d.options.Prompt != "" {
		reservedLines += 2 // Prompt + blank line after
	}

	maxItems := termHeight - reservedLines
	if d.options.MaxHeight > 0 && d.options.MaxHeight < maxItems {
		maxItems = d.options.MaxHeight
	}
	if maxItems < 3 {
		maxItems = 3 // Minimum visible items
	}

	// Adjust window to keep selection visible
	if d.selectedIndex < d.windowStart {
		d.windowStart = d.selectedIndex
	} else if d.selectedIndex >= d.windowStart+maxItems {
		d.windowStart = d.selectedIndex - maxItems + 1
	}

	// Show prompt if provided
	if d.options.Prompt != "" {
		fmt.Printf("%s\r\n\r\n", d.options.Prompt)
	}

	// Show items above indicator
	if d.windowStart > 0 {
		if d.options.ShowCounts {
			fmt.Printf("  ↑ %d more items above\r\n", d.windowStart)
		} else {
			fmt.Printf("  ↑ more items above\r\n")
		}
	} else {
		// Empty line to maintain consistent spacing
		fmt.Println()
	}

	// Show items
	windowEnd := d.windowStart + maxItems
	if windowEnd > len(d.filteredItems) {
		windowEnd = len(d.filteredItems)
	}

	for i := d.windowStart; i < windowEnd; i++ {
		item := d.filteredItems[i]

		// Use compact display for ModelItem if available
		var display string
		if modelItem, ok := item.(*ModelItem); ok {
			display = modelItem.DisplayCompact(termWidth - 4)
		} else {
			display = truncateString(item.Display(), termWidth-4)
		}

		if i == d.selectedIndex {
			fmt.Printf("\033[1;34m> %s\033[0m\r\n", display)
		} else {
			fmt.Printf("  %s\r\n", display)
		}
	}

	// Show items below indicator
	if windowEnd < len(d.filteredItems) {
		if d.options.ShowCounts {
			fmt.Printf("  ↓ %d more items below\r\n", len(d.filteredItems)-windowEnd)
		} else {
			fmt.Printf("  ↓ more items below\r\n")
		}
	} else {
		// Empty line to maintain consistent spacing
		fmt.Println()
	}

	// Add separator line before status
	fmt.Printf("─────────────────────────────────────────────────\r\n")

	// Show search info and selection status
	if len(d.filteredItems) == 0 {
		fmt.Printf("\r\n[No matches]")
	} else {
		fmt.Printf("\r\n[%d matches, showing %d-%d]", len(d.filteredItems), d.windowStart+1, windowEnd)
	}

	// Show search box at bottom with visible cursor
	fmt.Printf("\r\n%s%s", d.options.SearchPrompt, d.searchText)

	// Clear to end of line to remove any artifacts
	fmt.Print("\033[K")

	// Show cursor at end of search text
	fmt.Print("\033[?25h") // Show cursor

	// Ensure the output is flushed
	os.Stdout.Sync()
}

func (d *Dropdown) restore() {
	// Always show cursor again
	fmt.Print("\033[?25h")

	if d.oldState != nil {
		// Only restore terminal state if we saved it
		// Switch back to main screen buffer
		fmt.Print("\033[?1049l")

		// Restore terminal state
		term.Restore(int(os.Stdin.Fd()), d.oldState)
	}
}

// restoreInAgentConsole handles cleanup when running inside agent console
func (d *Dropdown) restoreInAgentConsole() {
	// Just show cursor again - don't mess with terminal modes
	fmt.Print("\033[?25h")
	// Clear screen one more time to ensure clean state
	fmt.Print("\033[2J\033[H")
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
