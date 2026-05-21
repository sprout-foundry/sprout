package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BuildManifest tracks file modification times from the last successful
// BuildIndex call. It allows subsequent builds to skip parsing unchanged
// files, turning a multi-minute full parse into a ~2-second stat sweep.
type BuildManifest struct {
	// Files maps file path → mtime UnixNano at last successful build.
	Files map[string]int64 `json:"files"`

	// ModelHash is the model hash used when this manifest was created.
	// If the model changes, the manifest is invalidated.
	ModelHash string `json:"modelHash"`
}

// ManifestDiff holds the result of comparing the current workspace state
// against a stored manifest.
type ManifestDiff struct {
	ChangedFiles   []string
	UnchangedFiles []string
	DeletedFiles   []string

	// ManifestInvalidated is true when the model hash changed and all
	// files must be re-embedded even if their content hashes match.
	ManifestInvalidated bool
}

// LoadManifest loads a manifest from a JSON file. Returns (nil, nil) if the
// file does not exist. The caller should check both return values.
func LoadManifest(path string) (*BuildManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("embedding: read manifest %s: %w", path, err)
	}

	var m BuildManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("embedding: unmarshal manifest %s: %w", path, err)
	}

	return &m, nil
}

// SaveManifest writes the manifest to path atomically (temp file + rename).
func SaveManifest(path string, m *BuildManifest) error {
	if m == nil {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("embedding: create manifest dir %s: %w", dir, err)
	}

	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("embedding: marshal manifest: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".manifest-tmp-*")
	if err != nil {
		return fmt.Errorf("embedding: create manifest tmp: %w", err)
	}
	tmpPath := tmp.Name()

	writeErr := func(err error) error {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		return writeErr(fmt.Errorf("embedding: write manifest: %w", err))
	}
	if err := tmp.Close(); err != nil {
		return writeErr(fmt.Errorf("embedding: close manifest tmp: %w", err))
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("embedding: rename manifest: %w", err)
	}
	return nil
}

// CheckFileChanged stats the file at path and returns true if its mtime
// (UnixNano) differs from lastMtimeNano. Returns (false, nil) if the file
// does not exist (it was deleted).
func CheckFileChanged(path string, lastMtimeNano int64) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("embedding: stat %s: %w", path, err)
	}
	return info.ModTime().UnixNano() != lastMtimeNano, nil
}

// walkForManifest walks the workspace and returns all file paths, using
// either code-file-only or full indexable-file walking depending on
// indexFileLevel.
func walkForManifest(ctx context.Context, rootDir string, indexFileLevel bool) ([]string, error) {
	if indexFileLevel {
		return WalkAllIndexableFiles(ctx, rootDir)
	}
	return WalkCodeFiles(ctx, rootDir)
}

// DiffManifest compares the current workspace state against a stored manifest.
// It returns a ManifestDiff indicating which files are changed, unchanged, or
// deleted. If manifest.ModelHash differs from currentModelHash, all files are
// treated as changed (manifest invalidated).
func DiffManifest(ctx context.Context, manifest *BuildManifest, currentModelHash, rootDir string, indexFileLevel bool) (*ManifestDiff, error) {
	currentFiles, err := walkForManifest(ctx, rootDir, indexFileLevel)
	if err != nil {
		return nil, fmt.Errorf("embedding: walk for manifest: %w", err)
	}

	diff := &ManifestDiff{}

	// No manifest: first build, treat everything as changed (not invalidated).
	if manifest == nil {
		diff.ChangedFiles = currentFiles
		return diff, nil
	}

	// Model changed: everything needs re-embedding regardless of content.
	if manifest.ModelHash != currentModelHash {
		diff.ChangedFiles = currentFiles
		diff.ManifestInvalidated = true
		return diff, nil
	}

	currentSet := make(map[string]bool, len(currentFiles))
	for _, f := range currentFiles {
		currentSet[f] = true
	}

	// Check current files against manifest.
	for _, f := range currentFiles {
		lastMtime, existsInManifest := manifest.Files[f]
		if !existsInManifest {
			diff.ChangedFiles = append(diff.ChangedFiles, f)
			continue
		}

		isChanged, err := CheckFileChanged(f, lastMtime)
		if err != nil {
			// Can't stat — assume changed to be safe.
			diff.ChangedFiles = append(diff.ChangedFiles, f)
			continue
		}

		if isChanged {
			diff.ChangedFiles = append(diff.ChangedFiles, f)
		} else {
			diff.UnchangedFiles = append(diff.UnchangedFiles, f)
		}
	}

	// Detect deleted files: in manifest but no longer on disk.
	for f := range manifest.Files {
		if !currentSet[f] {
			diff.DeletedFiles = append(diff.DeletedFiles, f)
		}
	}

	return diff, nil
}

// BuildManifestFromFiles creates a new manifest by statting all given files.
func BuildManifestFromFiles(files []string, modelHash string) *BuildManifest {
	m := &BuildManifest{
		Files:     make(map[string]int64, len(files)),
		ModelHash: modelHash,
	}

	for _, f := range files {
		mtime, err := fileModTime(f)
		if err == nil {
			m.Files[f] = mtime
		}
	}

	return m
}

// fileModTime returns the modification time of a file as UnixNano.
func fileModTime(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.ModTime().UnixNano(), nil
}
