package console

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSteerInput_UndoMatchesThePrompt(t *testing.T) {
	r := &SteerInputReader{}
	for _, c := range "stop and rerun" {
		r.insertAtCursor([]byte(string(c)))
	}
	r.deleteWordBackward()
	require.Equal(t, "stop and ", r.line)
	r.undoEdit()
	require.Equal(t, "stop and rerun", r.line)
	r.undoEdit()
	require.Equal(t, "stop and", r.line)
}

func TestSteerInput_EscClearIsUndoable(t *testing.T) {
	r := &SteerInputReader{}
	r.insertAtCursor([]byte("half a thought"))
	r.clearBuffer()
	require.Empty(t, r.line)
	r.undoEdit()
	require.Equal(t, "half a thought", r.line)
}

func TestSteerInput_SubmitStartsAFreshUndoHistory(t *testing.T) {
	var sent string
	r := &SteerInputReader{submitFn: func(s string) { sent = s }}
	r.insertAtCursor([]byte("go"))
	r.handleSubmit()
	require.Equal(t, "go", sent)
	r.undoEdit()
	require.Empty(t, r.line, "undo must not resurrect a sent message")
}

func TestSteerInput_EnterAcceptsAtPathInsteadOfSending(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), nil, 0o644))
	t.Chdir(dir)

	sent := false
	r := &SteerInputReader{autocomplete: newInlineAutocomplete(), submitFn: func(string) { sent = true }}
	r.insertAtCursor([]byte("see @no"))
	refreshTestDropdown(r)
	require.True(t, r.autocomplete.visible)
	r.handleEvent(&InputEvent{Type: EventEnter})
	require.False(t, sent)
	require.Equal(t, "see @notes.md", r.line)
}
