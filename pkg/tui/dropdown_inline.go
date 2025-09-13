package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// InlineDropdown represents an inline dropdown selector that doesn't create a new tea.Program
type InlineDropdown struct {
	title        string
	items        []dropdownItem
	selected     int
	filterText   string
	filtering    bool
	visibleItems []int // indices of items that match filter
}

// NewInlineDropdown creates a new inline dropdown
func NewInlineDropdown(title string, items []dropdownItem) *InlineDropdown {
	dropdown := &InlineDropdown{
		title:     title,
		items:     items,
		selected:  0,
		filtering: false,
	}
	dropdown.updateVisibleItems()
	return dropdown
}

// Update handles input for the dropdown
func (d *InlineDropdown) Update(input string) (done bool, cancelled bool) {
	switch input {
	case "up":
		d.moveSelection(-1)
	case "down":
		d.moveSelection(1)
	case "enter":
		return true, false
	case "esc", "ctrl+c":
		return true, true
	case "backspace":
		if d.filtering && len(d.filterText) > 0 {
			d.filterText = d.filterText[:len(d.filterText)-1]
			d.updateVisibleItems()
		}
	default:
		// Handle filter text input
		if len(input) == 1 {
			d.filtering = true
			d.filterText += input
			d.updateVisibleItems()
		}
	}
	return false, false
}

// moveSelection moves the selection up or down
func (d *InlineDropdown) moveSelection(delta int) {
	if len(d.visibleItems) == 0 {
		return
	}

	// Find current position in visible items
	currentPos := 0
	for i, idx := range d.visibleItems {
		if idx == d.selected {
			currentPos = i
			break
		}
	}

	// Calculate new position
	newPos := currentPos + delta
	if newPos < 0 {
		newPos = 0
	} else if newPos >= len(d.visibleItems) {
		newPos = len(d.visibleItems) - 1
	}

	d.selected = d.visibleItems[newPos]
}

// updateVisibleItems updates the list of visible items based on filter
func (d *InlineDropdown) updateVisibleItems() {
	d.visibleItems = nil
	filterLower := strings.ToLower(d.filterText)

	for i, item := range d.items {
		if d.filterText == "" || strings.Contains(strings.ToLower(item.title), filterLower) {
			d.visibleItems = append(d.visibleItems, i)
		}
	}

	// Reset selection if current item is not visible
	if len(d.visibleItems) > 0 {
		found := false
		for _, idx := range d.visibleItems {
			if idx == d.selected {
				found = true
				break
			}
		}
		if !found {
			d.selected = d.visibleItems[0]
		}
	}
}

// GetSelected returns the selected item value
func (d *InlineDropdown) GetSelected() interface{} {
	if d.selected >= 0 && d.selected < len(d.items) {
		return d.items[d.selected].value
	}
	return nil
}

// View renders the dropdown
func (d *InlineDropdown) View(width, height int) string {
	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("170")).
		MarginBottom(1)

	content := []string{titleStyle.Render(d.title)}

	// Filter indicator
	if d.filtering && d.filterText != "" {
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("3")).
			Italic(true)
		content = append(content, filterStyle.Render(fmt.Sprintf("Filter: %s", d.filterText)))
	}

	// Calculate how many items we can show
	availableHeight := height - len(content) - 5 // Leave room for help text and borders
	if availableHeight > 15 {
		availableHeight = 15 // Limit max height to prevent overflow
	}
	if availableHeight < 5 {
		availableHeight = 5
	}

	// Show items
	startIdx := 0
	if len(d.visibleItems) > availableHeight {
		// Ensure selected item is visible
		for i, idx := range d.visibleItems {
			if idx == d.selected {
				if i >= availableHeight/2 {
					startIdx = i - availableHeight/2
				}
				break
			}
		}
		if startIdx+availableHeight > len(d.visibleItems) {
			startIdx = len(d.visibleItems) - availableHeight
		}
	}

	endIdx := startIdx + availableHeight
	if endIdx > len(d.visibleItems) {
		endIdx = len(d.visibleItems)
	}

	// Render visible items
	for i := startIdx; i < endIdx; i++ {
		idx := d.visibleItems[i]
		item := d.items[idx]

		prefix := "  "
		style := lipgloss.NewStyle()

		if idx == d.selected {
			prefix = "▶ "
			style = style.
				Foreground(lipgloss.Color("6")).
				Bold(true)
		}

		line := fmt.Sprintf("%s%s", prefix, item.title)
		if item.description != "" {
			descStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Italic(true)
			line += "\n    " + descStyle.Render(item.description)
		}

		content = append(content, style.Render(line))
	}

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("8")).
		MarginTop(1)
	help := "↑/↓: Navigate • Enter: Select • Esc: Cancel • Type to filter"
	content = append(content, helpStyle.Render(help))

	// Join all content
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(1).
		Render(strings.Join(content, "\n"))
}
