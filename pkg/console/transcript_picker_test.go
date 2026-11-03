//go:build unix && !js

package console

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A question is on screen when a parallel tool finishes. Its line must go
// above the picker, with the picker redrawn beneath it — not be parked at the
// region's last row, on top of the picker's own rows.
func TestWriteTranscript_GoesAboveAnOpenPicker(t *testing.T) {
	var transcript bytes.Buffer
	t.Cleanup(SetTranscriptWriterForTest(&transcript))

	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldErr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldErr; _ = r.Close() })

	sl := NewSelectList(SelectListOptions{Title: "Trust", Items: []SelectItem{
		{Label: "third-party", Value: "third-party"}, {Label: "ours", Value: "ours"},
	}})
	LockOutput()
	activeSelectList = sl
	UnlockOutput()
	t.Cleanup(func() { LockOutput(); activeSelectList = nil; UnlockOutput() })

	WriteTranscript("✓ read_file(main.go)\n")
	SectionBreak()
	BlankLine()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)

	require.Empty(t, transcript.String(), "nothing parks at the region bottom while the picker is up")
	got := string(out)
	line := strings.Index(got, "read_file(main.go)")
	require.GreaterOrEqual(t, line, 0, "the line is printed: %q", got)
	require.Greater(t, strings.Index(got, "third-party"), line, "the picker is redrawn below it: %q", got)
	require.Equal(t, 1, strings.Count(got, "read_file(main.go)"))
}
