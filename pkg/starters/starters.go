// Package starters owns the embedded, versioned starter trees that sprout
// ships (SP-153 §153b) and the discovery entry points over them.
// Instantiating a starter into a project directory lives in
// instantiate.go; this file is the catalogue: which starters are embedded,
// what version each tree carries, and what its manifest declares.
//
// Layout: each starter is a directory under data/ holding the files a new
// project should start with, plus a top-level starter.json descriptor. The
// descriptor is itself a valid .sprout/starter.json document — the same
// on-disk schema owned by pkg/startermanifest — and is the source of the
// manifest that instantiation writes into the new project. Because the
// version lives inside the tree, a tree and its version always move
// together: bumping a starter's version is a change to its data/
// directory, and SP-153 §153d upgrade detection compares a project's
// manifest version against Version() (the embedded version).
//
// The trees are embedded with //go:embed, so the package is a pure,
// stdlib-only consumer of the repository: the CLI (sprout new --starter,
// 153.4), the web UI starter list and instantiation endpoint (153.6), and
// WASM builds all link the same trees with no on-disk dependency at
// runtime.
//
// This item (TODO 153.3) ships a single minimal, test-only fixture
// starter that proves the mechanism. The product starters (static site,
// web app, web app with data) and their stack skills land in later items
// (153.4–153.8, SP-153 §153b/§153c): a new starter is added by dropping a
// directory with a valid descriptor into data/ — no Go code change.
package starters

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// dataRoot is the in-embed prefix of the starter trees; declared once so
// the discovery and instantiation paths can't drift.
const dataRoot = "data"

// DescriptorName is the file at the root of each embedded starter tree. It
// is a valid .sprout/starter.json document (the pkg/startermanifest
// on-disk schema) declaring the starter's id, version, and commands. It is
// the one tree file that is never copied verbatim into an instantiated
// project: its validated form becomes .sprout/starter.json instead
// (instantiate.go).
const DescriptorName = "starter.json"

// Starter identifies one embedded starter tree.
type Starter struct {
	// ID is the starter identifier (the data/ directory name, which the
	// descriptor must agree with).
	ID string
	// Version is the version of the embedded tree (the descriptor's
	// starter.version).
	Version string
}

// Errors returned by the package's entry points. Callers branch on them
// with errors.Is.
var (
	// ErrUnknownStarter is returned when the requested starter id has no
	// embedded tree (or its descriptor is missing).
	ErrUnknownStarter = errors.New("starters: unknown starter")

	// ErrInvalidStarterID is returned when the requested starter id is
	// not a safe single path segment (empty, ".", "..", or containing a
	// path separator).
	ErrInvalidStarterID = errors.New("starters: invalid starter id")
)

//go:embed data
var dataFS embed.FS

// List returns the embedded starters (id and version), sorted by id.
//
// A malformed embedded tree (a starter directory with a missing, corrupt,
// or id-mismatched descriptor) is a build bug in this repository, so it
// is surfaced as an error, never skipped: the catalogue is what 153.4
// (`sprout new --starter`) and 153.6 (GET /api/starters) display, and a
// silently dropped entry would be a worse failure than a loud one.
func List() ([]Starter, error) {
	entries, err := fs.ReadDir(dataFS, dataRoot)
	if err != nil {
		return nil, fmt.Errorf("starters: list embedded starters: %w", err)
	}
	out := make([]Starter, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := manifestFor(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Starter{ID: m.Starter.ID, Version: m.Starter.Version})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Version returns the version of the embedded tree for starterID. It is
// the "embedded version" side of the SP-153 §153d upgrade check: an
// existing project whose manifest names an older version than this one
// may be offered an upgrade (and the upgrade is never applied silently).
func Version(starterID string) (string, error) {
	m, err := manifestFor(starterID)
	if err != nil {
		return "", err
	}
	return m.Starter.Version, nil
}

// Manifest returns the embedded starter's manifest (its descriptor,
// validated) for starterID. Consumers that need the starter's commands
// without instantiating anything — e.g. the web UI starter list (153.6) —
// use this instead of reading the tree.
func Manifest(starterID string) (*startermanifest.StarterManifest, error) {
	return manifestFor(starterID)
}

// FileCount returns the number of project-content files that
// Instantiate writes for starterID: every regular file of the embedded
// tree except the top-level descriptor (DescriptorName), which becomes
// .sprout/starter.json instead of a copied file. The manifest writeManifest
// produces is not counted — it is not a tree file.
//
// The embedded catalogue (List) and the web UI starter list (153.6)
// report this number, so a caller sees the tree's size without
// instantiating anything. A tree whose descriptor is missing or invalid
// is not a starter the catalogue would list, so FileCount refuses it
// with the same errors as Manifest.
func FileCount(starterID string) (int, error) {
	if !validStarterID(starterID) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidStarterID, starterID)
	}
	if _, err := manifestFor(starterID); err != nil {
		return 0, err
	}
	sub, err := fs.Sub(dataFS, dataRoot+"/"+starterID)
	if err != nil {
		return 0, fmt.Errorf("starters: open embedded tree %q: %w", starterID, err)
	}
	var n int
	if err := fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		// Mirror copyTree's skip rule exactly: a regular file that is the
		// top-level descriptor never lands in the project, so it is not
		// counted.
		if d.Type().IsRegular() && p != DescriptorName {
			n++
		}
		return nil
	}); err != nil {
		return 0, fmt.Errorf("starters: count files in %q: %w", starterID, err)
	}
	return n, nil
}

// manifestFor loads and validates the descriptor for starterID and checks
// the embedded-tree/descriptor consistency (directory name must equal the
// descriptor's starter id, so the catalogue can never point at another
// starter's manifest).
func manifestFor(starterID string) (*startermanifest.StarterManifest, error) {
	if !validStarterID(starterID) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidStarterID, starterID)
	}
	data, err := fs.ReadFile(dataFS, dataRoot+"/"+starterID+"/"+DescriptorName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownStarter, starterID)
		}
		return nil, fmt.Errorf("starters: read starter %q: %w", starterID, err)
	}
	m, err := startermanifest.ValidateJSON(data)
	if err != nil {
		return nil, fmt.Errorf("starters: starter %q descriptor %s is invalid: %w", starterID, DescriptorName, err)
	}
	if m.Starter.ID != starterID {
		return nil, fmt.Errorf("starters: embedded tree %q declares starter id %q (tree and descriptor disagree)", starterID, m.Starter.ID)
	}
	return m, nil
}

// validStarterID reports whether id is a safe single path segment for use
// as an embedded-tree directory name: non-empty, not "." or "..", and
// containing no path separator (either flavour).
func validStarterID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	for _, r := range id {
		if r == '/' || r == '\\' {
			return false
		}
	}
	return true
}
