package kit

// The project's look on disk: .sprout/look.json, written once per project by
// the look step (GenerateLook) after reading the project's own styling, then
// read by every draw so a mockup of a change to the product looks like it.
//
// It lives in the project, beside the plan, so it travels with the code and
// lands in git history: a look re-derived on every draw would drift, and the
// person reviewing the mockup would see the product's colours change under
// them.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// LookRelPath is the look's place in the project.
const LookRelPath = ".sprout/look.json"

// ErrNoLook is returned by LoadLook when the project has no look yet. It is an
// expected not-found (the look step has not run), not a failure.
var ErrNoLook = errors.New("no project look")

// LoadLook reads the project's look from root. A missing file yields
// ErrNoLook; a file that exists but does not parse is an error (it was
// written by us and should be valid — a corrupt one is worth surfacing, not
// silently re-deriving).
func LoadLook(root string) (Look, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(LookRelPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Look{}, ErrNoLook
		}
		return Look{}, err
	}
	var l Look
	if err := json.Unmarshal(data, &l); err != nil {
		return Look{}, err
	}
	// Clean on read: the file is ours, but it is also in the repository and
	// may have been edited by hand. A look that cleans to nothing is no look.
	clean, ok := l.Clean()
	if !ok {
		return Look{}, ErrNoLook
	}
	return clean, nil
}

// SaveLook writes the project's look to root, cleaned first so an invalid
// value never reaches the file. It reports whether anything was written: a
// look with no usable values is not saved.
func SaveLook(root string, l Look) (bool, error) {
	clean, ok := l.Clean()
	if !ok {
		return false, nil
	}
	path := filepath.Join(root, filepath.FromSlash(LookRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// LookNotes is the look's prose, trimmed; "" when the look has none.
func (l Look) LookNotes() string { return strings.TrimSpace(l.Notes) }
