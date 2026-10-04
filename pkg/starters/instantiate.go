package starters

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// ErrNonEmptyDestination is returned by Instantiate when the destination
// exists and contains any file or directory. The refusal happens before
// any write, so a refused call leaves the destination exactly as it was.
var ErrNonEmptyDestination = errors.New("starters: destination directory is not empty")

// dirPerm and filePerm are the permissions of the files and directories
// Instantiate creates. A starter tree is ordinary project content, not
// executable-by-permission material.
const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// Instantiate copies the embedded starter tree for starterID into destDir
// and writes the starter's manifest to .sprout/starter.json under destDir,
// so the new project immediately carries a manifest that
// pkg/starterstore.LoadStarterManifest loads — the manifest is the single
// source of build/test/dev/preview commands for the project's later
// consumers (SP-149 verification, SP-155 preview, SP-156 deploys).
//
// Destination contract:
//
//   - destDir does not exist: it is created, including any missing parents.
//   - destDir exists and is an empty directory: the tree is written into it.
//   - destDir exists and is a file: a clear error.
//   - destDir exists and contains anything: Instantiate refuses with
//     ErrNonEmptyDestination and writes nothing.
//
// The copy includes every file and directory of the starter tree except
// the top-level descriptor (DescriptorName): the descriptor is not
// project content, its validated form is what becomes .sprout/starter.json.
//
// On a mid-copy failure (e.g. disk full) destDir may be left partially
// populated; retry into a fresh directory. A refused non-empty
// destination is always left untouched, because the emptiness check runs
// before any write.
func Instantiate(starterID, destDir string) error {
	if destDir == "" {
		return errors.New("starters: destination directory is required")
	}
	m, err := manifestFor(starterID)
	if err != nil {
		return err
	}
	if err := prepareDestination(destDir); err != nil {
		return err
	}
	if err := copyTree(starterID, destDir); err != nil {
		return err
	}
	return writeManifest(destDir, m)
}

// prepareDestination enforces the destination contract. It performs no
// writes when the destination is non-empty (or a file), so a refusal
// leaves the directory exactly as it was.
func prepareDestination(destDir string) error {
	info, err := os.Stat(destDir)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(destDir, dirPerm); err != nil {
				return fmt.Errorf("starters: create destination %s: %w", destDir, err)
			}
			return nil
		}
		return fmt.Errorf("starters: stat destination %s: %w", destDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("starters: destination %s is not a directory", destDir)
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return fmt.Errorf("starters: read destination %s: %w", destDir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%w: %s", ErrNonEmptyDestination, destDir)
	}
	return nil
}

// copyTree copies the embedded starter tree into destDir, skipping the
// top-level descriptor (which becomes .sprout/starter.json instead of a
// copied file).
func copyTree(starterID, destDir string) error {
	sub, err := fs.Sub(dataFS, dataRoot+"/"+starterID)
	if err != nil {
		return fmt.Errorf("starters: open embedded tree %q: %w", starterID, err)
	}
	return fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == "." {
			return nil
		}
		// The top-level descriptor is not project content.
		if p == DescriptorName && d.Type().IsRegular() {
			return nil
		}
		target := filepath.Join(destDir, p)
		if d.IsDir() {
			if err := os.MkdirAll(target, dirPerm); err != nil {
				return fmt.Errorf("starters: create %s: %w", target, err)
			}
			return nil
		}
		data, err := fs.ReadFile(sub, p)
		if err != nil {
			return fmt.Errorf("starters: read embedded %s: %w", p, err)
		}
		if err := os.WriteFile(target, data, filePerm); err != nil {
			return fmt.Errorf("starters: write %s: %w", target, err)
		}
		return nil
	})
}

// writeManifest writes the validated manifest as the project's
// .sprout/starter.json (canonical indented form, trailing newline) and
// creates the .sprout directory if needed.
func writeManifest(destDir string, m *startermanifest.StarterManifest) error {
	sproutDir := filepath.Join(destDir, startermanifest.SproutDir)
	if err := os.MkdirAll(sproutDir, dirPerm); err != nil {
		return fmt.Errorf("starters: create %s: %w", sproutDir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("starters: marshal starter manifest: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(sproutDir, startermanifest.StarterJSONName)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		return fmt.Errorf("starters: write %s: %w", path, err)
	}
	return nil
}
