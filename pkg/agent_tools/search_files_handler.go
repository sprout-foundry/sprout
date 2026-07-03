package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type searchFilesHandler struct{}

func (h *searchFilesHandler) Name() string {
	return "search_files"
}

func (h *searchFilesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "search_files",
		Description: "Search text pattern in files (cross-platform, ignores .git, node_modules, .sprout by default)",
		Parameters: []ParameterDef{
			{Name: "search_pattern", Type: "string", Description: "Text pattern or regex to search for", Required: true},
			{Name: "directory", Type: "string", Description: "Directory to search (default: .)"},
			{Name: "file_glob", Type: "string", Description: "Glob to limit files (e.g., *.go)"},
			{Name: "case_sensitive", Type: "boolean", Description: "Case sensitive search (default: false)"},
			{Name: "max_results", Type: "integer", Description: "Maximum results to return (default: 50)"},
			{Name: "max_bytes", Type: "integer", Description: "Maximum total bytes of matches to return (default: 102400)"},
		},
		Required: []string{"search_pattern"},
	}
}

func (h *searchFilesHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "search_pattern")
	return err
}

func (h *searchFilesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()

	// Event publishing (optional — runs only when EventBus is configured)
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	searchPattern, err := extractString(args, "search_pattern")
	if err != nil {
		return ToolResult{
			Output:  err.Error(),
			IsError: true,
		}, nil
	}

	directory, _ := extractString(args, "directory")
	fileGlob, _ := extractString(args, "file_glob")
	caseSensitive := getBoolArg(args, "case_sensitive")
	maxResults, _ := extractInt(args, "max_results")
	if maxResults <= 0 {
		maxResults = 50
	}
	maxBytes, _ := extractInt(args, "max_bytes")
	if maxBytes <= 0 {
		maxBytes = 102400
	}

	if directory == "" {
		directory = "."
	}

	if directory != "." {
		if strings.Contains(directory, "..") {
			return ToolResult{
				Output:  fmt.Sprintf("invalid search directory: %q", directory),
				IsError: true,
			}, nil
		}
	}

	compiled, err := compileSearchPattern(searchPattern, caseSensitive)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Error: invalid search pattern '%s': %v", searchPattern, err),
			IsError: true,
		}, nil
	}

	matcher := newGlobMatcher(fileGlob)
	results := make([]string, 0)
	totalBytes := 0
	matchCount := 0

	err = filepath.WalkDir(directory, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			if shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}

		if info.Type()&os.ModeSymlink != 0 {
			return nil
		}

		if !isTextFile(path) {
			return nil
		}

		if matcher != nil && !matcher.Match(path) {
			return nil
		}

		if matchCount >= maxResults || totalBytes >= maxBytes {
			return nil
		}

		matches, bytesUsed, err := searchFile(path, compiled)
		if err != nil {
			return nil
		}

		totalBytes += bytesUsed
		results = append(results, matches...)
		matchCount += len(matches)

		return nil
	})

	output := formatSearchResults(results, directory, searchPattern, matchCount, maxResults)
	if err != nil && len(results) == 0 {
		output = fmt.Sprintf("Error searching directory: %v", err)
	}

	// When grep finds nothing and embedding is available, hint at semantic search.
	// Guard: only hint on clean zero-result runs, not when an error occurred.
	if matchCount == 0 && err == nil && env.EmbeddingMgr != nil && env.EmbeddingMgr.IsInitialized() {
		output = fmt.Sprintf("No results found for '%s' in %s.\n\nNo text matches, but the embedding index is available — try `semantic_search` to find code with similar meaning.", searchPattern, directory)
	}

	return ToolResult{
		Output:  output,
		IsError: false,
	}, nil
}

func (h *searchFilesHandler) Aliases() []string         { return nil }
func (h *searchFilesHandler) Timeout() time.Duration    { return 0 }
func (h *searchFilesHandler) MaxResultSize() int        { return 0 }
func (h *searchFilesHandler) SafeForParallel() bool     { return false }
func (h *searchFilesHandler) Interactive() bool         { return false }

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

func shouldSkipDir(path string) bool {
	name := filepath.Base(path)
	skipDirs := []string{".git", "node_modules", ".sprout", ".idea", ".vscode", "vendor", "__pycache__"}
	for _, skip := range skipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

func isTextFile(path string) bool {
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
