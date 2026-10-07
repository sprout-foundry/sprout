package configuration

import (
	"fmt"
	"os"
)

// writeFileAtomic writes data to path via a temp file + rename so concurrent
// readers never observe a half-written file. The config layer is read on
// every workspace open and on every config load; a bare WriteFile truncates
// the destination first, and a reader in that window fails to parse the file
// ("unexpected end of JSON input") and silently drops the settings in it.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace file: %w", err)
	}
	return nil
}
