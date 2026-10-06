//go:build js && wasm

// fs_compat_wasm.go — O_DIRECTORY-free directory reading for js/wasm.
//
// Go's os.ReadDir opens directories with O_DIRECTORY. In the browser (js/wasm)
// that flag is rejected by the syscall layer, producing:
//
//	"syscall.Open: O_DIRECTORY is not supported on Windows"
//
// The workaround (mirroring pkg/agent_tools/fs_compat_wasm.go and
// pkg/wasmshell/wasm_fs_compat.go) is to open the directory with O_RDONLY via
// os.Open — the js/wasm syscall layer pre-populates directory entries during
// Open regardless of O_DIRECTORY — then read them with Readdirnames and stat
// each entry with os.Lstat.

package automate

import (
	"os"
	"path/filepath"
	"sort"
)

func readDirCompat(dir string) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	entries := make([]os.DirEntry, 0, len(names))
	for _, name := range names {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		entries = append(entries, &compatDirEntry{name: name, info: info})
	}
	return entries, nil
}

// compatDirEntry implements os.DirEntry. The Info it wraps is os.FileInfo
// obtained via os.Lstat, so symlinks are not followed (matching os.ReadDir
// semantics).
type compatDirEntry struct {
	name string
	info os.FileInfo
}

func (e *compatDirEntry) Name() string               { return e.name }
func (e *compatDirEntry) IsDir() bool                { return e.info != nil && e.info.IsDir() }
func (e *compatDirEntry) Type() os.FileMode          { return e.info.Mode().Type() }
func (e *compatDirEntry) Info() (os.FileInfo, error) { return e.info, nil }
