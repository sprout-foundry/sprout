//go:build !js

package tools

import (
	"os"
	"path/filepath"
)

// readDirCompat lists directory entries. On native builds it is a thin
// wrapper around os.ReadDir. On js/wasm (see fs_compat_wasm.go) it avoids
// os.ReadDir's O_DIRECTORY flag, which is rejected by the js/wasm syscall
// layer in browsers, producing errors such as:
//
//	"syscall.Open: O_DIRECTORY is not supported on Windows"
//
// All directory reads in agent_tools go through this function so the same
// call sites work in both native and WASM builds.
func readDirCompat(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(dir)
}

// walkDirCompat walks a directory tree. On native builds it is a thin
// wrapper around filepath.WalkDir. On js/wasm it delegates to an
// O_DIRECTORY-free implementation. The callback signature matches
// filepath.WalkDir's (func(path string, d os.DirEntry, err error) error)
// so existing closures need no changes.
func walkDirCompat(root string, fn func(path string, d os.DirEntry, err error) error) error {
	return filepath.WalkDir(root, fn)
}
