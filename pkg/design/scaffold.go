package design

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed templates/manifest.md
var manifestTemplate embed.FS

const manifestTemplatePath = "templates/manifest.md"

// ErrManifestExists is returned by Scaffold when the design manifest
// already exists, so callers can detect that a scaffold would be a no-op
// rather than clobbering user work.
var ErrManifestExists = errors.New("design manifest already exists")

// ErrManifestIsDirectory is returned by Scaffold when the manifest path
// exists but is a directory, which Scaffold cannot replace with a file.
var ErrManifestIsDirectory = errors.New("design manifest path is a directory")

// Scaffold creates the design workspace tree under dir: the design/
// root, each canonical subdirectory with a .gitkeep placeholder (git
// does not track empty directories), and the manifest from the embedded
// template.
//
// Scaffold never overwrites user work: if design/README.md exists with
// content it returns ErrManifestExists (wrapped with the path) and
// leaves the file untouched. A 0-byte manifest is treated as an empty
// placeholder — the greenfield flow creates the tree before filling the
// manifest — and is replaced with the template. If the manifest path is
// a directory it returns ErrManifestIsDirectory. Subdirectory and
// .gitkeep creation is idempotent — existing entries are skipped,
// missing ones are created.
func Scaffold(dir string) error {
	designRoot := filepath.Join(dir, DirName)

	if err := os.MkdirAll(designRoot, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", designRoot, err)
	}

	for _, sub := range Subdirs {
		subDir := filepath.Join(designRoot, sub)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", subDir, err)
		}
		gitkeep := filepath.Join(subDir, ".gitkeep")
		if _, err := os.Stat(gitkeep); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("checking %s: %w", gitkeep, err)
		}
		if err := os.WriteFile(gitkeep, nil, 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", gitkeep, err)
		}
	}

	manifest := filepath.Join(designRoot, ManifestName)
	info, err := os.Stat(manifest)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("scaffolding %s: %w", manifest, ErrManifestIsDirectory)
	case err == nil && info.Size() > 0:
		return fmt.Errorf("scaffolding %s: %w", manifest, ErrManifestExists)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("checking %s: %w", manifest, err)
	}
	// Not present, a 0-byte placeholder, or a non-regular empty entry:
	// proceed to write the template over it.

	template, err := manifestTemplate.ReadFile(manifestTemplatePath)
	if err != nil {
		return fmt.Errorf("reading embedded manifest template: %w", err)
	}
	if err := os.WriteFile(manifest, template, 0o644); err != nil {
		return fmt.Errorf("creating %s: %w", manifest, err)
	}
	return nil
}

// ManifestTemplate returns the embedded starter manifest content, for
// callers that need the template itself (for example to repair a
// manifest or to preview what a scaffold would write).
func ManifestTemplate() ([]byte, error) {
	template, err := manifestTemplate.ReadFile(manifestTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("reading embedded manifest template: %w", err)
	}
	return template, nil
}
