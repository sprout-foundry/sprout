package wasmshell

import (
	"os"
	"path/filepath"
	"strings"
)

// StoreWriter is the interface for persisting file writes/deletes to external storage.
// In the WASM build, this is backed by IndexedDB. In tests, a no-op is used.
type StoreWriter interface {
	SaveFile(path, content string)
	DeleteFile(path string)
}

// noopStore is a no-op implementation of StoreWriter for tests.
type noopStore struct{}

func (noopStore) SaveFile(string, string) {}
func (noopStore) DeleteFile(string)      {}

// storeWriter holds the active StoreWriter (defaults to no-op).
var storeWriter StoreWriter = noopStore{}

// SetStoreWriter sets the StoreWriter used for persisting files.
func SetStoreWriter(s StoreWriter) {
	storeWriter = s
}

// SyncWriteFile writes content to the filesystem (MEMFS) and syncs to the StoreWriter.
func SyncWriteFile(path, content string) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	storeWriter.SaveFile(path, content)
	return nil
}

// SyncDeleteFile removes a file from the filesystem (MEMFS) and syncs to the StoreWriter.
func SyncDeleteFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	storeWriter.DeleteFile(path)
	return nil
}

// RecursiveSync removes deleted files from the store by walking the filesystem.
// This is a best-effort reconciliation after operations like rm -rf.
func RecursiveSync(dir string) {
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err == nil {
				relPath := strings.TrimPrefix(path, "/")
				relPath = strings.TrimPrefix(relPath, "./")
				storeWriter.SaveFile(relPath, string(data))
			}
		}
		return nil
	})
}
