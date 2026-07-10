//go:build js && wasm

// fs_compat_wasm.go — O_DIRECTORY-free directory reading for js/wasm.
//
// Go's os.ReadDir opens directories with O_DIRECTORY. On the js/wasm target
// O_DIRECTORY is rejected when Node.js fs constants aren't available (i.e.
// in browsers), producing:
//
//	"syscall.Open: O_DIRECTORY is not supported on Windows"
//
// The workaround (mirroring pkg/wasmshell/wasm_fs_compat.go) is to open the
// directory with O_RDONLY via os.Open — the js/wasm syscall layer
// pre-populates directory entries during Open regardless of O_DIRECTORY —
// then read them with Readdirnames and stat each entry with os.Lstat.
//
// This package deliberately does NOT import pkg/wasmshell (see the comment
// in shell_js.go) so the compat logic is duplicated here in a self-contained
// form. The logic is small and stable.

package tools

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

// walkDirCompat mirrors filepath.WalkDir's semantics using readDirCompat so
// it never opens a directory with O_DIRECTORY. The callback receives an
// os.DirEntry (a compatDirEntry wrapper) just like filepath.WalkDir.
//
// filepath.SkipDir and filepath.SkipAll handling mirror the stdlib's
// filepath.WalkDir implementation: SkipDir on a directory skips that
// directory's children; SkipAll halts the walk entirely; any other non-nil
// error halts the walk and is returned.
func walkDirCompat(root string, fn func(path string, d os.DirEntry, err error) error) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, &compatDirEntry{name: filepath.Base(root), info: nil}, err)
	} else {
		err = walkDirCompatWalk(root, info, fn)
	}
	return err
}

func walkDirCompatWalk(path string, info os.FileInfo, walkFn func(path string, d os.DirEntry, err error) error) error {
	entry := &compatDirEntry{name: filepath.Base(path), info: info}
	if err := walkFn(path, entry, nil); err != nil || info == nil || !info.IsDir() {
		if err == filepath.SkipDir && info != nil && info.IsDir() {
			err = nil
		}
		if err == filepath.SkipAll {
			err = nil
		}
		return err
	}

	entries, err := readDirCompat(path)
	if err != nil {
		// Second call, report the read error.
		if err := walkFn(path, entry, err); err != nil {
			if err == filepath.SkipDir || err == filepath.SkipAll {
				err = nil
			}
			return err
		}
		return nil
	}

	for _, dirEntry := range entries {
		filename := filepath.Join(path, dirEntry.Name())
		// readDirCompat populates Info via os.Lstat, so this never errors.
		fileInfo, _ := dirEntry.Info()
		if err := walkDirCompatWalk(filename, fileInfo, walkFn); err != nil {
			if !dirEntry.IsDir() || err != filepath.SkipDir {
				if err == filepath.SkipAll {
					err = nil
				}
				return err
			}
		}
	}
	return nil
}

// compatDirEntry implements os.DirEntry. The Info it wraps is os.FileInfo
// obtained via os.Lstat, so symlinks are not followed (matching os.ReadDir
// and filepath.WalkDir semantics).
type compatDirEntry struct {
	name string
	info os.FileInfo
}

func (e *compatDirEntry) Name() string                { return e.name }
func (e *compatDirEntry) IsDir() bool                 { return e.info != nil && e.info.IsDir() }
func (e *compatDirEntry) Type() os.FileMode           { return e.info.Mode().Type() }
func (e *compatDirEntry) Info() (os.FileInfo, error)  { return e.info, nil }
