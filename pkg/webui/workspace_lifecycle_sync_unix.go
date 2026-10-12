//go:build !js && !windows

package webui

import "syscall"

// syncFilesystem flushes dirty pages to disk so a checkpoint survives an
// abrupt power-off right after prepare-stop.
func syncFilesystem() {
	syscall.Sync()
}
