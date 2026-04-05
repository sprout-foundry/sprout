// Mouse tracking and context menu support for ledit IDE
package console

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Mouse event types
type MouseEventKind int

const (
	MouseEventPress MouseEventKind = iota
	MouseEventRelease
	MouseEventMotion
	MouseEventWheelUp
	MouseEventWheelDown
	MouseEventWheelLeft
	MouseEventWheelRight
)

// MouseButton represents which mouse button was pressed
type MouseButton int

const (
	MouseButtonLeft MouseButton = iota
	MouseButtonMiddle
	MouseButtonRight
	MouseButtonExtra1
	MouseButtonExtra2
)

// MouseModifier represents modifier keys pressed with mouse event
type MouseModifier struct {
	Shift bool
	Alt   bool
	Ctrl  bool
}

// MouseEvent represents a complete mouse event from the terminal
type MouseEvent struct {
	Kind      MouseEventKind
	Button    MouseButton
	Modifiers MouseModifier
	Row       int // 1-based row
	Col       int // 1-based column
	Flags     int // Additional flags (e.g., motion flags)
}

// ContextMenuItem represents a single item in the context menu
type ContextMenuItem struct {
	ID          string
	Label       string
	Description string
	Shortcut    string
	Enabled     bool
	SubMenu     []*ContextMenuItem
}

// ContextMenu represents the right-click context menu
type ContextMenu struct {
	Items    []*ContextMenuItem
	Selected int
	Visible  bool
	Row      int // Screen row where menu appears
	Col      int // Screen column where menu appears
	Width    int // Menu width in characters
	Height   int // Menu height in lines
	OnSelect func(item *ContextMenuItem)
	OnEscape func()
}

// Mouse tracking escape sequences
const (
	// Enable mouse tracking (X10 mode - button press/release only)
	MouseTrackingX10 = "\x1b[?9h"
	// Enable mouse tracking (VT200 mode - all mouse events)
	MouseTrackingVT200 = "\x1b[?1000h"
	// Enable mouse tracking with SGR extended coordinates
	MouseTrackingSGR = "\x1b[?1006h"
	// Disable all mouse tracking
	MouseTrackingDisable = "\x1b[?1006l\x1b[?1000l\x1b[?9l"
)

// NewContextMenu creates a new context menu
func NewContextMenu() *ContextMenu {
	return &ContextMenu{
		Items:    make([]*ContextMenuItem, 0),
		Selected: -1,
		Visible:  false,
	}
}

// AddItem adds a menu item to the context menu
func (cm *ContextMenu) AddItem(id, label, description, shortcut string, enabled bool, subMenu ...[]*ContextMenuItem) {
	item := &ContextMenuItem{
		ID:          id,
		Label:       label,
		Description: description,
		Shortcut:    shortcut,
		Enabled:     enabled,
	}
	if len(subMenu) > 0 {
		item.SubMenu = subMenu[0]
	}
	cm.Items = append(cm.Items, item)
}

// ClearItems removes all items from the menu
func (cm *ContextMenu) ClearItems() {
	cm.Items = cm.Items[:0]
	cm.Selected = -1
}

// SetPosition sets the screen position for the menu
func (cm *ContextMenu) SetPosition(row, col int) {
	cm.Row = row
	cm.Col = col
	// Calculate menu dimensions
	cm.Width = 0
	cm.Height = len(cm.Items)
	for _, item := range cm.Items {
		labelWidth := len(item.Label)
		if item.Shortcut != "" {
			labelWidth += len(item.Shortcut) + 4
		}
		if item.Description != "" {
			labelWidth += len(item.Description) + 4
		}
		if labelWidth > cm.Width {
			cm.Width = labelWidth
		}
	}
	if cm.Width < 20 {
		cm.Width = 20 // Minimum width
	}
	if cm.Height > 20 {
		cm.Height = 20 // Maximum visible items
	}
}

// Show displays the context menu
func (cm *ContextMenu) Show() {
	cm.Visible = true
	if cm.Selected < 0 {
		cm.Selected = 0
	}
}

// Hide hides the context menu
func (cm *ContextMenu) Hide() {
	cm.Visible = false
	cm.Selected = -1
}

// Toggle shows/hides the context menu
func (cm *ContextMenu) Toggle() {
	if cm.Visible {
		cm.Hide()
	} else {
		cm.Show()
	}
}

// NavigateUp moves selection up
func (cm *ContextMenu) NavigateUp() {
	if !cm.Visible || len(cm.Items) == 0 {
		return
	}
	cm.Selected--
	if cm.Selected < 0 {
		cm.Selected = len(cm.Items) - 1
	}
}

// NavigateDown moves selection down
func (cm *ContextMenu) NavigateDown() {
	if !cm.Visible || len(cm.Items) == 0 {
		return
	}
	cm.Selected++
	if cm.Selected >= len(cm.Items) {
		cm.Selected = 0
	}
}

// SelectCurrent selects the currently highlighted item
func (cm *ContextMenu) SelectCurrent() *ContextMenuItem {
	if !cm.Visible || cm.Selected < 0 || cm.Selected >= len(cm.Items) {
		return nil
	}
	item := cm.Items[cm.Selected]
	if !item.Enabled {
		return nil
	}
	if cm.OnSelect != nil {
		cm.OnSelect(item)
	}
	return item
}

// Render draws the context menu to the terminal
func (cm *ContextMenu) Render() {
	if !cm.Visible {
		return
	}

	// Calculate menu position (ensure it fits on screen)
	menuRow := cm.Row
	menuCol := cm.Col

	// Adjust if menu would go off screen
	if menuRow+cm.Height > 24 { // Assume 24 line terminal
		menuRow = 24 - cm.Height
	}
	if menuRow < 1 {
		menuRow = 1
	}
	if menuCol+cm.Width > 80 { // Assume 80 column terminal
		menuCol = 80 - cm.Width
	}
	if menuCol < 1 {
		menuCol = 1
	}

	// Move cursor to menu position
	fmt.Printf("\x1b[%d;%dH", menuRow, menuCol)

	// Draw menu border and items
	for i := 0; i < cm.Height; i++ {
		row := menuRow + i
		col := menuCol

		if i < len(cm.Items) {
			item := cm.Items[i]
			selected := i == cm.Selected

			// Draw selection highlight
			if selected {
				fmt.Printf("\x1b[7m")
			}

			// Draw item label
			label := item.Label
			if item.Shortcut != "" {
				label = fmt.Sprintf("%-20s [%s]", label, item.Shortcut)
			}
			if item.Description != "" {
				label = fmt.Sprintf("%s - %s", label, item.Description)
			}

			// Truncate if too long
			if utf8.RuneCountInString(label) > cm.Width-2 {
				label = string([]rune(label)[:cm.Width-3]) + "..."
			}

			fmt.Printf("\x1b[%d;%dH", row, col+1)
			fmt.Printf("%s", label)

			// Fill rest of line with spaces
			fmt.Printf("\x1b[%s", ClearLineSeq())

			if selected {
				fmt.Printf("\x1b[27m") // Reset highlight
			}
		} else {
			// Empty line
			fmt.Printf("\x1b[%d;%dH", row, col+1)
			fmt.Printf("\x1b[%s", ClearLineSeq())
		}
	}
}

// ParseMouseEvent parses a mouse escape sequence into a MouseEvent
// Format: ESC [ M Cb Cx Cy (X10 mode)
//         ESC [ < Cb;Cx;Cy M (SGR mode)
func ParseMouseEvent(data string) (*MouseEvent, error) {
	if len(data) < 4 || data[0] != '\x1b' || data[1] != '[' || (data[2] != 'M' && data[2] != '<') {
		return nil, fmt.Errorf("not a mouse event")
	}

	event := &MouseEvent{}

	if data[2] == 'M' {
		// X10 mode: ESC [ M Cb Cx Cy
		if len(data) != 6 {
			return nil, fmt.Errorf("invalid X10 mouse event length")
		}
		event = parseX10MouseEvent(data)
	} else {
		// SGR mode: ESC [ < Cb;Cx;Cy M
		if len(data) < 8 {
			return nil, fmt.Errorf("invalid SGR mouse event")
		}
		event = parseSGRMouseEvent(data)
	}

	return event, nil
}

func parseX10MouseEvent(data string) *MouseEvent {
	// Cb is the button/flags byte
	// Cx and Cy are 1-based coordinates
	cb := int(data[3]) - 32
	cx := int(data[4]) - 32
	cy := int(data[5]) - 32

	event := &MouseEvent{
		Row: cy,
		Col: cx,
	}

	// Decode button and flags
	if cb&0x1 != 0 {
		event.Modifiers.Shift = true
	}
	if cb&0x2 != 0 {
		event.Modifiers.Alt = true
	}
	if cb&0x4 != 0 {
		event.Modifiers.Ctrl = true
	}

	// Determine button and kind
	if cb&0x60 == 0x60 {
		// Release event
		switch cb & 0x1f {
		case 0:
			event.Button = MouseButtonLeft
		case 1:
			event.Button = MouseButtonMiddle
		case 2:
			event.Button = MouseButtonRight
		default:
			event.Button = MouseButtonExtra1
		}
		event.Kind = MouseEventRelease
	} else if cb&0x20 != 0 {
		// Motion event
		event.Kind = MouseEventMotion
		event.Button = MouseButtonLeft // Motion is always left button
	} else {
		// Press event
		switch cb & 0x1f {
		case 0:
			event.Button = MouseButtonLeft
		case 1:
			event.Button = MouseButtonMiddle
		case 2:
			event.Button = MouseButtonRight
		default:
			event.Button = MouseButtonExtra1
		}
		event.Kind = MouseEventPress
	}

	return event
}

func parseSGRMouseEvent(data string) *MouseEvent {
	// Find the 'M' at the end
	endIdx := strings.Index(data, "M")
	if endIdx < 0 {
		return nil
	}

	// Extract the middle part: < Cb;Cx;Cy
	parts := strings.Split(data[4:endIdx], ";")
	if len(parts) != 3 {
		return nil
	}

	cb, _ := strconv.Atoi(parts[0])
	cx, _ := strconv.Atoi(parts[1])
	cy, _ := strconv.Atoi(parts[2])

	event := &MouseEvent{
		Row: cy,
		Col: cx,
	}

	// Decode modifiers (bits 1-3)
	if cb&0x1 != 0 {
		event.Modifiers.Shift = true
	}
	if cb&0x2 != 0 {
		event.Modifiers.Alt = true
	}
	if cb&0x4 != 0 {
		event.Modifiers.Ctrl = true
	}

	// Decode button (bits 4-6)
	button := (cb >> 4) & 0x7

	// Check for release (bit 7)
	if cb&0x80 != 0 {
		event.Kind = MouseEventRelease
	} else if cb&0x20 != 0 {
		event.Kind = MouseEventMotion
	} else {
		event.Kind = MouseEventPress
	}

	// Decode button number
	switch button {
	case 0:
		event.Button = MouseButtonLeft
	case 1:
		event.Button = MouseButtonMiddle
	case 2:
		event.Button = MouseButtonRight
	case 3:
		event.Button = MouseButtonExtra1
	case 64:
		event.Kind = MouseEventWheelUp
	case 65:
		event.Kind = MouseEventWheelDown
	case 66:
		event.Kind = MouseEventWheelLeft
	case 67:
		event.Kind = MouseEventWheelRight
	default:
		event.Button = MouseButtonExtra1
	}

	return event
}

// EnableMouseTracking enables mouse tracking in the terminal
func EnableMouseTracking() {
	fmt.Print(MouseTrackingSGR)
}

// DisableMouseTracking disables mouse tracking
func DisableMouseTracking() {
	fmt.Print(MouseTrackingDisable)
}
