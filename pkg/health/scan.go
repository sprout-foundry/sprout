package health

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/ast"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// Defaults for the size and complexity scan. They are deliberately
// conservative: a health check that flags everything is noise. Both are
// overridable on Config.
const (
	// DefaultMaxFileLines is the line count at which a source file is
	// reported as too long.
	DefaultMaxFileLines = 800
	// DefaultMaxComplexity is the branching-count proxy at which a function
	// is reported as too complex.
	DefaultMaxComplexity = 15
	// DefaultMaxFiles bounds how many files a scan reads, so a health check
	// on a huge tree stays bounded and fast. Files are visited in lexical
	// order, so the bound is deterministic.
	DefaultMaxFiles = 2000
	// maxScanFileBytes skips generated/vendored blobs that would dominate
	// the line counts.
	maxScanFileBytes = 2 << 20 // 2 MiB
)

// FunctionComplexity is one function's branching-complexity proxy: 1 plus the
// number of branch nodes in its body (the classic McCabe base).
type FunctionComplexity struct {
	// File is the workspace-relative path.
	File string `json:"file"`
	// Name is the function's name (as the parser reports it).
	Name string `json:"name"`
	// Line is the 1-based start line.
	Line int `json:"line"`
	// Complexity is the branching proxy, >= 1.
	Complexity int `json:"complexity"`
}

// SourceStats summarises one source file.
type SourceStats struct {
	// File is the workspace-relative path.
	File string `json:"file"`
	// Lines is the file's line count.
	Lines int `json:"lines"`
}

// ScanResult is the raw output of walking a project's source tree: the file
// sizes and the per-function complexity, before they are turned into findings.
type ScanResult struct {
	// Files is every counted source file, sorted by path.
	Files []SourceStats `json:"files"`
	// Functions is every function with a complexity reading, sorted by file
	// then line. Test functions and unsupported files are excluded.
	Functions []FunctionComplexity `json:"functions"`
	// Skipped counts files that were visited but not parsed (too large, or
	// unreadable), so the scan says plainly what it did not look at.
	Skipped int `json:"skipped,omitempty"`
}

// ScanSources walks the source tree rooted at root — skipping vendored, built,
// and hidden directories via the shared skip list — and returns the file line
// counts and per-function complexity for the source files it understands
// (Go, TypeScript, JavaScript, Python). The walk is deterministic (lexical
// order) and bounded by maxFiles. A single unreadable file is skipped, not
// fatal; only an unreadable tree returns an error.
func ScanSources(root string, maxFiles int) (*ScanResult, error) {
	if root == "" {
		return nil, fmt.Errorf("health: scan root is required")
	}
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}

	paths, err := collectSourceFiles(root, maxFiles)
	if err != nil {
		return nil, err
	}

	res := &ScanResult{}
	for _, path := range paths {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || info.Size() > maxScanFileBytes {
			res.Skipped++
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			res.Skipped++
			continue
		}
		rel := path
		if r, relErr := filepath.Rel(root, path); relErr == nil {
			rel = filepath.ToSlash(r)
		}
		res.Files = append(res.Files, SourceStats{File: rel, Lines: CountLines(content)})
		res.Functions = append(res.Functions, fileComplexities(rel, path, content)...)
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].File < res.Files[j].File })
	sort.Slice(res.Functions, func(i, j int) bool {
		if res.Functions[i].File != res.Functions[j].File {
			return res.Functions[i].File < res.Functions[j].File
		}
		return res.Functions[i].Line < res.Functions[j].Line
	})
	return res, nil
}

// collectSourceFiles returns the analyzable source files under root, in
// lexical order, capped at maxFiles. It skips the shared skip-dir list, hidden
// entries, and files whose language the parser does not understand.
func collectSourceFiles(root string, maxFiles int) ([]string, error) {
	var paths []string
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// A single unreadable entry is skipped, not fatal.
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info == nil {
			return nil
		}
		base := info.Name()
		if strings.HasPrefix(base, ".") {
			if info.IsDir() && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if filesystem.IsSkipDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		if !ast.IsSupported(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("health: walk %s: %w", root, walkErr)
	}
	sort.Strings(paths)
	if len(paths) > maxFiles {
		paths = paths[:maxFiles]
	}
	return paths, nil
}

// CountLines returns the number of lines in content: the count of newlines,
// plus one for a final line without a trailing newline. Empty content is 0.
func CountLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	lines := 0
	for _, b := range content {
		if b == '\n' {
			lines++
		}
	}
	if content[len(content)-1] != '\n' {
		lines++
	}
	return lines
}

// fileComplexities parses content as a source file and returns the complexity
// proxy of every function it declares. Unparseable files yield nil.
func fileComplexities(rel, path string, content []byte) []FunctionComplexity {
	result, err := ast.ParseFile(path, content)
	if err != nil {
		return nil
	}
	defer result.Release()

	var out []FunctionComplexity
	for _, sym := range ast.ExtractSymbolsWithMaxDepth(result.Root, result.Bound, result.Language, 3) {
		if sym.Kind != "function" && sym.Kind != "method" {
			continue
		}
		body := sym.Body
		if body == "" && sym.StartLine > 0 && sym.EndLine >= sym.StartLine {
			body = linesFrom(content, sym.StartLine, sym.EndLine)
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		out = append(out, FunctionComplexity{
			File:       rel,
			Name:       sym.Name,
			Line:       sym.StartLine,
			Complexity: BranchingComplexity(body, result.Language),
		})
	}
	return out
}

// linesFrom returns the slice of content between two 1-based inclusive lines.
func linesFrom(content []byte, start, end int) string {
	text := string(content)
	lines := strings.Split(text, "\n")
	if start < 1 || start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}
