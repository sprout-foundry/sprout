package console

import (
	"fmt"
	"os"
	"sort"
	"strings"
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
	ir.pasteActive = false

	// Raw image bytes (some terminals paste the clipboard image itself).
	if placeholder := attachPastedImageData(rawBytes); placeholder != "" {
		ir.insertAttachment(placeholder)
		return true
	}

	// Strip trailing newline that triggered the paste
	pastedContent = strings.TrimRight(pastedContent, "\n")
	if pastedContent == "" {
		return true
	}

	// Paths to image files (a dragged-in or Finder-pasted screenshot).
	if paths, ok := PastedImageFiles(pastedContent); ok {
		if placeholder := attachPastedImageFiles(paths); placeholder != "" {
			ir.insertAttachment(placeholder)
			return true
		}
	}

	// SP-048-4c: Smart paste — if the paste is large (>100 lines OR >5KB),
	// prompt the user to choose: insert inline, save as file & reference, or discard.
	if ShouldSmartSavePaste(pastedContent) {
		if savedPath, err := SavePastedText(pastedContent, ""); err == nil {
			lineCount := strings.Count(pastedContent, "\n") + 1
			fmt.Fprintln(os.Stderr)
			GlyphAction.Fprintf(os.Stderr, "%d lines · %d bytes saved to %s",
				lineCount, len(pastedContent), savedPath)
			ir.insertAttachment("@" + savedPath + " ")
			return true
		} else {
			GlyphError.Fprintf(os.Stderr, "smart-paste save failed: %v (falling back to inline insert)", err)
			// fall through to inline insertion below
		}
	}

	ir.markEdited()
	start := ir.cursorPos
	ir.insertText(pastedContent)
	if shouldCollapsePaste(pastedContent) {
		ir.addCollapsedPaste(start, start+len(pastedContent))
	}

	// Show feedback and refresh
	ir.Refresh()

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
	for _, span := range ir.collapsedPastes {
		if span.end == ir.cursorPos {
			ir.markEdited()
			return ir.deleteRange(span.start, span.end, editOther)
		}
	}
	return false
}

func (ir *InputReader) deleteCollapsedPasteStartingAtCursor() bool {
	for _, span := range ir.collapsedPastes {
		if span.start == ir.cursorPos {
			ir.markEdited()
			return ir.deleteRange(span.start, span.end, editOther)
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

		label := pastePlaceholder(ir.line[span.start:span.end])
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

// Pastes at least this tall, or longer than collapsePasteMinChars, show as a
// one-line placeholder; anything smaller (a path, a command, a few lines) is
// left visible so it can be read and edited before sending.
const (
	collapsePasteMinLines = 5
	collapsePasteMinChars = 400
)

func shouldCollapsePaste(content string) bool {
	return strings.Count(content, "\n")+1 >= collapsePasteMinLines ||
		utf8.RuneCountInString(content) > collapsePasteMinChars
}

func pastePlaceholder(content string) string {
	if n := len(ParsePastedImagePlaceholders(content)); n > 0 && strings.HasPrefix(content, pastedImageBracketPrefix) {
		if n == 1 {
			return "[image]"
		}
		return fmt.Sprintf("[%d images]", n)
	}
	if lines := strings.Count(content, "\n") + 1; lines > 1 {
		return fmt.Sprintf("[pasted %d lines]", lines)
	}
	return fmt.Sprintf("[pasted %d chars]", utf8.RuneCountInString(content))
}

// insertAttachment inserts an attachment placeholder at the cursor as one
// collapsed unit, so editing treats it as a single token.
func (ir *InputReader) insertAttachment(placeholder string) {
	start := ir.cursorPos
	ir.markEdited()
	ir.insertText(placeholder)
	ir.addCollapsedPaste(start, ir.cursorPos)
	ir.Refresh()
}
