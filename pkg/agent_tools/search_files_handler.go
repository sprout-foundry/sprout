package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type searchFilesHandler struct{}

func (h *searchFilesHandler) Name() string {
	return "search_files"
}

func (h *searchFilesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "search_files",
		Description: "Search text pattern in files (cross-platform, ignores .git, node_modules, .sprout by default)",
		Hidden:      true, // superseded by `search`; still callable by name
		Parameters: []ParameterDef{
			{Name: "search_pattern", Type: "string", Description: "Text pattern or regex to search for", Required: true},
			{Name: "directory", Type: "string", Description: "Directory to search (default: .)"},
			{Name: "file_glob", Type: "string", Description: "Glob filter (e.g. *.go)"},
			{Name: "case_sensitive", Type: "boolean", Description: "Case sensitive (default false)"},
			{Name: "max_results", Type: "integer", Description: "Max results (default 50)"},
			{Name: "max_bytes", Type: "integer", Description: "Max bytes of matches (default 102400)"},
		},
		Required: []string{"search_pattern"},
	}
}

func (h *searchFilesHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "search_pattern")
	return err
}

func (h *searchFilesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	searchPattern, err := extractString(args, "search_pattern")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	// Capture whether the user explicitly named a directory before
	// resolveSearchDirectory normalises it.
	rawDir, _ := extractString(args, "directory")

	directory, err := resolveSearchDirectory(rawDir, env.WorkspaceRoot)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	// Gate 1 precheck — only when the user explicitly named a non-default
	// directory. An empty or "." directory resolves to the workspace root,
	// which is already allowlisted; gating the default would prompt on every
	// vanilla search.
	if rawDir != "" && rawDir != "." {
		resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "search_files", directory)
		if decision == "deny" {
			return ToolResult{Output: fmt.Sprintf("read blocked: %s is not accessible from this session", directory), IsError: true},
				fmt.Errorf("read blocked: %s is not accessible", directory)
		}
		if decision == "prompt" && env.FileAccessPrompter != nil {
			if ctx2, approved := promptForOffWorkspacePath(ctx, env, "search_files", directory, resolvedPath, "read"); approved {
				ctx = ctx2
			} else {
				return ToolResult{Output: fmt.Sprintf("read blocked: off-workspace access to %s was not approved", directory), IsError: true},
					fmt.Errorf("read blocked: off-workspace access to %s was not approved", directory)
			}
		}
	}

	maxResults, _ := extractInt(args, "max_results")
	if maxResults <= 0 {
		maxResults = 50
	}
	maxBytes, _ := extractInt(args, "max_bytes")
	if maxBytes <= 0 {
		maxBytes = 102400
	}

	// Shares runLiteralSearch with the fused `search` tool to avoid drift.
	res, err := runLiteralSearch(ctx, literalSearchOpts{
		Directory:     directory,
		Pattern:       searchPattern,
		FileGlob:      firstString(args, "file_glob"),
		CaseSensitive: getBoolArg(args, "case_sensitive"),
		MaxFiles:      maxResults,
		MaxBytes:      maxBytes,
	})
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error: %v", err), IsError: true}, nil
	}

	lines := make([]string, 0, len(res.Hits))
	for _, hit := range res.Hits {
		lines = append(lines, hit.String())
	}

	output := formatSearchResults(lines, directory, searchPattern, len(res.Hits), maxResults)
	if res.Truncated {
		output += fmt.Sprintf("\n(showing %d of %d matching files — narrow the pattern or raise max_results)\n",
			res.FilesShown, res.FilesMatched)
	}

	// When the literal pass finds nothing and semantic search can answer, say so.
	if len(res.Hits) == 0 && env.EmbeddingMgr != nil && env.EmbeddingMgr.Readiness().CanAnswerQueries() {
		output = fmt.Sprintf("No text matches for '%s' in %s.\n\nThe embedding index is available — `search` with a plain-language description will also find code that uses different wording.", searchPattern, directory)
	}

	return ToolResult{Output: output, IsError: false}, nil
}

// firstString returns a string arg or "".
func firstString(args map[string]any, key string) string {
	v, _ := extractString(args, key)
	return v
}

func (h *searchFilesHandler) Aliases() []string      { return nil }
func (h *searchFilesHandler) Timeout() time.Duration { return 30 * time.Second }
func (h *searchFilesHandler) MaxResultSize() int     { return 0 }
func (h *searchFilesHandler) SafeForParallel() bool  { return false }
func (h *searchFilesHandler) Interactive() bool      { return false }

func compileSearchPattern(pattern string, caseSensitive bool) (*regexp.Regexp, error) {
	var raw string
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) > 2 {
		raw = pattern[1 : len(pattern)-1]
		if !caseSensitive {
			raw = "(?i)" + raw
		}
	} else {
		raw = regexp.QuoteMeta(pattern)
		if !caseSensitive {
			raw = "(?i)" + raw
		}
	}
	return regexp.Compile(raw)
}

// shouldSkipDir returns true for well-known directories that should never be
// searched. Delegates to the canonical shared list in pkg/filesystem.
func shouldSkipDir(path string) bool {
	name := filepath.Base(path)
	if filesystem.IsSkipDir(name) {
		return true
	}
	return false
}

// binaryExtensions lists file extensions known to be binary or non-text formats.
var binaryExtensions = map[string]bool{
	// images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
	".bmp": true, ".tiff": true, ".tif": true, ".webp": true, ".svgz": true,
	// fonts
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	// archives & packages
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".rar": true, ".jar": true, ".war": true, ".aar": true,
	".apk": true, ".ipa": true, ".dmg": true, ".pkg": true, ".deb": true, ".rpm": true,
	// compiled binaries & libraries
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
	".o": true, ".obj": true, ".class": true, ".wasm": true, ".bin": true,
	// media
	".mp3": true, ".mp4": true, ".mov": true, ".avi": true, ".mkv": true,
	".wav": true, ".flac": true, ".ogg": true, ".m4a": true, ".webm": true,
	// documents (binary formats)
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".pages": true, ".numbers": true, ".key": true,
	// database & data formats
	".db": true, ".sqlite": true, ".sqlite3": true, ".dat": true,
	".ldb": true, ".sst": true,
	// compiled / serialized
	".tsbuildinfo": true, ".map": true,
	// certificates & keys
	".p12": true, ".pfx": true, ".der": true, ".cer": true, ".crt": true,
	".jks": true, ".keystore": true, ".keychain": true,
	// iOS / macOS bundles
	".xcworkspace": true, ".xcuserstate": true,
	".nib": true, ".car": true, ".mom": true, ".momd": true,
	".storyboardc": true, ".xcdatamodeld": true,
	// Android
	".dex": true, ".ap_": true,
}

func isTextFile(path string) bool {
	// Fast path: skip known binary extensions without opening the file.
	ext := strings.ToLower(filepath.Ext(path))
	if binaryExtensions[ext] {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	buf = buf[:n]

	for _, b := range buf {
		if b == 0 {
			return false
		}
	}
	return true
}

func searchFile(path string, pattern *regexp.Regexp) ([]string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	lines := strings.Split(string(data), "\n")
	var results []string
	totalBytes := 0

	for i, line := range lines {
		if pattern.MatchString(line) {
			formatted := fmt.Sprintf("%s:%d:%s", path, i+1, line)
			results = append(results, formatted)
			totalBytes += len(formatted)
		}
	}

	return results, totalBytes, nil
}

func formatSearchResults(results []string, directory, pattern string, matchCount, maxResults int) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for '%s' in %s", pattern, directory)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d result(s) for '%s' in %s:\n", matchCount, pattern, directory))

	printed := 0
	for _, r := range results {
		if printed >= maxResults {
			sb.WriteString(fmt.Sprintf("\n... (%d more results not shown)", matchCount-printed))
			break
		}
		sb.WriteString(r + "\n")
		printed++
	}

	return sb.String()
}

type globMatcher interface {
	Match(path string) bool
}

func newGlobMatcher(pattern string) globMatcher {
	if pattern == "" {
		return nil
	}
	return &simpleGlobMatcher{pattern: pattern}
}

type simpleGlobMatcher struct {
	pattern string
}

func (m *simpleGlobMatcher) Match(path string) bool {
	base := filepath.Base(path)
	matched, err := filepath.Match(m.pattern, base)
	if err != nil {
		return true
	}
	return matched
}
