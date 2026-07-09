//go:build !js || !wasm

// wasm_fs_compat.go — Non-WASM stub.
// On non-WASM targets, os.ReadDir works fine; this just delegates to it.

package wasmshell

import (
	"os"
	"path/filepath"
)

func ReadDirCompat(dir string) ([]os.DirEntry, error) {
	return os.ReadDir(dir)
}

func WalkCompat(root string, fn func(path string, info os.FileInfo, err error) error) error {
	return filepath.Walk(root, fn)
}
