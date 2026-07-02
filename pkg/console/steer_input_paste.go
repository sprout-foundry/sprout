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

	// Check for binary image data
	if len(paste) > 4 && len(paste) <= MaxPastedImageSize {
		if ext, mimeType := DetectImageMagic(paste); ext != "" {
			fmt.Fprintln(os.Stderr)
			GlyphAction.Fprintf(os.Stderr, "Image paste detected (%s, %d bytes)", mimeType, len(paste))
			savedPath, err := SavePastedImage(paste, "")
			if err != nil {
				GlyphError.Fprintf(os.Stderr, "Failed to save pasted image: %v", err)
			} else {
				GlyphSuccess.Fprintf(os.Stderr, "Saved to %s", savedPath)
				placeholder := fmt.Sprintf("Pasted image saved to disk: %s ", savedPath)
				r.insertAtCursor([]byte(placeholder))
				return
			}
		}
	}

	// Convert to string for text processing
	content := string(paste)

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
