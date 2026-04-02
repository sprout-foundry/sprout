package console

import (
	"fmt"
)

// showContextMenu creates and displays the context menu
func (ir *InputReader) showContextMenu() {
	if ir.contextMenu == nil {
		return
	}

	// Clear previous items
	ir.contextMenu.ClearItems()

	// Add common IDE context menu items
	ir.contextMenu.AddItem("copy", "Copy", "Copy selected text", "Ctrl+C", true)
	ir.contextMenu.AddItem("paste", "Paste", "Paste from clipboard", "Ctrl+V", true)
	ir.contextMenu.AddItem("cut", "Cut", "Cut selected text", "Ctrl+X", true)
	ir.contextMenu.AddItem("undo", "Undo", "Undo last action", "Ctrl+Z", true)
	ir.contextMenu.AddItem("redo", "Redo", "Redo last undone action", "Ctrl+Y", true)
	ir.contextMenu.AddItem("find", "Find", "Search in file", "Ctrl+F", true)
	ir.contextMenu.AddItem("replace", "Replace", "Find and replace", "Ctrl+H", true)
	ir.contextMenu.AddItem("goto", "Go to Line", "Jump to specific line", "Ctrl+G", true)
	ir.contextMenu.AddItem("terminal", "New Terminal", "Open a new terminal", "Ctrl+T", true)
	ir.contextMenu.AddItem("split", "Split View", "Split editor view", "Ctrl+\\", true)

	// Set position based on mouse coordinates
	ir.contextMenu.SetPosition(ir.mouseRow, ir.mouseCol)

	// Set up callbacks
	ir.contextMenu.OnSelect = func(item *ContextMenuItem) {
		// Handle menu item selection
		ir.handleMenuItemSelected(item)
	}

	// Show the menu
	ir.contextMenu.Show()
	ir.contextMenu.Render()
}

// handleMenuItemSelected handles the selection of a menu item
func (ir *InputReader) handleMenuItemSelected(item *ContextMenuItem) {
	// This is a placeholder - in a real implementation,
	// this would trigger the appropriate action
	fmt.Printf("\n[Menu] Selected: %s\n", item.Label)
}
