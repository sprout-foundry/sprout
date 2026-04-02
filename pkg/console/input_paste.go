package console

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func (ir *InputReader) consumeBracketedPasteByte(b byte) bool {
	expected := bracketedPasteEndSeq[ir.bracketedMatch]
	if b == expected {
		ir.bracketedMatch++
		if ir.bracketedMatch == len(bracketedPasteEndSeq) {
			ir.bracketedPaste = false
			ir.bracketedMatch = 0
			return true
		}
		return false
	}

	if ir.bracketedMatch > 0 {
		ir.pasteBuffer.WriteString(bracketedPasteEndSeq[:ir.bracketedMatch])
		ir.bracketedMatch = 0
	}

	if b == 13 {
		ir.pasteBuffer.WriteRune('\n')
		ir.bracketedSawCR = true
		return false
	}
	if b == 10 && ir.bracketedSawCR {
		ir.bracketedSawCR = false
		return false
	}
	ir.bracketedSawCR = false

	// Always accumulate raw bytes for image paste detection (capped to prevent unbounded growth)
	if len(ir.rawPasteBuffer) < MaxPastedImageSize+1024 {
		ir.rawPasteBuffer = append(ir.rawPasteBuffer, b)
	}

	if b == 9 || b == 10 || b >= 32 {
		ir.pasteBuffer.WriteByte(b)
	}

	return false
}

// finalizePaste processes pasted content and inserts it literally at cursor.
func (ir *InputReader) finalizePaste() bool {
	// Snapshot and clear raw binary buffer for image paste detection
	rawBytes := ir.rawPasteBuffer
	ir.rawPasteBuffer = nil

	pastedContent := ir.pasteBuffer.String()
	ir.pasteBuffer.Reset()
	ir.inPasteMode = false
	ir.pasteActive = false

	// Check for binary image paste data (bracketed paste may contain raw image bytes)
	if len(rawBytes) > 4 && len(rawBytes) <= MaxPastedImageSize {
		if ext, mimeType := DetectImageMagic(rawBytes); ext != "" {
			fmt.Fprintf(os.Stderr, "\n[img] Image paste detected (%s, %d bytes)\n", mimeType, len(rawBytes))
			savedPath, err := SavePastedImage(rawBytes, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "[FAIL] Failed to save pasted image: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "[save] Saved to %s\n", savedPath)
				placeholder := fmt.Sprintf("Pasted image saved to disk: %s ", savedPath)
				// Insert placeholder at cursor position
				before := ir.line[:ir.cursorPos]
				after := ir.line[ir.cursorPos:]
				ir.line = before + placeholder + after
				ir.cursorPos += len(placeholder)
				ir.shiftPasteSpans(len(before), len(placeholder))
				ir.addCollapsedPaste(len(before), ir.cursorPos)
				ir.hasEditedLine = true
				ir.historyIndex = -1
				ir.Refresh()
				promptWidth := visibleRuneWidth(ir.prompt)
				lineWidth := len([]rune(ir.line))
				newLength := promptWidth + lineWidth
				ir.lastLineLength = newLength
				cursorPos := promptWidth + ir.cursorPos
				ir.lastWrapPending = isWrapPending(ir.terminalWidth, newLength, cursorPos, newLength)
				return true
			}
		}
	}

	// Strip trailing newline that triggered the paste
	pastedContent = strings.TrimRight(pastedContent, "\n")
	if pastedContent == "" {
		return true
	}

	ir.hasEditedLine = true
	ir.historyIndex = -1

	// Insert at cursor position instead of always appending.
	start := ir.cursorPos
	before := ir.line[:ir.cursorPos]
	after := ir.line[ir.cursorPos:]
	ir.line = before + pastedContent + after
	ir.cursorPos += len(pastedContent)
	ir.shiftPasteSpans(start, len(pastedContent))
	ir.addCollapsedPaste(start, start+len(pastedContent))

	// Show feedback and refresh
	ir.Refresh()

	promptWidth := visibleRuneWidth(ir.prompt)
	lineWidth := len([]rune(ir.line))
	newLength := promptWidth + lineWidth
	ir.lastLineLength = newLength
	cursorPos := promptWidth + ir.cursorPos
	ir.lastWrapPending = isWrapPending(ir.terminalWidth, newLength, cursorPos, newLength)

	return true
}

func (ir *InputReader) addCollapsedPaste(start, end int) {
	if start < 0 || end <= start || end > len(ir.line) {
		return
	}
	ir.collapsedPastes = append(ir.collapsedPastes, pasteSpan{start: start, end: end})
	sort.Slice(ir.collapsedPastes, func(i, j int) bool {
		return ir.collapsedPastes[i].start < ir.collapsedPastes[j].start
	})
}

func (ir *InputReader) shiftPasteSpans(pos, delta int) {
	if delta == 0 || len(ir.collapsedPastes) == 0 {
		return
	}
	filtered := ir.collapsedPastes[:0]
	for _, span := range ir.collapsedPastes {
		if span.end <= pos {
			filtered = append(filtered, span)
			continue
		}
		if span.start >= pos {
			span.start += delta
			span.end += delta
		} else {
			// Edits inside a collapsed span are ambiguous; expand it.
			continue
		}
		if span.start < 0 {
			span.start = 0
		}
		if span.end > len(ir.line) {
			span.end = len(ir.line)
		}
		if span.end > span.start {
			filtered = append(filtered, span)
		}
	}
	ir.collapsedPastes = filtered
}

func (ir *InputReader) findCollapsedPasteAtCursor() int {
	for i, span := range ir.collapsedPastes {
		if ir.cursorPos > span.start && ir.cursorPos < span.end {
			return i
		}
	}
	return -1
}

func (ir *InputReader) expandPasteAtCursor() {
	if idx := ir.findCollapsedPasteAtCursor(); idx >= 0 {
		ir.collapsedPastes = append(ir.collapsedPastes[:idx], ir.collapsedPastes[idx+1:]...)
	}
}

func (ir *InputReader) deleteCollapsedPasteEndingAtCursor() bool {
	for i, span := range ir.collapsedPastes {
		if span.end == ir.cursorPos {
			ir.line = ir.line[:span.start] + ir.line[span.end:]
			ir.cursorPos = span.start
			ir.hasEditedLine = true
			ir.historyIndex = -1
			removed := span.end - span.start
			ir.collapsedPastes = append(ir.collapsedPastes[:i], ir.collapsedPastes[i+1:]...)
			ir.shiftPasteSpans(span.end, -removed)
			return true
		}
	}
	return false
}

func (ir *InputReader) deleteCollapsedPasteStartingAtCursor() bool {
	for i, span := range ir.collapsedPastes {
		if span.start == ir.cursorPos {
			ir.line = ir.line[:span.start] + ir.line[span.end:]
			ir.hasEditedLine = true
			ir.historyIndex = -1
			removed := span.end - span.start
			ir.collapsedPastes = append(ir.collapsedPastes[:i], ir.collapsedPastes[i+1:]...)
			ir.shiftPasteSpans(span.end, -removed)
			return true
		}
	}
	return false
}

func (ir *InputReader) renderLineWithCollapsedPastes() (string, int) {
	if len(ir.collapsedPastes) == 0 {
		return ir.line, ir.cursorPos
	}
	var out strings.Builder
	rawPos := 0
	displayCursor := 0
	cursorSet := false

	for _, span := range ir.collapsedPastes {
		if span.start < rawPos || span.end > len(ir.line) || span.start >= span.end {
			continue
		}
		out.WriteString(ir.line[rawPos:span.start])
		if !cursorSet && ir.cursorPos <= span.start {
			displayCursor = out.Len() - (span.start - ir.cursorPos)
			cursorSet = true
		}

		label := fmt.Sprintf("[pasted %d chars]", utf8.RuneCountInString(ir.line[span.start:span.end]))
		if !cursorSet && ir.cursorPos > span.start && ir.cursorPos <= span.end {
			displayCursor = out.Len() + len(label)
			cursorSet = true
		}
		out.WriteString(label)
		rawPos = span.end
	}
	out.WriteString(ir.line[rawPos:])

	if !cursorSet {
		displayCursor = out.Len() - (len(ir.line) - ir.cursorPos)
	}
	if displayCursor < 0 {
		displayCursor = 0
	}
	if displayCursor > out.Len() {
		displayCursor = out.Len()
	}

	return out.String(), displayCursor
}

func runeCountAtByteIndex(s string, byteIndex int) int {
	if byteIndex <= 0 {
		return 0
	}
	if byteIndex >= len(s) {
		return utf8.RuneCountInString(s)
	}
	return utf8.RuneCountInString(s[:byteIndex])
}

func shouldStartHeuristicPaste(chunk []byte, timeSinceLastChar time.Duration) bool {
	if len(chunk) < minHeuristicPasteBytes {
		return false
	}

	printable := 0
	for _, b := range chunk {
		switch {
		case b >= 32 && b <= 126:
			printable++
		case b == 9 || b == 10 || b == 13:
			printable++
		case b == 27 || b == 8 || b == 127:
			// Explicitly exclude ESC/backspace/delete bursts.
			return false
		default:
			// Ignore unsupported control bytes for paste detection.
		}
	}

	// Require nearly all bytes to be printable paste content.
	if printable < len(chunk)-1 {
		return false
	}

	// For moderate bursts, still require rapid arrival.
	if len(chunk) < 20 && timeSinceLastChar >= 30*time.Millisecond {
		return false
	}

	return true
}
