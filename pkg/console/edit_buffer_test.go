package console

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func typeText(b *editBuffer, s string) {
	for _, r := range s {
		b.insertText(string(r))
	}
}

func TestEditBuffer_UndoWalksBackAWordAtATime(t *testing.T) {
	var b editBuffer
	typeText(&b, "fix the tests")
	require.True(t, b.undo())
	require.Equal(t, "fix the", b.line)
	require.True(t, b.undo())
	require.Equal(t, "fix", b.line)
	require.True(t, b.undo())
	require.Equal(t, "", b.line)
	require.False(t, b.undo())
}

func TestEditBuffer_UndoRestoresKillsAndReplacements(t *testing.T) {
	var b editBuffer
	typeText(&b, "hello world")
	b.killWordBefore()
	require.Equal(t, "hello ", b.line)
	require.Equal(t, "world", b.killBuffer)
	b.replaceLine("/help")
	require.True(t, b.undo())
	require.Equal(t, "hello ", b.line)
	require.True(t, b.undo())
	require.Equal(t, "hello world", b.line)
	require.Equal(t, len("hello world"), b.cursorPos)
}

func TestEditBuffer_CursorMoveStartsANewUndoStep(t *testing.T) {
	var b editBuffer
	typeText(&b, "ac")
	b.moveRunes(-1)
	typeText(&b, "b")
	require.Equal(t, "abc", b.line)
	require.True(t, b.undo())
	require.Equal(t, "ac", b.line)
}

func TestEditBuffer_DeletesStepByRune(t *testing.T) {
	b := editBuffer{line: "a🚀b", cursorPos: len("a🚀")}
	require.True(t, b.deleteRuneBefore())
	require.Equal(t, "ab", b.line)
	require.Equal(t, 1, b.cursorPos)
	require.True(t, b.deleteRuneAt())
	require.Equal(t, "a", b.line)
	require.False(t, b.deleteRuneAt())
}

func TestEditBuffer_DeleteRangeKeepsPasteSpansAligned(t *testing.T) {
	b := editBuffer{line: "ab[PASTE]cd", cursorPos: 11, collapsedPastes: []pasteSpan{{start: 2, end: 9}}}
	b.deleteRange(0, 2, editOther)
	require.Equal(t, []pasteSpan{{start: 0, end: 7}}, b.collapsedPastes)
	b.deleteRange(1, 3, editOther)
	require.Empty(t, b.collapsedPastes, "a span the deletion cuts into is dropped")
	require.True(t, b.undo())
	require.Equal(t, []pasteSpan{{start: 0, end: 7}}, b.collapsedPastes, "undo brings the span back with the text")
}

func TestAtFileCandidates(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg", "console"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkg", "main.go"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), nil, 0o644))
	t.Chdir(dir)

	displays := func(line string) []string {
		var out []string
		for _, c := range atFileCandidates(line) {
			out = append(out, c.Display)
		}
		return out
	}
	require.Equal(t, []string{"@pkg/", "@README.md"}, displays("look at @"), "directories first, dotfiles hidden")
	require.Equal(t, []string{"@.env"}, displays("@."), "a leading dot asks for dotfiles")
	require.Equal(t, []string{"@pkg/console/", "@pkg/main.go"}, displays("@pkg/"))
	require.Equal(t, []string{"@pkg/main.go"}, displays("@pkg/MA"), "prefix match ignores case")
	require.Nil(t, atFileCandidates("@pkg/main.go"), "an exact file has nothing left to complete")
	require.Nil(t, atFileCandidates("mail me@pkg"), "an @ inside a word is not a path")

	c := atFileCandidates("read @pkg/m")
	require.Equal(t, "read @pkg/main.go", c[0].Text, "Text is the whole completed line")
}

func TestInlineAutocomplete_FileModeKeepsGoingIntoDirectories(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pkg", "a.go"), nil, 0o644))
	t.Chdir(dir)

	a := newInlineAutocomplete()
	a.update("@p", 2, nil, nil)
	require.True(t, a.visible)
	require.True(t, a.fileMode)
	line := a.accept()
	require.Equal(t, "@pkg/", line)
	a.settleAfterAccept(line, true)
	a.update(line, len(line), nil, nil)
	require.True(t, a.visible, "accepting a directory lists its contents")
	require.Equal(t, "@pkg/a.go", a.candidates[0].Display)
}
