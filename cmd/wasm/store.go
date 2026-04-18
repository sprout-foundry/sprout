//go:build js && wasm

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall/js"

	"github.com/alantheprice/ledit/pkg/wasmshell"
)

// Store manages IndexedDB persistence for the virtual filesystem.
// Files written/deleted in MEMFS are synced to IndexedDB via JS callbacks.
// It implements wasmshell.StoreWriter so it can be plugged into the shell.
type Store struct {
	mu      sync.Mutex
	jsStore js.Value
}

var store *Store

// IDBFile represents a file stored in IndexedDB.
type IDBFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	ModTime int64  `json:"modTime"`
}

// newStore creates the IndexedDB persistence bridge.
func newStore() *Store {
	return &Store{}
}

// initStore grabs the __leditStore global set by JS before WASM init,
// restores all stored files into MEMFS, and returns any errors.
func (s *Store) initStore() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	global := js.Global()
	jsStore := global.Get("__leditStore")
	if !jsStore.Truthy() {
		// No store available — run in-memory only.
		return ""
	}
	s.jsStore = jsStore

	// List files from IndexedDB and restore them.
	listFn := jsStore.Get("listFiles")
	if !listFn.Truthy() {
		return "store.listFiles is not a function"
	}

	result := listFn.Invoke()
	if result.IsUndefined() || result.IsNull() {
		return ""
	}

	jsonStr := result.String()
	if jsonStr == "" || jsonStr == "undefined" {
		return ""
	}

	var files []IDBFile
	if err := json.Unmarshal([]byte(jsonStr), &files); err != nil {
		return "failed to parse stored files: " + err.Error()
	}

	for _, f := range files {
		// Ensure parent directories exist.
		dir := filepath.Dir(f.Path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		if err := os.WriteFile(f.Path, []byte(f.Content), 0644); err != nil {
			continue
		}
	}

	return ""
}

// saveFileSync persists a file to IndexedDB.
func (s *Store) saveFileSync(path, content string) {
	if s.jsStore.IsUndefined() || !s.jsStore.Truthy() {
		return
	}
	saveFn := s.jsStore.Get("saveFile")
	if !saveFn.Truthy() {
		return
	}
	saveFn.Invoke(path, content)
}

// deleteFileSync removes a file from IndexedDB.
func (s *Store) deleteFileSync(path string) {
	if s.jsStore.IsUndefined() || !s.jsStore.Truthy() {
		return
	}
	deleteFn := s.jsStore.Get("deleteFile")
	if !deleteFn.Truthy() {
		return
	}
	deleteFn.Invoke(path)
}

// SaveFile is the public thread-safe save method (implements wasmshell.StoreWriter).
func (s *Store) SaveFile(path, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveFileSync(path, content)
}

// DeleteFile is the public thread-safe delete method (implements wasmshell.StoreWriter).
func (s *Store) DeleteFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteFileSync(path)
}

// Ensure Store implements wasmshell.StoreWriter at compile time.
var _ wasmshell.StoreWriter = (*Store)(nil)

// RecursiveSync removes deleted files from IndexedDB by walking MEMFS.
// This is a best-effort reconciliation after rm -rf etc.
func (s *Store) recursiveSync(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.jsStore.IsUndefined() || !s.jsStore.Truthy() {
		return
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err == nil {
				relPath := strings.TrimPrefix(path, "/")
				relPath = strings.TrimPrefix(relPath, "./")
				s.saveFileSync(relPath, string(data))
			}
		}
		return nil
	})
}
