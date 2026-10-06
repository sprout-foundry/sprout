//go:build !js

package automate

import "os"

// readDirCompat lists directory entries without relying on os.ReadDir's
// O_DIRECTORY open flag. Native builds use os.ReadDir directly. On js/wasm
// (see fs_compat_wasm.go) the flag is rejected by the browser syscall layer
// ("syscall.Open: O_DIRECTORY is not supported on Windows"), so the WASM
// build opens the directory with O_RDONLY and enumerates it by name.
//
// The agent-tool path (list_automate_workflows → Discover) runs inside the
// in-browser WASM shell, so every directory read in this package must go
// through this function to work in both native and WASM builds.
func readDirCompat(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(dir)
}
