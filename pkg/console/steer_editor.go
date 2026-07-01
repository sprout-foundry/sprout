package console

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// runExternalEditor opens $EDITOR (or VISUAL) with the current steer
// buffer pre-filled, lets the user edit it, and reads the result back
// into the buffer. Called from the readLoop goroutine — blocks until
// the editor exits. While blocked, the goroutine is not reading stdin,
// which is correct because the editor owns stdin during its run.
//
// Terminal lifecycle: we temporarily restore cooked mode (exitSteerMode
// or groundTruth) and disable bracketed-paste / modifyOtherKeys so the
// editor has a clean terminal. On return we re-enter steer mode and
// re-enable our modes so the readLoop resumes cleanly.
func (r *SteerInputReader) runExternalEditor() {
	editor := chooseExternalEditor()
	if editor == "" {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: no $VISUAL or $EDITOR set and no fallback available")
		return
	}

	// Snapshot the buffer for the temp file.
	r.mu.Lock()
	content := string(r.buffer)
	r.mu.Unlock()

	tmpPath, err := writeBufferToTempFile(content)
	if err != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to stage buffer: %v", err)
		return
	}
	defer os.Remove(tmpPath)

	// Exit steer mode so the editor has a clean terminal (cooked mode,
	// no bracketed paste / modifyOtherKeys reporting).
	fmt.Fprint(os.Stderr, bracketedPasteDisable)
	fmt.Fprint(os.Stderr, modifyOtherKeysDisable)
	r.mu.Lock()
	oldState := r.oldState
	gt := r.groundTruth
	r.mu.Unlock()
	if gt != nil {
		_ = gt.Restore()
	} else if oldState != nil {
		_ = exitSteerMode(r.fd, oldState)
	}
	fmt.Fprintln(os.Stderr)

	// Run the editor. Blocks until the editor exits.
	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()

	// Re-enter steer mode regardless of the editor's exit status so
	// the readLoop can resume reading keystrokes.
	st, enterErr := enterSteerMode(r.fd)
	if enterErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to re-enter steer mode: %v", enterErr)
		return
	}
	r.mu.Lock()
	r.oldState = st
	r.mu.Unlock()
	fmt.Fprint(os.Stderr, bracketedPasteEnable)
	fmt.Fprint(os.Stderr, modifyOtherKeysEnable)

	if runErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: %s exited: %v", editor, runErr)
		r.renderLine()
		return
	}

	// Read back the edited content.
	fileContent, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to read back buffer: %v", readErr)
		r.renderLine()
		return
	}

	// Strip the trailing newline most editors append so the buffer
	// looks like the user typed exactly what they see.
	newContent := strings.TrimRight(string(fileContent), "\n")
	r.mu.Lock()
	r.buffer = []byte(newContent)
	r.cursorPos = len(r.buffer)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}
