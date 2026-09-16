package design

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProprietaryNames is the hardcoded word list SP-140-1's Acceptance Criteria
// fixes for the design tier: the six proprietary design-tool product names.
//
// The design tree's formats are deliberately open (W3C DTCG tokens, SVG,
// mermaid, self-contained HTML — see SP-140-1 §1), so the design-tier code must
// never couple itself to a specific vendor. The list is versioned here, not in
// the guard test, so any future design-tier file (Go or webui) can be scanned
// with the same vocabulary without duplicating it.
//
// One entry is load-bearing: the "sketch" product name collides with SP-140-2's
// `design_import_sketch` tool, which takes its name from the *activity* of
// sketching, not the product. This list is therefore scoped to the SP-140-1
// design-tier files only (pkg/design and its design_validate handler);
// SP-140-2's tool files are covered by their own spelling convention
// (`import_sketch`) rather than this rule.
//
// The literals below are the vocabulary itself — the one sanctioned place the
// names may appear (see providerScanExemptBasenames).
var ProprietaryNames = []string{
	"figma",
	"penpot",
	"sketch",
	"illustrator",
	"adobe",
	"photoshop",
}

// ProviderWordHit is one occurrence of a proprietary product name in a scanned
// file. Line is 1-based; Text is the trimmed offending line so a failure
// message can point at the exact coupling.
type ProviderWordHit struct {
	File string
	Line int
	Text string
}

// Scanner is the read-only file surface ScanProviderNames needs. Tests inject a
// fake so the scan contract can be exercised without touching disk; production
// callers use FSScanner.
type Scanner interface {
	// HasAnyDir reports whether any of the given filesystem paths exists and
	// is a directory.
	HasAnyDir(roots ...string) bool
	// HasAnyFile reports whether any of the given filesystem paths exists and
	// is a regular file.
	HasAnyFile(paths ...string) bool
	// WalkDir visits every regular, non-hidden file under root, honoring
	// SkipDir results. The callback receives the full path and the directory
	// entry so the caller can inspect the mode and control descent.
	WalkDir(root string, fn func(path string, d os.DirEntry, err error) error) error
}

// ContentReader is the optional companion to Scanner: when implemented, the
// scan reads file bodies through it instead of os.ReadFile. FSScanner does not
// implement it (os.ReadFile is correct for real paths); the test fake does.
type ContentReader interface {
	ReadFile(path string) ([]byte, error)
}

// FSScanner is the production Scanner backed by os.Stat and filepath.WalkDir.
type FSScanner struct{}

// HasAnyDir reports whether any root exists and is a directory.
func (FSScanner) HasAnyDir(roots ...string) bool {
	for _, root := range roots {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// HasAnyFile reports whether any path exists and is a regular file.
func (FSScanner) HasAnyFile(paths ...string) bool {
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// WalkDir delegates to filepath.WalkDir; missing roots surface the usual
// *fs.PathError to the callback.
func (FSScanner) WalkDir(root string, fn func(path string, d os.DirEntry, err error) error) error {
	return filepath.WalkDir(root, fn)
}

// tierSpec is the design-tier file surface for one language: the directory
// trees to walk plus explicit, individually-named files that live outside those
// trees. `pkg/design` and its ToolHandlers
// `pkg/agent_tools/design_validate_handler.go` and
// `pkg/agent_tools/design_assets_handler.go` are the SP-140 Go design tier; the
// webui design components live under `src/components/design` and `src/design`
// (SP-140-3).
type tierSpec struct {
	dirs  []string
	files []string
}

// designTier is the language → file-surface map. Keeping the handler as an
// explicit file (rather than adding all of `pkg/agent_tools` as a root) scopes
// the Go scan to the design tier — the ~280 unrelated agent_tools files are
// never read.
var designTier = map[string]tierSpec{
	"go": {
		dirs:  []string{"pkg/design"},
		files: []string{"pkg/agent_tools/design_validate_handler.go", "pkg/agent_tools/design_assets_handler.go"},
	},
	"webui": {
		dirs: []string{"src/components/design", "src/design"},
	},
}

// scanExtensions maps a language to the source-file extensions it scans. The Go
// scan covers every `.go` file under the directory roots, including `_test.go`
// files — that matches TestVisionTierNoProviderNames (the pattern SP-140-1
// cites), whose own source names the providers it forbids and is therefore
// exempt by basename.
var scanExtensions = map[string][]string{
	"go":    {".go"},
	"webui": {".ts", ".tsx"},
}

// providerScanExemptBasenames is the small, explicit carve-out list of files
// whose job is to *enumerate* the forbidden vocabulary:
//
//   - providernames.go — the word-list holder itself.
//   - providernames_test.go — the pkg/design guard, which pins the list order
//     and exercises the scanner with seeded hits (mirrors SP-137, whose own
//     TestVisionTierNoProviderNames file names the providers it forbids).
//
// Exemptions are per-file basenames, never globs, and each is justified here in
// prose; a narrow-shape test (TestProviderNameListHolderOnlyEnumerates) pins
// that the holder file's mentions are only list literals or comments.
var providerScanExemptBasenames = map[string]bool{
	"providernames.go":      true,
	"providernames_test.go": true,
}

// ScanProviderNames walks the design-tier surface for lang (relative to root) —
// the directory trees plus the explicit files in designTier — and returns every
// occurrence of a name in ProprietaryNames, sorted by file then line. Absent
// roots are skipped silently (the webui design directories do not exist until
// SP-140-3 lands). The second return value reports whether any root for lang
// existed, so a caller can fail loudly on a vacuous scan instead of passing
// green over nothing.
func ScanProviderNames(root, lang string, fs Scanner) ([]ProviderWordHit, bool, error) {
	spec, ok := designTier[lang]
	if !ok {
		return nil, false, nil
	}
	if !fs.HasAnyDir(joinAll(root, spec.dirs)...) && !fs.HasAnyFile(joinAll(root, spec.files)...) {
		return nil, false, nil
	}
	exts := scanExtensions[lang]
	readFile := os.ReadFile
	if cr, ok := fs.(ContentReader); ok {
		readFile = cr.ReadFile
	}
	var hits []ProviderWordHit

	scanFile := func(path string, d os.DirEntry) error {
		if !hasScannedExt(path, exts) || providerScanExemptBasenames[d.Name()] {
			return nil
		}
		data, readErr := readFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(data), "\n") {
			lower := strings.ToLower(line)
			for _, name := range ProprietaryNames {
				if strings.Contains(lower, name) {
					hits = append(hits, ProviderWordHit{
						File: rel,
						Line: i + 1,
						Text: strings.TrimSpace(line),
					})
					break // one hit per line, first matching name
				}
			}
		}
		return nil
	}

	for _, dir := range spec.dirs {
		abs := filepath.Join(root, filepath.FromSlash(dir))
		err := fs.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			// Skip hidden directories (build/vendor caches). Guard against
			// filepath.Dir collapsing a top-level component to "." so a
			// relative scan root does not filter its own children.
			if d != nil && d.IsDir() {
				if d.Name() != "." && strings.HasPrefix(d.Name(), ".") && path != abs {
					return filepath.SkipDir
				}
				return nil
			}
			return scanFile(path, d)
		})
		if err != nil && !os.IsNotExist(err) {
			return hits, true, err
		}
	}

	// Explicit files outside the walked trees (the design_validate handler).
	for _, file := range spec.files {
		abs := filepath.Join(root, filepath.FromSlash(file))
		err := fs.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d != nil && d.IsDir() {
				return nil
			}
			return scanFile(path, d)
		})
		if err != nil && !os.IsNotExist(err) {
			return hits, true, err
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
	return hits, true, nil
}

// hasScannedExt reports whether path carries one of the scanned extensions.
func hasScannedExt(path string, exts []string) bool {
	for _, e := range exts {
		if strings.HasSuffix(path, e) {
			return true
		}
	}
	return false
}

// joinAll joins every dir under root, preserving order.
func joinAll(root string, dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, filepath.Join(root, filepath.FromSlash(d)))
	}
	return out
}
