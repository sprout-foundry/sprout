//go:build windows

package webui

// syncFilesystem is a no-op on Windows, which has no system-wide sync call;
// the checkpoint's git writes are already flushed per file.
func syncFilesystem() {}
