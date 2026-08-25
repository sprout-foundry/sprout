package console

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"
)

// runExternalEditor opens $EDITOR (or the next available fallback) with
// the current input buffer pre-filled, lets the user edit it, and reads
// the file back into the buffer when the editor exits. Returns the new
// raw-mode *term.State if raw mode was successfully re-entered, or nil
// to signal the caller to keep its existing state.
//
// SP-048-4f. Triggered by Ctrl-X Ctrl-E from the input core loop. While
// the editor is running the terminal is in cooked mode (and bracketed
// paste / mouse tracking are disabled), so the editor can read keystrokes
// normally. On exit we re-enter raw mode and re-enable our terminal
// modes so input handling resumes cleanly.
//
// Failures are reported to stderr but never fatal: a broken editor leaves
// the buffer untouched and returns the user to the prompt.
func (ir *InputReader) runExternalEditor(prevState *term.State, nonBlocking bool) *term.State {
	editor := chooseExternalEditor()
	if editor == "" {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: no $VISUAL or $EDITOR set and no fallback available")
		return nil
	}

	tmpPath, err := writeBufferToTempFile(ir.line)
	if err != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to stage buffer: %v", err)
		return nil
	}
	defer os.Remove(tmpPath)

	// Exit raw mode + disable bracketed paste / mouse so the editor has a
	// clean terminal. Newline puts the editor below the prompt rather
	// than overwriting it.
	if prevState != nil {
		_ = term.Restore(ir.termFd, prevState)
	}
	if nonBlocking {
		_ = setNonblock(ir.termFd, false)
	}
	fmt.Print(bracketedPasteDisable)
	fmt.Print(MouseTrackingDisable)
	writeModifyOtherKeysDisable(os.Stdout)
	fmt.Println()

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	runErr := cmd.Run()

	// Re-enter raw mode + restore our terminal modes regardless of editor
	// exit status, so the caller can keep reading input.
	newState, mErr := term.MakeRaw(ir.termFd)
	if nonBlocking {
		_ = setNonblock(ir.termFd, true)
	}
	fmt.Print(bracketedPasteEnable)
	fmt.Print(MouseTrackingSGR)
	writeModifyOtherKeysEnable(os.Stdout)

	if runErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: %s exited: %v", editor, runErr)
		if mErr != nil {
			return nil
		}
		return newState
	}

	content, readErr := os.ReadFile(tmpPath)
	if readErr != nil {
		fmt.Fprint(os.Stderr, "\r\n")
		GlyphError.Fprintf(os.Stderr, "editor: failed to read back buffer: %v", readErr)
		if mErr != nil {
			return nil
		}
		return newState
	}

	// Most editors append a trailing newline; strip it so the buffer
	// looks like the user typed exactly what they see in the editor.
	newLine := strings.TrimRight(string(content), "\n")
	ir.line = newLine
	ir.cursorPos = len(newLine)
	ir.hasEditedLine = true
	ir.historyIndex = -1
	ir.resetCompletionCycle()

	if mErr != nil {
		return nil
	}
	return newState
}

// chooseExternalEditor returns the command to launch for the Ctrl-X
// Ctrl-E flow. Honors $VISUAL first (the readline convention), then
// $EDITOR, then falls back to whichever common editor is installed.
// Returns empty string when nothing usable is available.
func chooseExternalEditor() string {
	if e := strings.TrimSpace(os.Getenv("VISUAL")); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	for _, candidate := range []string{"nano", "vim", "vi"} {
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// writeBufferToTempFile creates a temp file pre-populated with content
// (suffix .md so editors that pick a syntax highlighter default to
// something pleasant for prose). Returns the path on success.
func writeBufferToTempFile(content string) (string, error) {
	tmp, err := os.CreateTemp("", "sprout-edit-*.md")
	if err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}
