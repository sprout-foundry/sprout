package console

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var pngHeader = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestPastedImageFiles_TerminalEncodings(t *testing.T) {
	dir := t.TempDir()
	shot := writeFile(t, filepath.Join(dir, "Screenshot 2026-10-02 at 9.41.03\u202fAM.png"), pngHeader)
	other := writeFile(t, filepath.Join(dir, "diagram.png"), pngHeader)
	escape := func(p string) string {
		return strings.NewReplacer(" ", `\ `, "\u202f", "\\\u202f").Replace(p)
	}

	cases := map[string][]string{
		"raw path (Finder copy)":            {shot},
		"backslash-escaped (drag and drop)": {escape(shot)},
		"single-quoted":                     {"'" + shot + "'"},
		"file URL":                          {"file://" + strings.ReplaceAll(strings.ReplaceAll(shot, " ", "%20"), "\u202f", "%E2%80%AF")},
		"narrow space pasted as plain":      {strings.ReplaceAll(shot, "\u202f", " ")},
		"two dropped files":                 {escape(shot) + " " + other},
	}
	for name, in := range cases {
		paths, ok := PastedImageFiles(in[0])
		require.True(t, ok, name)
		require.Equal(t, shot, paths[0], name)
	}
	paths, _ := PastedImageFiles(escape(shot) + " " + other)
	require.Equal(t, []string{shot, other}, paths)
}

func TestPastedImageFiles_LeavesOtherTextAlone(t *testing.T) {
	dir := t.TempDir()
	img := writeFile(t, filepath.Join(dir, "a.png"), pngHeader)
	notImage := writeFile(t, filepath.Join(dir, "notes.png"), []byte("just text"))

	for name, in := range map[string]string{
		"prose mentioning a path": "look at " + img,
		"non-image contents":      notImage,
		"relative path":           "a.png",
		"missing file":            filepath.Join(dir, "gone.png"),
		"plain words":             "hello world",
	} {
		_, ok := PastedImageFiles(in)
		require.False(t, ok, name)
	}
}

func TestAttachPastedImageFile_CopiesIntoWorkspace(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "Screenshot\u202fPM.png"), pngHeader)
	t.Chdir(t.TempDir())

	ph, err := AttachPastedImageFile(src)
	require.NoError(t, err)
	paths := ParsePastedImagePlaceholders(ph)
	require.Len(t, paths, 1)
	require.True(t, strings.HasPrefix(paths[0], "./"+PastedImageDirName+"/paste_"), paths[0])
	data, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	require.Equal(t, pngHeader, data, "the copy survives the temp file being cleaned up")
}

func TestPastePlaceholder_LabelsImages(t *testing.T) {
	require.Equal(t, "[image]", pastePlaceholder(PastedImagePlaceholder("./x.png")+" "))
	require.Equal(t, "[2 images]", pastePlaceholder(PastedImagePlaceholder("./x.png")+" "+PastedImagePlaceholder("./y.png")+" "))
}
