package components

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/console"
)

// DropdownItem represents an item that can be displayed in the dropdown
type DropdownItem interface {
	// Display returns the string to show in the dropdown
	Display() string
	// SearchText returns the text used for searching
	SearchText() string
	// Value returns the actual value when selected
	Value() interface{}
}

// DropdownOptions configures dropdown behavior
type DropdownOptions struct {
	Prompt     string
	MaxHeight  int  // Max visible items (0 = auto)
	ShowSearch bool // Show search input
	ShowCounts bool // Show item counts
}

// DropdownComponent is a console-integrated dropdown selector
type DropdownComponent struct {
	*console.BaseComponent
	mu sync.RWMutex

	// Configuration
	options DropdownOptions

	// State
	items         []DropdownItem
	filteredItems []DropdownItem
	selectedIndex int
	windowStart   int
	searchText    string
	isActive      bool
	hasExclusive  bool

	// Callbacks
	onSelect func(item DropdownItem) error
	onCancel func() error

	// Layout
	region        console.Region
	visibleHeight int

	// Terminal state
	savedCursorX, savedCursorY int
}

// NewDropdownComponent creates a new dropdown component
func NewDropdownComponent() *DropdownComponent {
	return &DropdownComponent{
		BaseComponent: console.NewBaseComponent("dropdown", "dropdown"),
		options: DropdownOptions{
			ShowSearch: true,
			ShowCounts: true,
		},
	}
}

// Init initializes the dropdown component
func (d *DropdownComponent) Init(ctx context.Context, deps console.Dependencies) error {
	if err := d.BaseComponent.Init(ctx, deps); err != nil {
		return err
	}

	// Subscribe to resize events
	d.Deps.Events.Subscribe("terminal.resized", d.handleResize)

	// Subscribe to input exclusive responses
	d.Deps.Events.Subscribe("input.exclusive_granted", d.handleExclusiveGranted)
	d.Deps.Events.Subscribe("input.exclusive_denied", d.handleExclusiveDenied)

	return nil
}

// Show activates the dropdown with items
func (d *DropdownComponent) Show(items []DropdownItem, options DropdownOptions, onSelect func(DropdownItem) error, onCancel func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isActive {
		return fmt.Errorf("dropdown already active")
	}

	// Set up state
	d.items = items
	d.filteredItems = items
	d.options = options
	d.onSelect = onSelect
	d.onCancel = onCancel
	d.selectedIndex = 0
	d.windowStart = 0
	d.searchText = ""

	// Calculate layout
	if err := d.calculateLayout(); err != nil {
		return err
	}

	// Request exclusive input
	fmt.Printf("DEBUG Dropdown: Requesting exclusive input\n")
	d.Deps.Events.Publish(console.Event{
		Type:   "input.request_exclusive",
		Source: d.ID(),
	})

	return nil
}

// calculateLayout determines the dropdown's position and size
func (d *DropdownComponent) calculateLayout() error {
	width, height, err := d.Deps.Terminal.GetSize()
	if err != nil {
		return err
	}

	// Save current cursor position
	// TODO: Add GetCursorPosition to TerminalManager interface
	d.savedCursorX = 1
	d.savedCursorY = height - 2 // Above input line

	// Calculate dropdown height
	maxHeight := d.options.MaxHeight
	if maxHeight == 0 {
		maxHeight = height / 3 // Default to 1/3 of screen
	}

	itemCount := len(d.filteredItems)
	headerLines := 3 // Prompt + border + search
	footerLines := 1 // Border

	d.visibleHeight = itemCount
	if d.visibleHeight > maxHeight {
		d.visibleHeight = maxHeight
	}

	totalHeight := headerLines + d.visibleHeight + footerLines

	// Position above cursor or at top if not enough space
	startY := d.savedCursorY - totalHeight
	if startY < 1 {
		startY = 1
	}

	// Define our region
	d.region = console.Region{
		X:       0,
		Y:       startY,
		Width:   width,
		Height:  totalHeight,
		ZOrder:  100, // High z-order for overlay
		Visible: true,
	}

	return d.Deps.Layout.DefineRegion("dropdown", d.region)
}

// handleExclusiveGranted handles when we get exclusive input
func (d *DropdownComponent) handleExclusiveGranted(e console.Event) error {
	if e.Data.(map[string]interface{})["component"] != d.ID() {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.isActive = true
	d.hasExclusive = true
	d.SetNeedsRedraw(true)

	fmt.Printf("DEBUG Dropdown: Got exclusive input, now active\n")

	// Hide cursor during dropdown
	d.Deps.Terminal.HideCursor()

	// Initial render
	return d.Render()
}

// handleExclusiveDenied handles when we can't get exclusive input
func (d *DropdownComponent) handleExclusiveDenied(e console.Event) error {
	if e.Data.(map[string]interface{})["component"] != d.ID() {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Clean up
	d.isActive = false
	if d.onCancel != nil {
		return d.onCancel()
	}
	return nil
}

// Render draws the dropdown
func (d *DropdownComponent) Render() error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if !d.isActive {
		return nil
	}

	fmt.Printf("DEBUG Dropdown: Rendering with %d items\n", len(d.filteredItems))

	// Save terminal state
	d.Deps.Terminal.SaveCursor()
	defer d.Deps.Terminal.RestoreCursor()

	// Clear the region
	for y := d.region.Y; y < d.region.Y+d.region.Height; y++ {
		d.Deps.Terminal.MoveCursor(d.region.X+1, y)
		d.Deps.Terminal.ClearToEndOfLine()
	}

	y := d.region.Y

	// Draw top border
	d.Deps.Terminal.MoveCursor(d.region.X+1, y)
	d.Deps.Terminal.WriteText("┌" + strings.Repeat("─", d.region.Width-2) + "┐")
	y++

	// Draw prompt
	if d.options.Prompt != "" {
		d.Deps.Terminal.MoveCursor(d.region.X+1, y)
		d.Deps.Terminal.WriteText(fmt.Sprintf("│ %s%s │", d.options.Prompt,
			strings.Repeat(" ", d.region.Width-4-len(d.options.Prompt))))
		y++
	}

	// Draw search box if enabled
	if d.options.ShowSearch {
		d.Deps.Terminal.MoveCursor(d.region.X+1, y)
		searchLine := fmt.Sprintf("│ Search: %s%s │", d.searchText,
			strings.Repeat(" ", d.region.Width-13-len(d.searchText)))
		d.Deps.Terminal.WriteText(searchLine)
		y++
	}

	// Draw items
	endIdx := d.windowStart + d.visibleHeight
	if endIdx > len(d.filteredItems) {
		endIdx = len(d.filteredItems)
	}

	for i := d.windowStart; i < endIdx; i++ {
		d.Deps.Terminal.MoveCursor(d.region.X+1, y)

		item := d.filteredItems[i]
		display := item.Display()

		// Truncate if too long
		maxLen := d.region.Width - 6
		if len(display) > maxLen {
			display = display[:maxLen-3] + "..."
		}

		// Highlight selected item
		if i == d.selectedIndex {
			d.Deps.Terminal.WriteText(fmt.Sprintf("│ > %s%s │", display,
				strings.Repeat(" ", d.region.Width-6-len(display))))
		} else {
			d.Deps.Terminal.WriteText(fmt.Sprintf("│   %s%s │", display,
				strings.Repeat(" ", d.region.Width-6-len(display))))
		}
		y++
	}

	// Fill empty space
	for y < d.region.Y+d.region.Height-1 {
		d.Deps.Terminal.MoveCursor(d.region.X+1, y)
		d.Deps.Terminal.WriteText("│" + strings.Repeat(" ", d.region.Width-2) + "│")
		y++
	}

	// Draw bottom border with counts
	d.Deps.Terminal.MoveCursor(d.region.X+1, y)
	if d.options.ShowCounts && len(d.items) > d.visibleHeight {
		info := fmt.Sprintf(" %d-%d of %d ",
			d.windowStart+1, endIdx, len(d.filteredItems))
		borderLen := (d.region.Width - len(info) - 2) / 2
		d.Deps.Terminal.WriteText("└" + strings.Repeat("─", borderLen) +
			info + strings.Repeat("─", d.region.Width-borderLen-len(info)-2) + "┘")
	} else {
		d.Deps.Terminal.WriteText("└" + strings.Repeat("─", d.region.Width-2) + "┘")
	}

	d.SetNeedsRedraw(false)
	return nil
}

// HandleInput processes keyboard input
func (d *DropdownComponent) HandleInput(input []byte) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.isActive || !d.hasExclusive {
		return false, nil
	}

	// Handle special keys
	if len(input) == 1 {
		switch input[0] {
		case 27: // ESC
			return true, d.cancel()
		case 13: // Enter
			return true, d.selectCurrent()
		case 127, 8: // Backspace
			if len(d.searchText) > 0 {
				d.searchText = d.searchText[:len(d.searchText)-1]
				d.updateFilter()
				d.SetNeedsRedraw(true)
			}
			return true, nil
		}
	}

	// Handle arrow keys
	if len(input) == 3 && input[0] == 27 && input[1] == '[' {
		switch input[2] {
		case 'A': // Up
			d.moveSelection(-1)
			return true, nil
		case 'B': // Down
			d.moveSelection(1)
			return true, nil
		}
	}

	// Handle regular characters for search
	if len(input) == 1 && input[0] >= 32 && input[0] <= 126 {
		d.searchText += string(input[0])
		d.updateFilter()
		d.SetNeedsRedraw(true)
		return true, nil
	}

	return true, nil
}

// CanHandleInput returns true when dropdown is active
func (d *DropdownComponent) CanHandleInput() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.isActive
}

// moveSelection moves the selection up or down
func (d *DropdownComponent) moveSelection(delta int) {
	if len(d.filteredItems) == 0 {
		return
	}

	d.selectedIndex += delta

	// Wrap around
	if d.selectedIndex < 0 {
		d.selectedIndex = len(d.filteredItems) - 1
	} else if d.selectedIndex >= len(d.filteredItems) {
		d.selectedIndex = 0
	}

	// Adjust window
	if d.selectedIndex < d.windowStart {
		d.windowStart = d.selectedIndex
	} else if d.selectedIndex >= d.windowStart+d.visibleHeight {
		d.windowStart = d.selectedIndex - d.visibleHeight + 1
	}

	d.SetNeedsRedraw(true)
}

// updateFilter updates the filtered items based on search text
func (d *DropdownComponent) updateFilter() {
	if d.searchText == "" {
		d.filteredItems = d.items
	} else {
		d.filteredItems = make([]DropdownItem, 0)
		searchLower := strings.ToLower(d.searchText)

		for _, item := range d.items {
			if strings.Contains(strings.ToLower(item.SearchText()), searchLower) {
				d.filteredItems = append(d.filteredItems, item)
			}
		}
	}

	// Reset selection
	d.selectedIndex = 0
	d.windowStart = 0
}

// selectCurrent selects the current item
func (d *DropdownComponent) selectCurrent() error {
	if len(d.filteredItems) == 0 || d.selectedIndex >= len(d.filteredItems) {
		return nil
	}

	selected := d.filteredItems[d.selectedIndex]

	// Clean up first
	d.cleanup()

	// Call callback
	if d.onSelect != nil {
		return d.onSelect(selected)
	}

	return nil
}

// cancel cancels the dropdown
func (d *DropdownComponent) cancel() error {
	// Clean up first
	d.cleanup()

	// Call callback
	if d.onCancel != nil {
		return d.onCancel()
	}

	return nil
}

// cleanup cleans up the dropdown state
func (d *DropdownComponent) cleanup() {
	d.isActive = false
	d.hasExclusive = false

	// Show cursor again
	d.Deps.Terminal.ShowCursor()

	// Remove our region
	d.Deps.Layout.RemoveRegion("dropdown")

	// Release exclusive input
	d.Deps.Events.Publish(console.Event{
		Type:   "input.release_exclusive",
		Source: d.ID(),
	})

	// Request full redraw to clean up
	d.Deps.Layout.ForceRedraw()
}

// handleResize handles terminal resize events
func (d *DropdownComponent) handleResize(e console.Event) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.isActive {
		return nil
	}

	// Recalculate layout
	if err := d.calculateLayout(); err != nil {
		return err
	}

	d.SetNeedsRedraw(true)
	return nil
}

// Cleanup cleans up the component
func (d *DropdownComponent) Cleanup() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.isActive {
		d.cleanup()
	}

	return d.BaseComponent.Cleanup()
}
