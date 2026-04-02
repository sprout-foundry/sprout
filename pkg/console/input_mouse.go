package console

// handleMouseEvent processes mouse events from the terminal
func (ir *InputReader) handleMouseEvent(data string) {
	if ir.contextMenu == nil {
		return
	}

	// Parse the mouse event
	mouseEvent, err := ParseMouseEvent(data)
	if err != nil {
		return
	}

	// Update mouse position
	ir.mouseRow = mouseEvent.Row
	ir.mouseCol = mouseEvent.Col

	// Handle right-click (button 2)
	if mouseEvent.Button == MouseButtonRight && mouseEvent.Kind == MouseEventPress {
		// Show context menu at mouse position
		ir.showContextMenu()
		return
	}

	// Handle click elsewhere to close menu
	if mouseEvent.Button == MouseButtonLeft && mouseEvent.Kind == MouseEventPress {
		// Check if click is outside menu area
		if ir.contextMenu.Visible {
			// Close menu if click is not in menu area
			ir.contextMenu.Hide()
			if ir.contextMenu.OnEscape != nil {
				ir.contextMenu.OnEscape()
			}
		}
		return
	}

	// Handle keyboard navigation when menu is visible
	if ir.contextMenu.Visible {
		// Keyboard navigation is handled in HandleEvent
	}
}
