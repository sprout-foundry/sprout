package console

// status_footer_hint_state.go — the footer's hint-row and input-box mode
// state (turn active, working label, composer mode, keymap hint, picker
// override), split out of status_footer.go. Pure move.

// SetTurnActive switches the hint row between its mid-turn and idle
// wording.
func (f *StatusFooter) SetTurnActive(active bool) {
	if f == nil {
		return
	}
	mode := ComposerIdle
	if active {
		mode = ComposerSteer
	}
	if f.setComposerMode(mode) {
		f.Refresh()
	}
}

// SetWorkingLabel pins a stable working indication to the hint row for
// the whole duration of a turn, replacing the keymap wording; "" restores
// the keymap hints. This is the turn's in-progress signal that does not
// depend on the composer label appearing and disappearing: unlike the
// activity spinner it stays for tools and subagents, because nothing in
// the tool path rewrites the hint row. An open picker's hintOverride
// still wins while it is set.
func (f *StatusFooter) SetWorkingLabel(text string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	changed := f.workingLabel != text
	f.workingLabel = text
	f.mu.Unlock()
	if changed {
		f.Refresh()
	}
}

// workingLabelLocked returns the hint-row text for the current state:
// the picker override, else the turn's working label, else the keymap
// hints. Caller must hold f.mu.
func (f *StatusFooter) workingHintLocked(mode ComposerMode) string {
	if f.hintOverride != "" {
		return f.hintOverride
	}
	if f.workingLabel != "" {
		return f.workingLabel
	}
	return KeymapHintRow(mode)
}

// SetComposerMode records what the input box does on Enter. The caller's
// next box update redraws it.
func (f *StatusFooter) SetComposerMode(mode ComposerMode) {
	if f == nil {
		return
	}
	f.setComposerMode(mode)
}

func (f *StatusFooter) setComposerMode(mode ComposerMode) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	changed := f.composerMode != mode
	f.composerMode = mode
	return changed
}

// SetShowKeymapHint enables/disables the keyboard shortcut hint row
// above the rule. When true, drawLocked reserves an extra row.
// SP-115.
func (f *StatusFooter) SetShowKeymapHint(show bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	before := f.hintRowCountLocked()
	f.showKeymapHint = show
	after := f.hintRowCountLocked()
	f.mu.Unlock()
	f.applyHintRowChange(before, after, false)
}

// SetShowKeymapHintLocked is SetShowKeymapHint for callers that already hold
// outputMu. The Alt+V handler runs inside InputReader.HandleEvent, which its
// caller drives under the output lock; the unlocked variant would re-enter
// the non-reentrant mutex and wedge the reader goroutine.
func (f *StatusFooter) SetShowKeymapHintLocked(show bool) {
	if f == nil {
		return
	}
	f.mu.Lock()
	before := f.hintRowCountLocked()
	f.showKeymapHint = show
	after := f.hintRowCountLocked()
	f.mu.Unlock()
	f.applyHintRowChange(before, after, true)
}

// SetHintOverride shows text on the hint row in place of the composer's
// key hints; "" restores them. Reports whether the hint row is showing,
// i.e. whether the text is visible.
//
// An override shows the hint row even when the keymap hints are off: a
// picker's keys are the only thing telling the user how to answer, and
// hiding them under compact verbosity left the prompt with no key hints at
// all. The row therefore takes part in the reserved-row count — growing the
// pinned block by a row the scroll region must exclude. Without that, the
// row is painted on the region's last row and the next transcript line
// scrolls straight over it.
func (f *StatusFooter) SetHintOverride(text string) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	before := f.hintRowCountLocked()
	f.hintOverride = text
	after := f.hintRowCountLocked()
	shown := after > 0 && f.active
	f.mu.Unlock()
	f.applyHintRowChange(before, after, false)
	return shown
}

// applyHintRowChange re-applies the scroll region after the hint row's
// visibility changed, and repaints. Callers pass the row count before and
// after the change so the caller's own field writes decide visibility, and
// whether they already hold outputMu (the InputReader render path does).
//
// Growing the block must shift the region's content up first: the row the
// hint takes was transcript, and claiming it without scrolling would paint
// the hint over the last line and then scroll that line away on the next
// write. Shrinking leaves the vacated row to the draw, which clears every
// row it paints.
func (f *StatusFooter) applyHintRowChange(before, after int, locked bool) {
	if before == after {
		return
	}
	f.mu.Lock()
	live := f.active && f.isTTY && !f.proseStreaming
	f.mu.Unlock()
	if !live {
		// Between turns, or mid-stream: the next full draw reconciles the
		// block with the region. Resize and Stop read the live row count.
		if !locked {
			f.Refresh()
		}
		return
	}
	if !locked {
		LockOutput()
		defer UnlockOutput()
	}
	f.shiftScrollRegionLocked()
	f.applyScrollRegionLocked()
	f.drawLocked()
}

// Resize handles a terminal-size change (SIGWINCH). The OLD footer rows
// (tracked via lastRows) are cleared first so a grow doesn't leave the
// previous footer stranded mid-screen, then the scroll region is
// re-applied for the new height and the footer is redrawn at the new
// bottom.
//
// The entire body is wrapped in LockOutput so the scroll-region reset,
// row clearing, and re-application can't interleave with a concurrent
// SteerInputReader.renderLine → footer.draw or a PrintExternal call.
// Without the lock, two SIGWINCH handlers (footer + steer reader) race:
// the steer reader's draw renders footer rows that Resize just cleared,
// and the scroll-region manipulation in Resize displaces content the
// steer reader's draw just positioned — producing the stacked-duplicates
// symptom on every resize during an active turn.
