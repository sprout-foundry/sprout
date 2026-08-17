package agent

import (
	"os"
	"path/filepath"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// writeFileAtomic durably replaces path's contents: write to a temp file in
// the same directory, fsync, then rename over the destination. A crash at
// any point leaves either the old or the new file — never a torn write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return agenterrors.Wrap(err, "failed to create state directory")
	}
	tmp, err := os.CreateTemp(dir, ".sprout-state-tmp-*")
	if err != nil {
		return agenterrors.Wrap(err, "failed to create temp file")
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return agenterrors.Wrap(err, "failed to write temp file")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return agenterrors.Wrap(err, "failed to sync temp file")
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return agenterrors.Wrap(err, "failed to close temp file")
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath)
		return agenterrors.Wrap(err, "failed to set temp file permissions")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return agenterrors.Wrap(err, "failed to rename temp file into place")
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// backupFileWithExt copies the current contents of src to src+ext,
// overwriting any previous backup. Best-effort: errors are returned to the
// caller, who treats them as non-fatal.
func backupFileWithExt(src, ext string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return agenterrors.Wrap(err, "failed to read file for backup")
	}
	return writeFileAtomic(src+ext, data, 0o600)
}
