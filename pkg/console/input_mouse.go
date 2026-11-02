package console

// handleMouseEvent processes mouse events from the terminal. Clicks are only
// reported when mouse_click_to_position is on (see mouse_click.go).
func (ir *InputReader) handleMouseEvent(data string) {
	// ParseMouseEvent can return a nil event with a nil error when the
	// internal SGR/X10 parser fails to extract fields.
	mouseEvent, err := ParseMouseEvent(data)
	if err != nil || mouseEvent == nil {
		return
	}
	ir.mouseRow = mouseEvent.Row
	ir.mouseCol = mouseEvent.Col

	if mouseEvent.Button != MouseButtonLeft || mouseEvent.Kind != MouseEventPress {
		// Right-click no longer opens the context menu: its actions were
		// placeholders that only echoed the label (SP-166), and with click
		// reporting on it would pop a menu that does nothing.
		return
	}
	if ir.contextMenu != nil && ir.contextMenu.Visible {
		ir.contextMenu.Hide()
		if ir.contextMenu.OnEscape != nil {
			ir.contextMenu.OnEscape()
		}
		return
	}
	ir.clickAt(mouseEvent.Row, mouseEvent.Col)
}
