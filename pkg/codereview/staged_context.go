package codereview

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

const (
	// fileContextWindowLines is the number of unchanged lines shown on each
	// side of a hunk so the reviewer sees the code around the change.
	fileContextWindowLines = 20
	// fileContextMaxLinesPerFile bounds one file's excerpt; hunks beyond it
	// are listed as omitted so the reviewer knows to open the file.
	fileContextMaxLinesPerFile = 400
	fileContextMaxFileBytes    = 2 * 1024 * 1024
	maxKeyComments             = 10
)

// StagedContext is the metadata attached to a staged-change review to help the
// model judge intent: what kind of project, what changed, and the code around
// each hunk.
type StagedContext struct {
	ProjectType      string
	CommitMessage    string
	KeyComments      string
	ChangeCategories string
	FullFileContext  string
}

// Apply copies the context into rc.
func (c StagedContext) Apply(rc *ReviewContext) {
	rc.ProjectType = c.ProjectType
	rc.CommitMessage = c.CommitMessage
	rc.KeyComments = c.KeyComments
	rc.ChangeCategories = c.ChangeCategories
	rc.FullFileContext = c.FullFileContext
}

// BuildStagedContext derives review metadata for stagedDiff. Paths in the diff
// are relative to the git top level, so files are resolved against the
// repository containing dir rather than the process working directory.
func BuildStagedContext(ctx context.Context, dir, stagedDiff string) StagedContext {
	root := gitTopLevel(ctx, dir)
	return StagedContext{
		ProjectType:      DetectProjectType(root),
		CommitMessage:    stagedStatSummary(ctx, root),
		KeyComments:      ExtractKeyComments(stagedDiff),
		ChangeCategories: CategorizeChanges(stagedDiff),
		FullFileContext:  stagedHunkContext(ctx, root, stagedDiff),
	}
}

// stagedHunkContext excerpts the staged (index) version of each file so the
// review never shows unstaged edits that won't be committed.
func stagedHunkContext(ctx context.Context, root, stagedDiff string) string {
	return revHunkContext(ctx, root, "", stagedDiff)
}

// revHunkContext excerpts each file in diff as it exists at rev; an empty rev
// reads the index.
func revHunkContext(ctx context.Context, root, rev, diff string) string {
	excerpts := HunkExcerptsFrom(diff, func(rel string) ([]string, bool) {
		content, err := runGit(ctx, root, "show", rev+":"+rel)
		if err != nil {
			return nil, false
		}
		return SplitContextLines(content)
	})
	parts := make([]string, len(excerpts))
	for i, e := range excerpts {
		parts[i] = e.Body
	}
	return strings.Join(parts, "\n\n")
}

func gitTopLevel(ctx context.Context, dir string) string {
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	if out, err := runGit(ctx, dir, "rev-parse", "--show-toplevel"); err == nil {
		if top := strings.TrimSpace(out); top != "" {
			return top
		}
	}
	return dir
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "git", append([]string{"-C", dir, "--no-pager", "-c", "color.ui=false"}, args...)...) //nolint:gosec // G204: fixed git subcommands against the reviewed repository
	out, err := cmd.Output()
	return string(out), err
}

var projectMarkers = []struct{ file, name string }{
	{"go.mod", "Go project"},
	{"package.json", "Node.js project"},
	{"requirements.txt", "Python project"},
	{"setup.py", "Python project"},
	{"pyproject.toml", "Python project"},
	{"Cargo.toml", "Rust project"},
	{"Gemfile", "Ruby project"},
}

// DetectProjectType names the project type from marker files in root.
func DetectProjectType(root string) string {
	for _, m := range projectMarkers {
		if _, err := os.Stat(filepath.Join(root, m.file)); err == nil {
			return m.name
		}
	}
	return ""
}

// stagedStatSummary returns git's "N files changed, …" totals line; that is
// the last line of --stat output (the first line is a per-file entry).
func stagedStatSummary(ctx context.Context, root string) string {
	out, err := runGit(ctx, root, "diff", "--cached", "--stat")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if summary := strings.TrimSpace(lines[len(lines)-1]); summary != "" {
		return "Staged changes summary: " + summary
	}
	return ""
}

// diffFiles returns the post-image paths of the files in diff, in diff order.
func diffFiles(diff string) []string {
	var files []string
	seen := map[string]bool{}
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "diff --git") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		path := strings.TrimPrefix(parts[3], "b/")
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	return files
}

// ExtractKeyComments lists added comments that explain intent (see
// IsImportantComment), capped so they can't crowd out the diff.
func ExtractKeyComments(diff string) string {
	var keyComments []string
	currentFile := ""
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			if parts := strings.Fields(line); len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
			continue
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") &&
			(strings.Contains(line, "//") || strings.Contains(line, "#")) {
			comment := strings.TrimSpace(strings.TrimPrefix(line, "+"))
			if IsImportantComment(comment) {
				keyComments = append(keyComments, fmt.Sprintf("- %s: %s", currentFile, comment))
				if len(keyComments) == maxKeyComments {
					break
				}
			}
		}
	}
	return strings.Join(keyComments, "\n")
}

// CategorizeChanges gives a rough per-category count of changed lines.
func CategorizeChanges(diff string) string {
	categories := map[string]int{}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added := strings.TrimPrefix(line, "+")
			if strings.Contains(strings.ToUpper(added), "SECURITY") ||
				strings.Contains(added, "filesystem.ErrOutsideWorkingDirectory") ||
				strings.Contains(added, "WithSecurityBypass") {
				categories["Security fixes/improvements"]++
			}
			if strings.Contains(added, "error") || strings.Contains(added, "Err") ||
				strings.Contains(added, "return nil") || strings.Contains(added, "if err") {
				categories["Error handling"]++
			}
			if strings.Contains(added, "require(") || strings.Contains(added, "github.com/") ||
				strings.Contains(added, "go.mod") {
				categories["Dependency updates"]++
			}
			if strings.Contains(added, "Test") || strings.Contains(added, "test") {
				categories["Test changes"]++
			}
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			categories["Code removal/refactoring"]++
		}
	}

	names := make([]string, 0, len(categories))
	for name := range categories {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("- %s (%d changes)", name, categories[name]))
	}
	return strings.Join(out, "\n")
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

type lineRange struct{ start, end int } // 1-based, inclusive

// hunkRanges maps each file in diff to the post-image line ranges its hunks
// touch.
func hunkRanges(diff string) map[string][]lineRange {
	ranges := map[string][]lineRange{}
	current := ""
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			current = ""
			if parts := strings.Fields(line); len(parts) >= 4 {
				current = strings.TrimPrefix(parts[3], "b/")
			}
			continue
		}
		m := hunkHeaderPattern.FindStringSubmatch(line)
		if m == nil || current == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		end := start + count - 1
		if count == 0 {
			end = start
		}
		ranges[current] = append(ranges[current], lineRange{start, end})
	}
	return ranges
}

// FileExcerpt is a formatted, line-numbered excerpt of one file.
type FileExcerpt struct {
	Path string
	Body string // markdown: "### path" heading plus fenced, line-numbered code
}

// ExtractHunkContext excerpts the current content of each changed file around
// its hunks, with line numbers, so a reviewer can judge a change against its
// surroundings. Skipped: generated/vendored/binary files and anything outside
// root.
func ExtractHunkContext(root, diff string) string {
	excerpts := HunkExcerpts(root, diff)
	parts := make([]string, len(excerpts))
	for i, e := range excerpts {
		parts[i] = e.Body
	}
	return strings.Join(parts, "\n\n")
}

// HunkExcerpts is ExtractHunkContext split per file, in diff order, so callers
// can apply their own size budget.
func HunkExcerpts(root, diff string) []FileExcerpt {
	return HunkExcerptsFrom(diff, func(rel string) ([]string, bool) { return readContextFile(root, rel) })
}

// HunkExcerptsFrom is HunkExcerpts with the post-image content supplied by
// read — e.g. from the git index, so a staged review isn't shown unstaged
// edits. Files that should never be excerpted are skipped before read.
func HunkExcerptsFrom(diff string, read func(rel string) ([]string, bool)) []FileExcerpt {
	ranges := hunkRanges(diff)
	var excerpts []FileExcerpt
	for _, rel := range diffFiles(diff) {
		if shouldSkipFileForContext(rel) {
			continue
		}
		lines, ok := read(rel)
		if !ok {
			continue
		}
		if excerpt := excerptAroundHunks(lines, ranges[rel]); excerpt != "" {
			excerpts = append(excerpts, newFileExcerpt(rel, excerpt))
		}
	}
	return excerpts
}

// NewFileExcerpt returns the head of a file that has no diff hunks (e.g. an
// untracked new file), line-numbered and capped like hunk excerpts.
func NewFileExcerpt(root, rel string) (FileExcerpt, bool) {
	lines, ok := readContextFile(root, rel)
	if !ok {
		return FileExcerpt{}, false
	}
	end := min(len(lines), fileContextMaxLinesPerFile)
	var b strings.Builder
	for n := 1; n <= end; n++ {
		fmt.Fprintf(&b, "%5d  %s\n", n, lines[n-1])
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "... (%d more lines; open the file to see them)\n", len(lines)-end)
	}
	return newFileExcerpt(rel, strings.TrimRight(b.String(), "\n")), true
}

func newFileExcerpt(rel, body string) FileExcerpt {
	return FileExcerpt{Path: rel, Body: fmt.Sprintf("### %s\n```%s\n%s\n```", rel, fenceLanguage(rel), body)}
}

func readContextFile(root, rel string) ([]string, bool) {
	if shouldSkipFileForContext(rel) {
		return nil, false
	}
	abs, ok := resolveInRoot(root, rel)
	if !ok {
		return nil, false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() || info.Size() > fileContextMaxFileBytes {
		return nil, false
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	return strings.Split(strings.TrimRight(string(content), "\n"), "\n"), true
}

func excerptAroundHunks(lines []string, hunks []lineRange) string {
	if len(lines) == 0 {
		return ""
	}
	if len(hunks) == 0 {
		// New or renamed file without hunks; show its head.
		hunks = []lineRange{{1, 1}}
	}

	var windows []lineRange
	for _, h := range hunks {
		w := lineRange{max(1, h.start-fileContextWindowLines), min(len(lines), h.end+fileContextWindowLines)}
		if w.start > len(lines) {
			continue
		}
		if n := len(windows); n > 0 && w.start <= windows[n-1].end+1 {
			windows[n-1].end = max(windows[n-1].end, w.end)
			continue
		}
		windows = append(windows, w)
	}

	var b strings.Builder
	shown := 0
	for i, w := range windows {
		if shown >= fileContextMaxLinesPerFile {
			fmt.Fprintf(&b, "... (%d more changed region(s) omitted; open the file to see them)\n", len(windows)-i)
			break
		}
		if i > 0 {
			b.WriteString("...\n")
		}
		end := min(w.end, w.start+fileContextMaxLinesPerFile-shown-1)
		for n := w.start; n <= end; n++ {
			fmt.Fprintf(&b, "%5d  %s\n", n, lines[n-1])
		}
		shown += end - w.start + 1
		if end < w.end {
			fmt.Fprintf(&b, "... (excerpt cut at line %d; the rest of this region and %d more changed region(s) omitted — open the file to see them)\n", end, len(windows)-i-1)
			break
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func resolveInRoot(root, rel string) (string, bool) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, abs)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

var fenceLanguages = map[string]string{
	".go": "go", ".ts": "ts", ".tsx": "tsx", ".js": "js", ".jsx": "jsx", ".mjs": "js",
	".py": "python", ".rs": "rust", ".rb": "ruby", ".java": "java", ".kt": "kotlin",
	".swift": "swift", ".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp",
	".cs": "csharp", ".sh": "bash", ".bash": "bash", ".zsh": "bash", ".sql": "sql",
	".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml", ".md": "markdown",
	".html": "html", ".css": "css", ".scss": "scss", ".proto": "protobuf",
}

func fenceLanguage(path string) string {
	return fenceLanguages[strings.ToLower(filepath.Ext(path))]
}

func shouldSkipFileForContext(filePath string) bool {
	if utils.ClassifyReviewFile(filePath).SkipForReview {
		return true
	}
	for _, suffix := range []string{
		".sum", ".lock", "package-lock.json", "yarn.lock", ".map", ".pb.go",
		"coverage.out", "coverage.html", ".test", ".out",
		".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".pdf", ".zip",
	} {
		if strings.HasSuffix(filePath, suffix) {
			return true
		}
	}
	for _, fragment := range []string{".min.", "_generated.", "node_modules/", "vendor/", ".git/"} {
		if strings.Contains(filePath, fragment) {
			return true
		}
	}
	return false
}

// SplitContextLines splits file content into lines for excerpting, rejecting
// content too large to be worth excerpting.
func SplitContextLines(content string) ([]string, bool) {
	if len(content) > fileContextMaxFileBytes {
		return nil, false
	}
	return strings.Split(strings.TrimRight(content, "\n"), "\n"), true
}
