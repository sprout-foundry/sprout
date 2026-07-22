// Package console: event dispatch and Alt+letter routing (split from input_core.go)

package console

// HandleEvent processes an input event
func (ir *InputReader) HandleEvent(event *InputEvent) {
	// CLI-D: any keypress while a tooltip is visible dismisses it.
	// Suppress the dismiss for the toggle key itself — that path
	// explicitly toggles visibility, handled in dispatchAltLetter.
	if ir.tooltipVisible() {
		ir.hideTooltip()
	}

	// When the autocomplete dropdown is visible, intercept navigation
	// and selection keys before the normal dispatch for an edited buffer.
	if ir.autocomplete != nil && ir.autocomplete.visible {
		switch event.Type {
		case EventUp:
			if ir.hasEditedLine {
				ir.autocomplete.moveSelection(-1)
				ir.Refresh()
				return
			}
		case EventDown:
			if ir.hasEditedLine {
				ir.autocomplete.moveSelection(1)
				ir.Refresh()
				return
			}
		case EventTab:
			text := ir.autocomplete.accept()
			if text != "" {
				ir.line = text
				ir.cursorPos = len(ir.line)
				ir.hasEditedLine = true
				ir.historyIndex = -1
				ir.resetCompletionCycle()
				ir.autocomplete.hide()
				ir.Refresh()
				return
			} else {
				ir.autocomplete.hide()
				ir.Refresh()
				return
			}
		case EventEscape:
			ir.autocomplete.hide()
			ir.Refresh()
			return
		}
	}

	switch event.Type {
	case EventChar:
		ir.InsertChar(event.Data)
	case EventBackspace:
		ir.Backspace()
	case EventDelete:
		ir.Delete()
	case EventLeft:
		ir.MoveCursor(-1)
	case EventRight:
		ir.MoveCursor(1)
	case EventHome:
		ir.SetCursor(0)
	case EventEnd:
		ir.SetCursor(len(ir.line))
	case EventWordLeft:
		ir.MoveWord(-1)
	case EventWordRight:
		ir.MoveWord(1)
	case EventDeleteWordBackward:
		ir.DeleteWordBackward()
	case EventAltLetter:
		ir.dispatchAltLetter(event.Data)
	case EventUp:
		// If context menu is visible, navigate it
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.NavigateUp()
			ir.contextMenu.Render()
		} else {
			ir.NavigateVertically(-1)
		}
	case EventDown:
		// If context menu is visible, navigate it
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.NavigateDown()
			ir.contextMenu.Render()
		} else {
			ir.NavigateVertically(1)
		}
	case EventTab:
		// Context menu takes precedence — Tab closes it like Escape.
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.Hide()
			if ir.contextMenu.OnEscape != nil {
				ir.contextMenu.OnEscape()
			}
			return
		}
		// SP-048-2a: slash command completion.
		ir.handleTabCompletion()
	case EventEscape:
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			ir.contextMenu.Hide()
			if ir.contextMenu.OnEscape != nil {
				ir.contextMenu.OnEscape()
			}
		}
	case EventEnter:
		// If context menu is visible, select current item
		if ir.contextMenu != nil && ir.contextMenu.Visible {
			item := ir.contextMenu.SelectCurrent()
			if item != nil {
				ir.contextMenu.Hide()
			}
			return
		}
		// Normal enter handling will occur after this function
	default:
		// Ignore other events
	}
}

// dispatchAltLetter routes an Alt+<letter> event to the registered
// keymap handler. The handler runs synchronously in the REPL
// goroutine. Unhandled letters are silently ignored — same behavior
// as web keybindings that don't have a match.
func (ir *InputReader) dispatchAltLetter(letter string) {
	// Suppress dismissal for the toggle key itself: HandleEvent hides
	// the tooltip on every keystroke (so any key dismisses), but Alt+T
	// is also how the user shows the tooltip in the first place.
	// Re-show from a fresh toggle; if the tooltip was visible, the
	// Hide already fired, so Toggle correctly transitions to "hidden".
	if letter == "T" || letter == "t" {
		if ir.footerTooltip != nil {
			cols, rows := ir.footerSize()
			ir.footerTooltip.Toggle(cols, rows)
		}
		return
	}

	// Generic keymap dispatch.
	entry, ok := GlobalKeymap().MatchAltLetter(letter)
	if !ok || entry.Handler == nil {
		return
	}
	entry.Handler()
}
