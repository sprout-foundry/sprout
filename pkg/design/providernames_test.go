package design

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// SP-140-1 item 1.11 — proprietary-name grep over the design tier.
//
// TestDesignTierNoProprietaryNames is the Acceptance-Criterion grep test that
// mirrors SP-137's TestVisionTierNoProviderNames: no design-tier source file may
// name a proprietary design-tool product (figma, penpot, sketch, illustrator,
// adobe, photoshop). The design tree's formats are deliberately open (SP-140-1
// §1), so the code that validates them must not couple to a vendor.
//
// Scope (per the AC) is the new design-tier Go files and the webui design
// components — never docs, specs, or fixtures:
//
//   - Go:        pkg/design/*.go (the design validators)
//   - Go:        pkg/agent_tools/design_validate_handler.go (its ToolHandler)
//   - webui:     src/components/design/**, src/design/** (SP-140-3, once it exists)
//
// The scan walks the real repository (two levels up from this package) rather
// than a copied fixture tree, so the guard fails the moment a name is added to
// a real design-tier file.
// -----------------------------------------------------------------------------

// TestDesignTierNoProprietaryNames scans the design-tier Go files and fails on
// any proprietary product name, naming the exact file and line.
func TestDesignTierNoProprietaryNames(t *testing.T) {
	root := repoRoot(t)
	files := designTierGoFiles(t, root)
	require.NotEmpty(t, files, "expected design-tier Go files under pkg/design and pkg/agent_tools")

	// Sanity: the guard is only meaningful if it actually reads the design
	// validators. pkg/design must contribute at least one file, and the
	// handler must be in scope too.
	assert.Contains(t, files, filepath.Join("pkg", "design", "design.go"),
		"design-tier Go scan must include pkg/design production files")
	assert.Contains(t, files, filepath.Join("pkg", "agent_tools", "design_validate_handler.go"),
		"design-tier Go scan must include the design_validate handler")
	assert.Contains(t, files, filepath.Join("pkg", "agent_tools", "design_assets_handler.go"),
		"design-tier Go scan must include the design_assets handler")

	assert.Empty(t, scanFilesForProprietaryNames(t, root, files),
		"design-tier Go files must not name a proprietary design tool (SP-140-1 AC)")
}

// TestDesignTierSharedScannerCoversHandler proves the shared scanner — not just
// the pkg/agent_tools half of the guard — actually reaches
// design_validate_handler.go. It injects that file (with a seeded proprietary
// name) through the fake scanner and requires a hit, so the scan scope cannot
// silently shrink to pkg/design only.
func TestDesignTierSharedScannerCoversHandler(t *testing.T) {
	fs := newFakeScanner(map[string]string{
		"pkg/design/clean.go":                        "package design\n",
		"pkg/agent_tools/design_validate_handler.go": "package tools\n// built for Penpot export\n",
	})
	hits, present, err := ScanProviderNames(".", "go", fs)
	require.NoError(t, err)
	require.True(t, present)
	require.Len(t, hits, 1, "the shared scanner must scan the design_validate handler file")
	assert.Equal(t, "pkg/agent_tools/design_validate_handler.go", hits[0].File)
	assert.Equal(t, 2, hits[0].Line)
}

// TestProprietaryNameList pins the hardcoded word list the AC fixes, in order.
func TestProprietaryNameList(t *testing.T) {
	want := []string{"figma", "penpot", "sketch", "illustrator", "adobe", "photoshop"}
	assert.Equal(t, want, ProprietaryNames,
		"the proprietary-name list is fixed by SP-140-1's Acceptance Criteria")
	for _, name := range ProprietaryNames {
		assert.Equal(t, strings.ToLower(name), name, "list entries must be lowercase")
	}
}

// TestProviderNameListHolderOnlyEnumerates pins the production exemption: every
// occurrence of a proprietary name in providernames.go must be a quoted list
// literal or inside a comment — never an arbitrary use. This is the SP-137
// "clientType carve-out" analogue.
//
// The guard test file (providernames_test.go) is the second exemption; it is not
// held to this shape because its whole job is to seed fixture strings that
// contain the names (exactly as SP-137's own TestVisionTierNoProviderNames file
// does) and to assert on them.
func TestProviderNameListHolderOnlyEnumerates(t *testing.T) {
	root := repoRoot(t)
	rel := filepath.Join("pkg", "design", "providernames.go")
	data, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)

	for i, line := range strings.Split(string(data), "\n") {
		lower := strings.ToLower(line)
		for _, name := range ProprietaryNames {
			if !strings.Contains(lower, name) {
				continue
			}
			trimmed := strings.TrimSpace(line)
			// Sanctioned forms: a quoted list literal, or a comment line.
			isLiteral := strings.Contains(trimmed, `"`+name+`"`)
			isComment := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*")
			assert.True(t, isLiteral || isComment,
				"%s:%d uses proprietary name %q outside a literal or comment: %s",
				rel, i+1, name, trimmed)
		}
	}
}

// TestScanProviderNamesContract exercises the scanner contract with the fake
// Scanner so the walk, extension filter, exemption, and multi-hit behavior are
// pinned independently of the repository's current contents.
func TestScanProviderNamesContract(t *testing.T) {
	t.Run("flags a proprietary name with file and line", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/example.go": "package design\n\n// exported to Figma for review\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		require.True(t, present, "a populated root must report presence")
		require.Len(t, hits, 1)
		assert.Equal(t, "pkg/design/example.go", hits[0].File)
		assert.Equal(t, 3, hits[0].Line)
		assert.Equal(t, "// exported to Figma for review", hits[0].Text)
	})

	t.Run("matching is case-insensitive and hits every occurrence", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/a.go": "PHOTOSHOP\nfigma\nIllustrator\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		require.True(t, present)
		require.Len(t, hits, 3)
		assert.Equal(t, []int{1, 2, 3}, []int{hits[0].Line, hits[1].Line, hits[2].Line})
	})

	t.Run("no names yields no hits but reports presence", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/clean.go": "package design\n// vendor-neutral by construction\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		assert.True(t, present)
		assert.Empty(t, hits)
	})

	t.Run("absent root is skipped and reports no presence", func(t *testing.T) {
		fs := newFakeScanner(nil)
		hits, present, err := ScanProviderNames(".", "webui", fs)
		require.NoError(t, err)
		assert.False(t, present, "a scan over a missing webui design dir must report absence")
		assert.Empty(t, hits)
	})

	t.Run("non-source extensions are ignored", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/notes.md":  "figma\n",
			"pkg/design/data.json": "{\"tool\": \"penpot\"}\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		assert.True(t, present)
		assert.Empty(t, hits, "markdown and JSON under the tier are not scanned")
	})

	t.Run("the word-list definition file is exempt", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/providernames.go": "var ProprietaryNames = []string{\"figma\", \"penpot\"}\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		assert.True(t, present)
		assert.Empty(t, hits, "the list holder enumerates the words and is exempt")
	})

	t.Run("the guard test file is exempt", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/providernames_test.go": "want := []string{\"figma\"}\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		assert.True(t, present)
		assert.Empty(t, hits, "the guard test enumerates the words and is exempt")
	})

	t.Run("results are sorted by file then line", func(t *testing.T) {
		fs := newFakeScanner(map[string]string{
			"pkg/design/z.go": "package design\n// adobe\n",
			"pkg/design/a.go": "package design\n// sketch\n// photoshop\n",
		})
		hits, present, err := ScanProviderNames(".", "go", fs)
		require.NoError(t, err)
		require.True(t, present)
		require.Len(t, hits, 3)
		assert.Equal(t, []string{
			"pkg/design/a.go", "pkg/design/a.go", "pkg/design/z.go",
		}, []string{hits[0].File, hits[1].File, hits[2].File})
		assert.Equal(t, 2, hits[0].Line)
		assert.Equal(t, 3, hits[1].Line)
	})
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// repoRoot resolves the repository root from this package's test working
// directory (pkg/design).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	info, err := os.Stat(root)
	require.NoError(t, err, "repository root must be reachable from the package test dir")
	require.True(t, info.IsDir())
	return root
}

// designTierGoFiles collects the design-tier Go files: every non-test *.go under
// pkg/design (the design validators) plus design_validate_handler.go under
// pkg/agent_tools. Synthetic test fixtures elsewhere — and the guard tests
// themselves, which enumerate the word list like SP-137's
// TestVisionTierNoProviderNames does — are outside this scope by construction.
func designTierGoFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string

	designDir := filepath.Join(root, "pkg", "design")
	require.NoError(t, filepath.WalkDir(designDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	}))

	files = append(files, filepath.ToSlash(filepath.Join("pkg", "agent_tools", "design_validate_handler.go")))
	files = append(files, filepath.ToSlash(filepath.Join("pkg", "agent_tools", "design_assets_handler.go")))
	files = append(files, filepath.ToSlash(filepath.Join("pkg", "agent_tools", "design_render_handler.go")))
	files = append(files, filepath.ToSlash(filepath.Join("pkg", "agent_tools", "design_render_mermaid.go")))
	sort.Strings(files)
	return files
}

// scanFilesForProprietaryNames reads each file and returns the formatted
// failure lines for any proprietary-name occurrence. It applies the same
// word-list-holder exemption the production scanner does: providernames.go
// enumerates the vocabulary and is the one sanctioned place the names appear.
func scanFilesForProprietaryNames(t *testing.T, root string, files []string) []string {
	t.Helper()
	var problems []string
	for _, rel := range files {
		if providerScanExemptBasenames[filepath.Base(filepath.FromSlash(rel))] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		require.NoError(t, err, "read design-tier file %s", rel)
		for i, line := range strings.Split(string(data), "\n") {
			lower := strings.ToLower(line)
			for _, name := range ProprietaryNames {
				if strings.Contains(lower, name) {
					problems = append(problems, formatHit(rel, i+1, name, line))
					break
				}
			}
		}
	}
	return problems
}

// formatHit renders one failure line in the style of SP-137's grep test.
func formatHit(file string, line int, name, text string) string {
	return file + ":" + itoa(line) + " references proprietary name " + name + ": " + strings.TrimSpace(text)
}

// itoa is a tiny local integer formatter to keep the test dependency-free of
// strconv import churn in failure messages.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// fakeScanner serves ScanProviderNames from an in-memory path->content map so
// the scan contract can be tested without disk. Directories are derived from
// the keys.
type fakeScanner struct {
	dirs     map[string]bool
	contents map[string]string
}

func newFakeScanner(files map[string]string) *fakeScanner {
	fs := &fakeScanner{dirs: map[string]bool{}, contents: map[string]string{}}
	for rel, body := range files {
		clean := filepath.ToSlash(rel)
		fs.contents[clean] = body
		dir := filepath.ToSlash(filepath.Dir(clean))
		for dir != "." && dir != "/" {
			fs.dirs[dir] = true
			dir = filepath.ToSlash(filepath.Dir(dir))
		}
	}
	return fs
}

func (f *fakeScanner) HasAnyDir(roots ...string) bool {
	for _, root := range roots {
		if f.dirs[filepath.ToSlash(root)] {
			return true
		}
	}
	return false
}

// HasAnyFile reports whether any path is a known in-memory file.
func (f *fakeScanner) HasAnyFile(paths ...string) bool {
	for _, p := range paths {
		if _, ok := f.contents[filepath.ToSlash(p)]; ok {
			return true
		}
	}
	return false
}

// ReadFile serves in-memory content for the paths WalkDir emits.
func (f *fakeScanner) ReadFile(path string) ([]byte, error) {
	if body, ok := f.contents[filepath.ToSlash(path)]; ok {
		return []byte(body), nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeScanner) WalkDir(root string, fn func(path string, d os.DirEntry, err error) error) error {
	root = filepath.ToSlash(filepath.Clean(root))
	// A direct file path: emit just that file (mirrors filepath.WalkDir on a
	// non-directory root, which visits the root itself).
	if _, isFile := f.contents[root]; isFile {
		return fn(root, fakeDirEntry{name: filepath.Base(root)}, nil)
	}
	visited := map[string]bool{}
	for rel := range f.contents {
		if !strings.HasPrefix(rel, root+"/") {
			continue
		}
		// Emit the directory entries on the way down so SkipDir and the
		// hidden-dir rule behave like filepath.WalkDir.
		prefix := root
		if prefix != "." {
			prefix += "/"
		}
		rest := strings.TrimPrefix(rel, prefix)
		parts := strings.Split(rest, "/")
		for i := 0; i < len(parts)-1; i++ {
			dir := prefix + strings.Join(parts[:i+1], "/")
			if !visited[dir] {
				visited[dir] = true
				if err := fn(dir, fakeDirEntry{name: parts[i], dir: true}, nil); err != nil {
					if err == filepath.SkipDir {
						break
					}
					return err
				}
			}
		}
		if err := fn(rel, fakeDirEntry{name: parts[len(parts)-1]}, nil); err != nil {
			return err
		}
	}
	return nil
}

// fakeDirEntry satisfies os.DirEntry for the in-memory scanner.
type fakeDirEntry struct {
	name string
	dir  bool
}

func (e fakeDirEntry) Name() string               { return e.name }
func (e fakeDirEntry) IsDir() bool                { return e.dir }
func (e fakeDirEntry) Type() os.FileMode          { return 0 }
func (e fakeDirEntry) Info() (os.FileInfo, error) { return nil, os.ErrNotExist }
