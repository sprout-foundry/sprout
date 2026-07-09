//go:build js && wasm

// wasm_fs_compat.go — Workarounds for Go js/wasm syscall limitations.
//
// Go's os.ReadDir calls openDirNolog which uses O_DIRECTORY. On the
// js/wasm target, O_DIRECTORY is rejected when Node.js fs constants
// aren't available (i.e. in browsers), producing:
//   "syscall.Open: O_DIRECTORY is not supported on Windows"
//
// This file provides ReadDirCompat that uses os.Open (O_RDONLY only)
// then Readdirnames, which works because Go's js/wasm syscall layer
// pre-populates directory entries during Open regardless of O_DIRECTORY.

package wasmshell

import (
	"os"
	"sort"
)

// ReadDirCompat lists directory entries without using O_DIRECTORY.
// On js/wasm, os.Open succeeds (O_RDONLY only), and the js/wasm
// syscall layer pre-reads directory entries into the fd during Open.
// Readdirnames then returns them without needing O_DIRECTORY.
func ReadDirCompat(dir string) ([]os.DirEntry, error) {
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

	var entries []os.DirEntry
	for _, name := range names {
		info, err := os.Lstat(dir + "/" + name)
		if err != nil {
			continue
		}
		entries = append(entries, &compatDirEntry{name: name, info: info})
	}
	return entries, nil
}

// WalkCompat is a drop-in replacement for filepath.Walk that uses
// ReadDirCompat instead of os.ReadDir to avoid the O_DIRECTORY issue.
func WalkCompat(root string, fn func(path string, info os.FileInfo, err error) error) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkCompat(root, info, fn)
	}
	return err
}

func walkCompat(path string, info os.FileInfo, walkFn func(string, os.FileInfo, error) error) error {
	if err := walkFn(path, info, nil); err != nil {
		if info.IsDir() && err != filepath.SkipDir {
			return err
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	entries, err := ReadDirCompat(path)
	if err != nil {
		return walkFn(path, info, err)
	}

	for _, entry := range entries {
		filename := filepath.Join(path, entry.Name())
		fileInfo, err := entry.Info()
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			err = walkCompat(filename, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != filepath.SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

type compatDirEntry struct {
	name string
	info os.FileInfo
}

func (e *compatDirEntry) Name() string               { return e.name }
func (e *compatDirEntry) IsDir() bool                 { return e.info.IsDir() }
func (e *compatDirEntry) Type() os.FileMode           { return e.info.Mode().Type() }
func (e *compatDirEntry) Info() (os.FileInfo, error)  { return e.info, nil }
