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

//go:embed templates/runtime/sprout-screens.js templates/runtime/chrome.css
//go:embed templates/runtime/base/phone.html templates/runtime/base/desktop.html
var runtimeTemplates embed.FS

// runtimeTemplateDir is the embedded prefix the runtime kit lives under.
const runtimeTemplateDir = "templates/runtime/"

// RuntimeAssets are the SP-143 fixed screen-kit assets the scaffold copies
// into design/runtime/, in copy order: the runtime itself, device chrome,
// then the base documents new screens start from. They are versioned assets,
// not user content — Scaffold never overwrites an existing file, so a tree
// may pin its own copy without the scaffold fighting it.
var RuntimeAssets = []string{
	"sprout-screens.js",
	"chrome.css",
	"base/phone.html",
	"base/desktop.html",
}

// ErrManifestExists is returned by Scaffold when the design manifest
// already exists, so callers can detect that a scaffold would be a no-op
// rather than clobbering user work.
var ErrManifestExists = errors.New("design manifest already exists")

// ErrManifestIsDirectory is returned by Scaffold when the manifest path
// exists but is a directory, which Scaffold cannot replace with a file.
var ErrManifestIsDirectory = errors.New("design manifest path is a directory")

// Scaffold creates the design workspace tree under dir: the design/
// root, each canonical subdirectory with a .gitkeep placeholder (git
// does not track empty directories), the manifest from the embedded
// template, and the workspace git contract (SP-140-1 §1h).
//
// The git contract appends GitAttributesDiffHTMLLine to the workspace
// .gitattributes and GitIgnoreCacheLine to the workspace .gitignore when the
// line is absent, creating either file when missing. Existing rules are never
// clobbered — the lines are appended, and an already-satisfied contract
// (including one covered by a broader rule) is a no-op.
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

	if err := ScaffoldRuntimeAssets(designRoot); err != nil {
		return err
	}

	if err := AppendGitContract(dir); err != nil {
		return err
	}
	return nil
}

// ScaffoldRuntimeAssets copies the SP-143 screen-kit assets (device chrome
// + base templates) from the embedded templates into root/design/runtime/,
// creating the directory. Idempotent and overwrite-free: an existing asset
// is left byte-for-byte alone, so a scaffold over a brownfield tree never
// clobbers user work — the same posture as the manifest.
func ScaffoldRuntimeAssets(designRoot string) error {
	for _, name := range RuntimeAssets {
		target := filepath.Join(designRoot, RuntimeSubdir, filepath.FromSlash(name))
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("checking %s: %w", target, err)
		}
		content, err := runtimeTemplates.ReadFile(runtimeTemplateDir + name)
		if err != nil {
			return fmt.Errorf("reading embedded runtime asset %s: %w", name, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", target, err)
		}
	}
	return nil
}

// AppendGitContract appends the SP-140-1 §1h lines to the workspace
// .gitattributes and .gitignore when absent, creating either file when
// missing. It is append-only: existing lines are preserved verbatim, and a
// contract already satisfied (directly or via a broader ignore rule) is left
// alone. For example, appending .gitattributes to:
//
//   - text=auto eol=lf
//     *.go text eol=lf
//
// yields the same rules plus "design/**/*.svg diff=html" at the end — the
// repo's text/binary rules are never clobbered. design/generated/ is
// deliberately never added (SP-140-1 §1h leaves that choice to the project).
func AppendGitContract(dir string) error {
	attrsPath := filepath.Join(dir, GitContractFile)
	attrs, err := os.ReadFile(attrsPath)
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("reading %s: %w", attrsPath, err)
	case err == nil && hasGitAttributesDiffHTMLLine(string(attrs)):
		// Already satisfied — leave the file untouched.
	default:
		if err := appendLine(attrsPath, attrs, GitAttributesDiffHTMLLine); err != nil {
			return err
		}
	}

	ignorePath := filepath.Join(dir, GitIgnoreFile)
	ignore, err := os.ReadFile(ignorePath)
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("reading %s: %w", ignorePath, err)
	case err == nil && hasGitIgnorePattern(string(ignore), GitIgnoreCacheLine):
		// Already satisfied — leave the file untouched.
	default:
		if err := appendLine(ignorePath, ignore, GitIgnoreCacheLine); err != nil {
			return err
		}
	}
	return nil
}

// appendLine appends line to path, creating the file when it does not exist
// and adding a separating newline first when the existing content does not end
// with one. An empty or missing file is written as the single line.
func appendLine(path string, existing []byte, line string) error {
	body := existing
	if len(body) > 0 {
		if body[len(body)-1] != '\n' {
			body = append(body, '\n')
		}
	}
	body = append(body, line...)
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
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
