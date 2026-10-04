// Package starterstore is the file-system-facing half of the SP-153 starter
// manifest: it reads the machine-readable document stored at
// .sprout/starter.json under a project root.
//
// It builds on pkg/startermanifest, which owns the manifest schema and its
// validator (the "pure data + validation contract", with no I/O of its own).
// This package owns the I/O:
//
//   - LoadStarterManifest reads and validates .sprout/starter.json; a missing
//     file (or missing .sprout directory) yields the distinguishable
//     ErrNoManifest sentinel, not a failure.
//
// LoadStarterManifest is the single source of build/test/dev/preview commands
// for the manifest's consumers (SP-149 verification, SP-155 preview, SP-156
// deploys). A missing manifest means "no commands" — the loader never guesses
// or fills in defaults, and an invalid manifest is a hard error, so no caller
// can ever observe a guessed manifest.
//
// The on-disk location is project-relative (.sprout/ under the project root),
// so a manifest travels with the code and lands in git history (SP-153
// §153a). The package is standard-library-only, so it can be imported by the
// CLI, by WASM (browser) builds, and by the manifest's consumers.
package starterstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/startermanifest"
)

// ErrNoManifest is returned by LoadStarterManifest when the project has no
// .sprout/starter.json (or no .sprout directory at all). It is an expected,
// distinguishable not-found (not a failure): callers detect it with
// errors.Is(err, ErrNoManifest) and treat it as "no starter manifest, no
// commands", exactly as planstore.ErrNoPlan marks a missing plan. A project
// without a starter manifest works as it does today (SP-153: projects without
// a starter keep working), so a missing file is the normal "this project has
// no starter" case rather than an error callers must special-case.
var ErrNoManifest = errors.New("no starter manifest found")

// StarterManifestPath returns the path to the project's .sprout/starter.json
// for the given project root.
func StarterManifestPath(root string) string {
	return filepath.Join(root, startermanifest.SproutDir, startermanifest.StarterJSONName)
}

// LoadStarterManifest reads and validates the starter manifest from
// .sprout/starter.json under projectRoot. It is the single source of
// build/test/dev/preview commands for the manifest's consumers: the fields of
// the returned manifest are the authoritative values, and the loader never
// fills in defaults for absent fields (SP-153 §153a: "missing file → none,
// never guessed").
//
//   - When the file does not exist (or .sprout/ does not exist),
//     LoadStarterManifest returns (nil, ErrNoManifest) — the normal "project
//     has no starter" case.
//   - When the file is corrupt JSON or fails validation, LoadStarterManifest
//     returns (nil, err), where err wraps a JSON decode error or a
//     *startermanifest.ValidationError. An invalid manifest is a hard error,
//     never a guessed one.
//   - When the file is valid, LoadStarterManifest returns the validated
//     manifest and a nil error.
//
// On any error the returned manifest is nil, so a caller can never observe
// an invalid or guessed manifest through the loader.
func LoadStarterManifest(projectRoot string) (*startermanifest.StarterManifest, error) {
	path := StarterManifestPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoManifest
		}
		return nil, fmt.Errorf("read starter manifest %s: %w", path, err)
	}

	m, err := startermanifest.ValidateJSON(data)
	if err != nil {
		// Discard the decoded-but-invalid manifest so callers only ever see
		// a valid manifest (or an error) from the loader.
		return nil, fmt.Errorf("load starter manifest %s: %w", path, err)
	}
	return m, nil
}
