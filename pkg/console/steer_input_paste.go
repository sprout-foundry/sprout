package console

import (
	"fmt"
	"os"
	"strings"
)

// beginPaste enters bracketed-paste accumulation mode. All bytes that
// arrive between now and endPaste() are appended verbatim to pasteBuf.
func (r *SteerInputReader) beginPaste() {
	r.mu.Lock()
	r.pasteActive = true
	r.pasteBuf = r.pasteBuf[:0]
	r.mu.Unlock()
}

// endPaste finalizes a bracketed paste: checks for image data,
// applies smart-save for large text pastes, or appends inline.
func (r *SteerInputReader) endPaste() {
	r.mu.Lock()
	paste := r.pasteBuf
	r.pasteBuf = r.pasteBuf[:0]
	r.pasteActive = false
	r.mu.Unlock()

	if len(paste) == 0 {
		return
	}

	// Raw image bytes (some terminals paste the clipboard image itself).
	if placeholder := attachPastedImageData(paste); placeholder != "" {
		r.insertAtCursor([]byte(placeholder))
		return
	}

	// Convert to string for text processing
	content := string(paste)

	// Paths to image files (a dragged-in or Finder-pasted screenshot).
	if paths, ok := PastedImageFiles(content); ok {
		if placeholder := attachPastedImageFiles(paths); placeholder != "" {
			r.insertAtCursor([]byte(placeholder))
			return
		}
	}

	// Smart paste: large text auto-saved as file reference
	if ShouldSmartSavePaste(content) {
		if savedPath, err := SavePastedText(content, ""); err == nil {
			lineCount := strings.Count(content, "\n") + 1
			fmt.Fprintln(os.Stderr)
			GlyphAction.Fprintf(os.Stderr, "%d lines · %d bytes saved to %s",
				lineCount, len(content), savedPath)
			placeholder := "@" + savedPath + " "
			r.insertAtCursor([]byte(placeholder))
			return
		} else {
			GlyphError.Fprintf(os.Stderr, "smart-paste save failed: %v (inserting inline)", err)
		}
	}

	// Default: insert inline
	r.insertAtCursor(paste)
}

// appendPasteByte adds one byte to the in-flight paste buffer. Called
// from readLoop while pasteActive is true.
func (r *SteerInputReader) appendPasteByte(b byte) {
	r.mu.Lock()
	r.pasteBuf = append(r.pasteBuf, b)
	r.mu.Unlock()
}
