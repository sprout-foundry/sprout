package components

import (
	"context"
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/ui/core"
)

// DropdownComponent is a pure component for dropdown selection
type DropdownComponent struct {
	*core.BaseComponent
	renderer    core.Renderer
	unsubscribe func()
}

// NewDropdownComponent creates a new dropdown component
func NewDropdownComponent(id string, store core.Store, renderer core.Renderer) *DropdownComponent {
	d := &DropdownComponent{
		BaseComponent: core.NewBaseComponent(id, store),
		renderer:      renderer,
	}

	// Don't subscribe here - let the app handle rendering
	// The input loop will trigger renders after state changes

	return d
}

// Unmount cleans up
func (d *DropdownComponent) Unmount() {
	d.BaseComponent.Unmount()
}

// Render renders the dropdown based on state
func (d *DropdownComponent) Render(ctx context.Context) error {
	// Get dropdown state from store
	state := d.Select(func(s core.State) interface{} {
		ui, _ := s["ui"].(core.State)
		dropdowns, _ := ui["dropdowns"].(map[string]interface{})
		return dropdowns[d.GetID()]
	})

	if state == nil {
		return nil // Dropdown not in state
	}

	dropdownState := state.(map[string]interface{})
	visible, _ := dropdownState["visible"].(bool)
	if !visible {
		return nil
	}

	// Extract state
	items, _ := dropdownState["items"].([]interface{})
	filteredItems, hasFiltered := dropdownState["filteredItems"].([]interface{})

	// Use filtered items if available, otherwise use all items
	displayItems := items
	if hasFiltered {
		displayItems = filteredItems
	}

	options, _ := dropdownState["options"].(map[string]interface{})
	selectedIndex, _ := dropdownState["selectedIndex"].(int)
	searchText, _ := dropdownState["searchText"].(string)

	// Get terminal dimensions from state
	terminalState := d.Select(func(s core.State) interface{} {
		return s["terminal"]
	}).(core.State)

	width, _ := terminalState["width"].(int)
	// height is available if needed later
	_, _ = terminalState["height"].(int)

	// Calculate dropdown position
	maxHeight := 10
	if opt, ok := options["maxHeight"].(int); ok && opt > 0 {
		maxHeight = opt
	}

	dropdownHeight := len(displayItems)
	if dropdownHeight > maxHeight {
		dropdownHeight = maxHeight
	}

	// Position dropdown
	dropdownWidth := width - 20
	if dropdownWidth > 60 {
		dropdownWidth = 60 // Max width
	}
	x := (width - dropdownWidth) / 2
	y := 3 // Position near top to avoid console text

	// Get prompt for height calculation
	prompt, _ := options["prompt"].(string)

	// Adjust for prompt and search
	totalHeight := dropdownHeight + 2 // Border
	if prompt != "" {
		totalHeight++
	}
	if options["showSearch"] != false {
		totalHeight++
	}

	// Clear the screen area first
	d.renderer.Clear()

	// Clear a larger area to prevent artifacts
	// Clear from top of dropdown to bottom including instructions
	clearWidth := dropdownWidth + 4 // Extra width for borders and padding
	if clearWidth < 1 {
		clearWidth = 1
	}
	clearHeight := totalHeight + 5 // Extra height for instructions and padding

	// Clear starting from one row above the dropdown
	for i := -1; i < clearHeight; i++ {
		// Ensure we don't draw outside screen bounds
		clearX := x - 2
		if clearX < 0 {
			clearX = 0
		}
		d.renderer.DrawText(clearX, y-2+i, strings.Repeat(" ", clearWidth))
	}

	// Draw border (box will clear its interior)
	d.renderer.DrawBox(x-1, y-1, dropdownWidth+2, totalHeight)

	// Start inside the box
	contentX := x
	contentY := y

	// Draw prompt
	if prompt != "" {
		d.renderer.DrawText(contentX, contentY, prompt)
		contentY++
	}

	// Draw search box (always show unless explicitly disabled)
	if options["showSearch"] != false {
		searchPrompt := "Search: "
		if sp, ok := options["searchPrompt"].(string); ok {
			searchPrompt = sp
		}
		searchLine := fmt.Sprintf("%s%s_", searchPrompt, searchText)

		// Show filtered count if searching
		if searchText != "" && len(displayItems) != len(items) {
			searchLine += fmt.Sprintf(" (%d/%d)", len(displayItems), len(items))
		}

		d.renderer.DrawText(contentX, contentY, searchLine)
		contentY++
	}

	// Draw items
	windowStart := 0
	if selectedIndex >= dropdownHeight {
		windowStart = selectedIndex - dropdownHeight + 1
	}

	for i := 0; i < dropdownHeight && windowStart+i < len(displayItems); i++ {
		item := displayItems[windowStart+i]
		display := ""

		// Extract display text
		if displayable, ok := item.(interface{ Display() string }); ok {
			display = displayable.Display()
		} else if str, ok := item.(string); ok {
			display = str
		} else {
			display = fmt.Sprintf("%v", item)
		}

		// Truncate if too long
		maxLen := dropdownWidth - 4
		if maxLen > 0 && len(display) > maxLen {
			if maxLen > 3 {
				display = display[:maxLen-3] + "..."
			} else {
				display = display[:maxLen]
			}
		}

		// Highlight selected
		if windowStart+i == selectedIndex {
			display = "▶ " + display
		} else {
			display = "  " + display
		}

		d.renderer.DrawText(contentX, contentY+i, display)
	}

	// Draw scroll indicator at bottom of dropdown (inside the box)
	if len(displayItems) > dropdownHeight {
		info := fmt.Sprintf(" %d-%d of %d ", windowStart+1,
			windowStart+dropdownHeight, len(displayItems))
		infoX := x + (dropdownWidth-len(info))/2
		d.renderer.DrawText(infoX, y+totalHeight-2, info)
	}

	// Draw instructions below the box
	instructions := "↑↓ Navigate • Enter: Select • Esc: Cancel • Type to search"

	// If we have many items, show page navigation hint
	if len(displayItems) > dropdownHeight {
		instructions = "↑↓/PgUp/PgDn Navigate • Enter: Select • Esc: Cancel"
	}

	// If instructions are too long, use shorter version
	if len(instructions) > dropdownWidth {
		if len(displayItems) > dropdownHeight {
			instructions = "↑↓/PgUp/PgDn • Enter • Esc"
		} else {
			instructions = "↑↓ Nav • Enter • Esc • Type"
		}
	}

	// Center the instructions
	instructX := x + (dropdownWidth-len(instructions))/2
	if instructX < x {
		instructX = x
	}

	// Clear the instruction line first
	if dropdownWidth > 0 {
		d.renderer.DrawText(x, y+totalHeight+1, strings.Repeat(" ", dropdownWidth))
	}

	// Draw the instructions
	d.renderer.DrawText(instructX, y+totalHeight+1, instructions)

	return d.renderer.Flush()
}

// HandleInput handles raw input bytes
func (d *DropdownComponent) HandleInput(input []byte) error {
	if len(input) == 0 {
		return nil
	}

	// Handle single byte input
	if len(input) == 1 {
		switch input[0] {
		case 27: // ESC

			d.Dispatch(core.CancelAction())
			d.Dispatch(core.HideDropdownAction(d.GetID()))
		case 13, 10: // Enter (CR or LF)

			// Get current state to ensure we have a selection
			state := d.Select(func(s core.State) interface{} {
				ui, _ := s["ui"].(core.State)
				dropdowns, _ := ui["dropdowns"].(map[string]interface{})
				return dropdowns[d.GetID()]
			})

			if state != nil {
				d.Dispatch(core.SelectAction())
				d.Dispatch(core.HideDropdownAction(d.GetID()))
			}
		default:
			// Regular character - only handle printable ASCII
			if input[0] >= 32 && input[0] <= 126 {
				d.HandleKeyPress(input[0])
			}
			// Ignore non-printable characters to prevent screen thrashing
		}
		return nil
	}

	// Handle escape sequences
	if input[0] == 27 { // ESC
		// Handle arrow keys and other navigation keys
		if len(input) >= 3 && input[1] == '[' {
			switch input[2] {
			case 'A': // Up
				d.HandleArrowKey("up")
				return nil
			case 'B': // Down
				d.HandleArrowKey("down")
				return nil
			case 'C', 'D': // Right/Left - ignore in dropdown
				return nil
			case '5': // Page Up (ESC [ 5 ~)
				if len(input) == 4 && input[3] == '~' {
					d.HandleArrowKey("pageup")
					return nil
				}
			case '6': // Page Down (ESC [ 6 ~)
				if len(input) == 4 && input[3] == '~' {
					d.HandleArrowKey("pagedown")
					return nil
				}
			case 'H': // Home
				d.HandleArrowKey("home")
				return nil
			case 'F': // End
				d.HandleArrowKey("end")
				return nil
			}
		}

		// Filter out mouse events to prevent screen thrashing
		// Mouse events typically start with ESC [ M or ESC [ <
		if len(input) >= 3 && input[1] == '[' && (input[2] == 'M' || input[2] == '<') {
			// Ignore mouse events entirely
			return nil
		}

		// Filter out other escape sequences that might cause issues
		if len(input) >= 2 && input[1] == '[' {
			// This is likely some other escape sequence, ignore it
			return nil
		}
	}

	return nil
}

// HandleKeyPress handles keyboard input
func (d *DropdownComponent) HandleKeyPress(key byte) {
	state := d.Select(func(s core.State) interface{} {
		ui, _ := s["ui"].(core.State)
		dropdowns, _ := ui["dropdowns"].(map[string]interface{})
		return dropdowns[d.GetID()]
	})

	if state == nil {
		return
	}

	dropdownState := state.(map[string]interface{})

	// Ensure dropdown is visible
	if visible, _ := dropdownState["visible"].(bool); !visible {
		return
	}

	// Handle backspace
	if key == 127 || key == 8 {
		searchText, _ := dropdownState["searchText"].(string)
		if len(searchText) > 0 {
			newSearchText := searchText[:len(searchText)-1]
			d.updateSearch(newSearchText)
		}
		return
	}

	// Regular character - add to search
	if key >= 32 && key <= 126 {
		searchText, _ := dropdownState["searchText"].(string)
		newSearchText := searchText + string(key)
		d.updateSearch(newSearchText)
	}
}

// HandleArrowKey handles arrow navigation
func (d *DropdownComponent) HandleArrowKey(direction string) {
	state := d.Select(func(s core.State) interface{} {
		ui, _ := s["ui"].(core.State)
		dropdowns, _ := ui["dropdowns"].(map[string]interface{})
		return dropdowns[d.GetID()]
	})

	if state == nil {
		return
	}

	dropdownState := state.(map[string]interface{})
	items, _ := dropdownState["filteredItems"].([]interface{})
	if len(items) == 0 {
		items, _ = dropdownState["items"].([]interface{})
	}
	selectedIndex, _ := dropdownState["selectedIndex"].(int)

	// Get dropdown height for page navigation
	options, _ := dropdownState["options"].(map[string]interface{})
	maxHeight := 10
	if opt, ok := options["maxHeight"].(int); ok && opt > 0 {
		maxHeight = opt
	}
	pageSize := maxHeight - 1 // Leave one item for context

	newIndex := selectedIndex
	switch direction {
	case "up":
		if selectedIndex > 0 {
			newIndex = selectedIndex - 1
		}
	case "down":
		if selectedIndex < len(items)-1 {
			newIndex = selectedIndex + 1
		}
	case "pageup":
		newIndex = selectedIndex - pageSize
		if newIndex < 0 {
			newIndex = 0
		}
	case "pagedown":
		newIndex = selectedIndex + pageSize
		if newIndex >= len(items) {
			newIndex = len(items) - 1
		}
	case "home":
		newIndex = 0
	case "end":
		newIndex = len(items) - 1
	}

	if newIndex != selectedIndex {
		d.Dispatch(core.UpdateDropdownAction(d.GetID(), map[string]interface{}{
			"selectedIndex": newIndex,
		}))
	}
}

// updateSearch updates the search text and filters items
func (d *DropdownComponent) updateSearch(searchText string) {

	state := d.Select(func(s core.State) interface{} {
		ui, _ := s["ui"].(core.State)
		dropdowns, _ := ui["dropdowns"].(map[string]interface{})
		return dropdowns[d.GetID()]
	})

	if state == nil {

		return
	}

	dropdownState := state.(map[string]interface{})
	allItems, _ := dropdownState["items"].([]interface{})

	// Filter items
	var filteredItems []interface{}
	searchLower := strings.ToLower(searchText)

	for _, item := range allItems {
		searchable := ""

		// Extract searchable text
		if s, ok := item.(interface{ SearchText() string }); ok {
			searchable = s.SearchText()
		} else if s, ok := item.(interface{ Display() string }); ok {
			searchable = s.Display()
		} else if str, ok := item.(string); ok {
			searchable = str
		}

		if strings.Contains(strings.ToLower(searchable), searchLower) {
			filteredItems = append(filteredItems, item)
		}
	}

	// Update state
	d.Dispatch(core.UpdateDropdownAction(d.GetID(), map[string]interface{}{
		"searchText":    searchText,
		"filteredItems": filteredItems,
		"selectedIndex": 0,
	}))
}
