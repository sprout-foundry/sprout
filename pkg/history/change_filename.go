package history

import (
	"runtime"
	"strings"
)

var (
	changeFilenameReplacer = strings.NewReplacer("/", "_", "\\", "_")
	// Windows rejects these in filenames, and a ':' after the first path
	// element silently writes an NTFS alternate data stream instead of a file.
	windowsChangeFilenameReplacer = strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
)

// SafeChangeFilename maps a tracked file path to the flat on-disk name used
// for its .original/.updated content files inside a change directory.
func SafeChangeFilename(filename string) string {
	if runtime.GOOS == "windows" {
		return windowsChangeFilenameReplacer.Replace(filename)
	}
	return changeFilenameReplacer.Replace(filename)
}
