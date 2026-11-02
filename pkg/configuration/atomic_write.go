package configuration

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic writes data to path via a temp file + rename so concurrent
// readers never observe a half-written file. The config layer is read on
// every workspace open and on every config load; a bare WriteFile truncates
// the destination first, and a reader in that window fails to parse the file
// ("unexpected end of JSON input") and silently drops the settings in it.
//
// The temp file gets a unique name in the destination's directory. A fixed
// name (path + ".tmp") is shared by every writer of the same file: when two
// writers overlap — two config managers for one workspace in the same
// process, or two processes — the first rename moves the shared temp file
// away and the second rename fails with "no such file or directory".
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("set temp file permissions: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
