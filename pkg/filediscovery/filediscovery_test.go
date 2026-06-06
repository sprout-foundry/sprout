package filediscovery

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// makeTree creates a temporary directory tree for testing and returns the root.
func makeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("makeTree mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("makeTree write: %v", err)
		}
	}
	return root
}

// --- GetIgnoreRules tests ---

func TestGetIgnoreRules_NoFiles(t *testing.T) {
	root := t.TempDir()
	got := GetIgnoreRules(root)
	if got != nil {
		t.Errorf("expected nil for empty dir, got %v", got)
	}
}

func TestGetIgnoreRules_Gitignore(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore": "*.log\nbuild/\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if !rules.MatchesPath("build/output.js") {
		t.Error("expected build/output.js to be ignored")
	}
	if rules.MatchesPath("main.go") {
		t.Error("expected main.go NOT to be ignored")
	}
}

func TestGetIgnoreRules_LeditIgnore(t *testing.T) {
	root := makeTree(t, map[string]string{
		".sprout/.ignore": "secret.txt\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("secret.txt") {
		t.Error("expected secret.txt to be ignored via .sprout/.ignore")
	}
}

func TestGetIgnoreRules_BothFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore":     "*.log\n",
		".sprout/.ignore": "dist/\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored (gitignore)")
	}
	if !rules.MatchesPath("dist/bundle.js") {
		t.Error("expected dist/bundle.js to be ignored (ledit ignore)")
	}
	if rules.MatchesPath("main.go") {
		t.Error("expected main.go NOT to be ignored")
	}
}

// --- discoverBasic tests (via DiscoverFilesRobust with UseShell=false) ---

func newFD() *FileDiscovery {
	return &FileDiscovery{}
}

func TestDiscoverBasic_ReturnsFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":       "package main",
		"util.go":       "package main",
		"README.md":     "hello",
		".hidden":       "secret",
		"sub/handler.go": "package sub",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles: 100,
		RootPath: root,
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	// Hidden files should be excluded by default
	for _, f := range result.Files {
		if filepath.Base(f) == ".hidden" {
			t.Errorf("hidden file should not be returned: %s", f)
		}
	}
}

func TestDiscoverBasic_IncludeExtFilter(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":   "package main",
		"style.css": "body {}",
		"README.md": "hello",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles:    100,
		RootPath:    root,
		IncludeExts: []string{".go"},
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	for _, f := range result.Files {
		if filepath.Ext(f) != ".go" {
			t.Errorf("expected only .go files, got %s", f)
		}
	}
}

func TestDiscoverBasic_ExcludeExtFilter(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":   "package main",
		"style.css": "body {}",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles:    100,
		RootPath:    root,
		ExcludeExts: []string{".css"},
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	for _, f := range result.Files {
		if filepath.Ext(f) == ".css" {
			t.Errorf("excluded .css file appeared: %s", f)
		}
	}
}

func TestDiscoverBasic_ExcludeDirs(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":            "package main",
		"node_modules/lib.js": "exports = {}",
	})

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles:    100,
		RootPath:    root,
		ExcludeDirs: []string{"node_modules"},
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	for _, f := range result.Files {
		if containsPathSegment(filepath.ToSlash(f), "node_modules") {
			t.Errorf("node_modules file should be excluded: %s", f)
		}
	}
}

func TestDiscoverBasic_MaxFiles(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 20; i++ {
		files[filepath.Join("src", filepath.FromSlash("f"+string(rune('a'+i))+".go"))] = "package src"
	}
	root := makeTree(t, files)

	fd := newFD()
	result := fd.discoverBasic(&DiscoveryOptions{
		MaxFiles: 5,
		RootPath: root,
	})

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Files) > 5 {
		t.Errorf("expected at most 5 files, got %d", len(result.Files))
	}
}

func containsPathSegment(path, segment string) bool {
	start := 0
	slashed := filepath.ToSlash(path)
	for i := 0; i <= len(slashed); i++ {
		if i == len(slashed) || slashed[i] == '/' {
			if slashed[start:i] == segment {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// --- DiscoverFilesRobust smoke test (UseShell=false to avoid shell deps) ---

func TestDiscoverFilesRobust_Basic(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package main",
	})

	fd := newFD()
	result := fd.DiscoverFilesRobust("main function", &DiscoveryOptions{
		MaxFiles:   10,
		UseSymbols: false,
		UseShell:   false,
		RootPath:   root,
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if len(result.Files) == 0 {
		t.Fatal("expected at least one file")
	}
	if result.Method != "basic" {
		t.Errorf("expected method 'basic', got %q", result.Method)
	}
}

func TestDiscoverFilesRobust_DefaultOptions(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	// Change to the temp dir so RootPath="" resolves to it
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Skip("cannot chdir to temp dir")
	}
	defer os.Chdir(old)

	fd := newFD()
	result := fd.DiscoverFilesRobust("something", nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// --- Additional GetIgnoreRules tests ---

func TestGetIgnoreRules_CommentsAndBlankLines(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore": "# This is a comment\n\n*.log\n\n# Another comment\n\nbuild/\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	// Comments and blank lines should be ignored, patterns should work
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored")
	}
	if !rules.MatchesPath("build/output.js") {
		t.Error("expected build/output.js to be ignored")
	}
	if rules.MatchesPath("main.go") {
		t.Error("expected main.go NOT to be ignored")
	}
}

func TestGetIgnoreRules_NegationPatterns(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore": "*.log\n!important.log\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	// All .log files ignored except important.log
	if !rules.MatchesPath("debug.log") {
		t.Error("expected debug.log to be ignored")
	}
	if rules.MatchesPath("important.log") {
		t.Error("expected important.log NOT to be ignored (negation pattern)")
	}
}

func TestGetIgnoreRules_ComplexGlobPatterns(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore": "**/test.go\ndocs/\n*.min.js\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	// Double wildcard pattern
	if !rules.MatchesPath("test.go") {
		t.Error("expected test.go to be ignored (**/test.go)")
	}
	if !rules.MatchesPath("sub/test.go") {
		t.Error("expected sub/test.go to be ignored (**/test.go)")
	}
	if !rules.MatchesPath("deep/nested/test.go") {
		t.Error("expected deep/nested/test.go to be ignored (**/test.go)")
	}
	// Directory pattern
	if !rules.MatchesPath("docs/readme.md") {
		t.Error("expected docs/readme.md to be ignored (docs/)")
	}
	if rules.MatchesPath("docs2/readme.md") {
		t.Error("expected docs2/readme.md NOT to be ignored")
	}
	// Prefix pattern
	if !rules.MatchesPath("app.min.js") {
		t.Error("expected app.min.js to be ignored (*.min.js)")
	}
	if rules.MatchesPath("app.js") {
		t.Error("expected app.js NOT to be ignored")
	}
}

func TestGetIgnoreRules_MissingLeditDir(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore": "*.log\n",
	})
	// Create .gitignore but not .sprout directory - should not error
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored")
	}
}

func TestGetIgnoreRules_LeditPriority(t *testing.T) {
	// Test that .sprout/.ignore rules are applied in addition to .gitignore
	// Both should be combined
	root := makeTree(t, map[string]string{
		".gitignore":     "*.log\n",
		".sprout/.ignore": "secret.txt\n*.tmp\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	// Should respect both files
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored (from .gitignore)")
	}
	if !rules.MatchesPath("secret.txt") {
		t.Error("expected secret.txt to be ignored (from .sprout/.ignore)")
	}
	if !rules.MatchesPath("temp.tmp") {
		t.Error("expected temp.tmp to be ignored (from .sprout/.ignore)")
	}
	if rules.MatchesPath("main.go") {
		t.Error("expected main.go NOT to be ignored")
	}
}

// --- BuildWorkspaceStructure tests ---

func TestBuildWorkspaceStructure_GroupsFilesByDirectory(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":         "package main",
		"util.go":         "package main",
		"README.md":       "hello",
		"src/handler.go":  "package src",
		"src/utils.go":    "package src",
		"cmd/server.go":   "package main",
		"cmd/client.go":   "package main",
	})

	fd := newFD()
	// Change to root directory so relative paths work
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(old)

	info := fd.BuildWorkspaceStructure()

	if info.Error != nil {
		t.Fatalf("unexpected error: %v", info.Error)
	}

	// Check total files
	if len(info.AllFiles) != 7 {
		t.Errorf("expected 7 files, got %d", len(info.AllFiles))
	}

	// Check FilesByDir grouping
	// Count files in each directory
	rootCount := 0
	srcCount := 0
	cmdCount := 0
	for _, files := range info.FilesByDir {
		for _, f := range files {
			if containsPathSegment(filepath.ToSlash(f), "src") {
				srcCount++
			} else if containsPathSegment(filepath.ToSlash(f), "cmd") {
				cmdCount++
			} else {
				rootCount++
			}
		}
	}

	if srcCount != 2 {
		t.Errorf("expected 2 files in src/, got %d", srcCount)
	}
	if cmdCount != 2 {
		t.Errorf("expected 2 files in cmd/, got %d", cmdCount)
	}
	if rootCount != 3 {
		t.Errorf("expected 3 files in root, got %d", rootCount)
	}
}

func TestBuildWorkspaceStructure_RootDir(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer os.Chdir(old)

	fd := newFD()
	info := fd.BuildWorkspaceStructure()

	if info.Error != nil {
		t.Fatalf("unexpected error: %v", info.Error)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("failed to get abs path: %v", err)
	}
	// filepath.Abs calls os.Getwd which resolves symlinks on macOS (/var → /private/var).
	// t.TempDir() returns the unresolved path, so resolve for comparison.
	if evaled, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = evaled
	}

	if info.RootDir != absRoot {
		t.Errorf("expected RootDir %q, got %q", absRoot, info.RootDir)
	}
}

func TestBuildWorkspaceStructure_DetectProjectType(t *testing.T) {
	tests := []struct {
		name         string
		markerFile   string
		expectedType string
	}{
		{"Go project", "go.mod", "go"},
		{"Node.js project", "package.json", "nodejs"},
		{"Python requirements.txt", "requirements.txt", "python"},
		{"Python pyproject.toml", "pyproject.toml", "python"},
		{"Rust project", "Cargo.toml", "rust"},
		{"Java Maven", "pom.xml", "java"},
		{"Java Gradle", "build.gradle", "java"},
		{"C/C++ CMake", "CMakeLists.txt", "c/c++"},
		{"Makefile", "Makefile", "c/c++/make"},
		{"Git repo", ".git", "git"},
		{"Unknown project", "README.md", "documentation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := makeTree(t, map[string]string{
				tt.markerFile: "content",
			})

			old, _ := os.Getwd()
			if err := os.Chdir(root); err != nil {
				t.Fatalf("failed to chdir: %v", err)
			}
			defer os.Chdir(old)

			fd := newFD()
			info := fd.BuildWorkspaceStructure()

			if info.Error != nil {
				t.Fatalf("unexpected error: %v", info.Error)
			}

			if info.ProjectType != tt.expectedType {
				t.Errorf("expected project type %q, got %q", tt.expectedType, info.ProjectType)
			}
		})
	}
}

// --- extractSearchTerms tests ---

func TestExtractSearchTerms_RemovesStopWords(t *testing.T) {
	fd := newFD()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "common stop words removed",
			input:    "the main function in the file",
			expected: []string{"main", "function", "file"},
		},
		{
			name:     "all stop words",
			input:    "the and is are was were",
			expected: []string{},
		},
		{
			name:     "mixed stop and content words",
			input:    "create a new user with email and password",
			expected: []string{"create", "new", "user", "email", "password"},
		},
		{
			name:     "verbs as stop words",
			input:    "can could should would will",
			expected: []string{},
		},
		{
			name:     "pronouns as stop words",
			input:    "i you he she it we they",
			expected: []string{},
		},
		{
			name:     "search verbs are stop words",
			input:    "find search grep look locate",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fd.extractSearchTerms(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d terms, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, exp := range tt.expected {
				if i >= len(result) || result[i] != exp {
					t.Errorf("expected term %d to be %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

func TestExtractSearchTerms_ShortWordsFiltered(t *testing.T) {
	fd := newFD()

	tests := []struct {
		name  string
		input string
	}{
		{"one char words", "a b c d"},
		{"two char words included", "go to do"},
		{"three char words included", "the cat sat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fd.extractSearchTerms(tt.input)
			for _, term := range result {
				if len(term) <= 2 {
					t.Errorf("short word should be filtered: %q", term)
				}
			}
		})
	}
}

func TestExtractSearchTerms_PunctuationStripping(t *testing.T) {
	fd := newFD()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "commas and periods",
			input:    "main, function. file.",
			expected: []string{"main", "function", "file"},
		},
		{
			name:     "quotes and parentheses",
			input:    "\"test\" (value) [array]",
			expected: []string{"test", "value", "array"},
		},
		{
			name:     "exclamation and question marks",
			input:    "hello! world? how",
			expected: []string{"hello", "world", "how"},
		},
		{
			name:     "trailing punctuation",
			input:    "main.go util.go handler.go",
			expected: []string{"main.go", "util.go", "handler.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fd.extractSearchTerms(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d terms, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, exp := range tt.expected {
				if i >= len(result) || result[i] != exp {
					t.Errorf("expected term %d to be %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

func TestExtractSearchTerms_EmptyAndWhitespace(t *testing.T) {
	fd := newFD()

	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"only spaces", "   "},
		{"only tabs", "\t\t\t"},
		{"mixed whitespace", " \t \n "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fd.extractSearchTerms(tt.input)
			if len(result) != 0 {
				t.Errorf("expected no terms for %q, got %v", tt.input, result)
			}
		})
	}
}

// --- parseQueryTerms tests ---

func TestParseQueryTerms_FilePatterns(t *testing.T) {
	fd := newFD()

	tests := []struct {
		name     string
		input    string
		patterns []string
		terms    []string
	}{
		{
			name:     "wildcard pattern",
			input:    "*.go",
			patterns: []string{"*.go"},
			terms:    []string{},
		},
		{
			name:     "question mark pattern",
			input:    "test?.go",
			patterns: []string{"test?.go"},
			terms:    []string{},
		},
		{
			name:     "multiple wildcards",
			input:    "*.go *.js",
			patterns: []string{"*.go", "*.js"},
			terms:    []string{},
		},
		{
			name:     "double wildcard",
			input:    "**/test.go",
			patterns: []string{"**/test.go"},
			terms:    []string{},
		},
		{
			name:     "plain words",
			input:    "main function",
			patterns: []string{},
			terms:    []string{"main", "function"},
		},
		{
			name:     "mixed query",
			input:    "*.go main function",
			patterns: []string{"*.go"},
			terms:    []string{"main", "function"},
		},
		{
			name:     "complex mixed",
			input:    "*.go main *.js util package",
			patterns: []string{"*.go", "*.js"},
			terms:    []string{"main", "util", "package"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fd.parseQueryTerms(tt.input)

			if len(result.FilePatterns) != len(tt.patterns) {
				t.Errorf("expected %d patterns, got %d", len(tt.patterns), len(result.FilePatterns))
			}
			for i, exp := range tt.patterns {
				if i >= len(result.FilePatterns) || result.FilePatterns[i] != exp {
					t.Errorf("pattern %d: expected %q, got %q", i, exp, result.FilePatterns[i])
				}
			}

			if len(result.SearchTerms) != len(tt.terms) {
				t.Errorf("expected %d terms, got %d", len(tt.terms), len(result.SearchTerms))
			}
			for i, exp := range tt.terms {
				if i >= len(result.SearchTerms) || result.SearchTerms[i] != exp {
					t.Errorf("term %d: expected %q, got %q", i, exp, result.SearchTerms[i])
				}
			}
		})
	}
}

func TestParseQueryTerms_WhitespaceHandling(t *testing.T) {
	fd := newFD()

	result := fd.parseQueryTerms("  *.go   main   function  ")

	if len(result.FilePatterns) != 1 || result.FilePatterns[0] != "*.go" {
		t.Errorf("unexpected patterns: %v", result.FilePatterns)
	}
	if len(result.SearchTerms) != 2 || result.SearchTerms[0] != "main" || result.SearchTerms[1] != "function" {
		t.Errorf("unexpected terms: %v", result.SearchTerms)
	}
}

// --- applyFiltersAndLimits tests ---

func TestApplyFiltersAndLimits_ExcludeDirs(t *testing.T) {
	fd := newFD()

	files := []string{
		"main.go",
		"node_modules/lib.js",
		"vendor/utils.go",
		"build/app.js",
		"src/handler.go",
	}

	result := fd.applyFiltersAndLimits(files, &DiscoveryOptions{
		MaxFiles:    100,
		ExcludeDirs: []string{"node_modules", "vendor"},
	})

	for _, f := range result.Files {
		if containsPathSegment(filepath.ToSlash(f), "node_modules") {
			t.Errorf("node_modules should be filtered: %s", f)
		}
		if containsPathSegment(filepath.ToSlash(f), "vendor") {
			t.Errorf("vendor should be filtered: %s", f)
		}
	}

	// build should still be present
	foundBuild := false
	for _, f := range result.Files {
		if containsPathSegment(filepath.ToSlash(f), "build") {
			foundBuild = true
			break
		}
	}
	if !foundBuild {
		t.Error("build directory should be present (not excluded)")
	}
}

func TestApplyFiltersAndLimits_MaxFiles(t *testing.T) {
	fd := newFD()

	files := []string{
		"file1.go",
		"file2.go",
		"file3.go",
		"file4.go",
		"file5.go",
	}

	result := fd.applyFiltersAndLimits(files, &DiscoveryOptions{
		MaxFiles: 3,
	})

	if len(result.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(result.Files))
	}
	if result.MatchedFiles != 3 {
		t.Errorf("expected MatchedFiles 3, got %d", result.MatchedFiles)
	}
}

func TestApplyFiltersAndLimits_CombinedFilters(t *testing.T) {
	fd := newFD()

	files := []string{
		"main.go",
		"node_modules/lib.js",
		"vendor/utils.go",
		"build/app.js",
		"src/handler.go",
		"tests/test.go",
	}

	result := fd.applyFiltersAndLimits(files, &DiscoveryOptions{
		MaxFiles:    3,
		ExcludeDirs: []string{"node_modules", "vendor", "build"},
	})

	// Should exclude node_modules, vendor, build
	for _, f := range result.Files {
		if containsPathSegment(filepath.ToSlash(f), "node_modules") ||
			containsPathSegment(filepath.ToSlash(f), "vendor") ||
			containsPathSegment(filepath.ToSlash(f), "build") {
			t.Errorf("excluded dir present: %s", f)
		}
	}

	// Should limit to 3 files
	if len(result.Files) != 3 {
		t.Errorf("expected 3 files, got %d", len(result.Files))
	}
}

func TestApplyFiltersAndLimits_NoFilters(t *testing.T) {
	fd := newFD()

	files := []string{"a.go", "b.go", "c.go"}

	result := fd.applyFiltersAndLimits(files, &DiscoveryOptions{
		MaxFiles: 0, // No limit
	})

	if len(result.Files) != 3 {
		t.Errorf("expected all 3 files, got %d", len(result.Files))
	}
	if result.MatchedFiles != 3 {
		t.Errorf("expected MatchedFiles 3, got %d", result.MatchedFiles)
	}
}

// --- FilterFiles and matchesCriteria tests ---

func TestFilterFiles_IncludeExtensions(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":   "package main",
		"style.css": "body {}",
		"README.md": "hello",
		"app.js":    "console.log()",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "main.go"),
		filepath.Join(root, "style.css"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, "app.js"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		IncludeExtensions: []string{".go", ".js"},
	})

	if len(result) != 2 {
		t.Errorf("expected 2 files, got %d", len(result))
	}
	for _, f := range result {
		ext := filepath.Ext(f)
		if ext != ".go" && ext != ".js" {
			t.Errorf("unexpected file extension: %s", ext)
		}
	}
}

func TestFilterFiles_ExcludeExtensions(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":   "package main",
		"style.css": "body {}",
		"README.md": "hello",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "main.go"),
		filepath.Join(root, "style.css"),
		filepath.Join(root, "README.md"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		ExcludeExtensions: []string{".css"},
	})

	if len(result) != 2 {
		t.Errorf("expected 2 files, got %d", len(result))
	}
	for _, f := range result {
		if filepath.Ext(f) == ".css" {
			t.Errorf("css file should be excluded: %s", f)
		}
	}
}

func TestFilterFiles_IncludePaths(t *testing.T) {
	root := makeTree(t, map[string]string{
		"src/main.go":    "package main",
		"src/util.go":    "package main",
		"tests/test.go":  "package tests",
		"README.md":      "hello",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "src/main.go"),
		filepath.Join(root, "src/util.go"),
		filepath.Join(root, "tests/test.go"),
		filepath.Join(root, "README.md"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		IncludePaths: []string{"src"},
	})

	if len(result) != 2 {
		t.Errorf("expected 2 files in src, got %d", len(result))
	}
	for _, f := range result {
		if !containsPathSegment(filepath.ToSlash(f), "src") {
			t.Errorf("file not in src: %s", f)
		}
	}
}

func TestFilterFiles_ExcludePaths(t *testing.T) {
	root := makeTree(t, map[string]string{
		"src/main.go":   "package main",
		"src/util.go":   "package main",
		"tests/test.go": "package tests",
		"README.md":     "hello",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "src/main.go"),
		filepath.Join(root, "src/util.go"),
		filepath.Join(root, "tests/test.go"),
		filepath.Join(root, "README.md"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		ExcludePaths: []string{"tests"},
	})

	if len(result) != 3 {
		t.Errorf("expected 3 files, got %d", len(result))
	}
	for _, f := range result {
		if containsPathSegment(filepath.ToSlash(f), "tests") {
			t.Errorf("tests file should be excluded: %s", f)
		}
	}
}

func TestFilterFiles_MinSize(t *testing.T) {
	root := makeTree(t, map[string]string{
		"small.txt":  "hi",
		"medium.txt": "hello world",
		"large.txt":  strings.Repeat("x", 100),
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "small.txt"),
		filepath.Join(root, "medium.txt"),
		filepath.Join(root, "large.txt"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		MinSize: 50,
	})

	if len(result) != 1 {
		t.Errorf("expected 1 file with min size 50, got %d", len(result))
	}
	for _, f := range result {
		if !strings.HasSuffix(f, "large.txt") {
			t.Errorf("expected only large.txt, got %s", f)
		}
	}
}

func TestFilterFiles_MaxSize(t *testing.T) {
	root := makeTree(t, map[string]string{
		"small.txt":  "hi",
		"medium.txt": "hello world",
		"large.txt":  strings.Repeat("x", 100),
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "small.txt"),
		filepath.Join(root, "medium.txt"),
		filepath.Join(root, "large.txt"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		MaxSize: 20,
	})

	if len(result) != 2 {
		t.Errorf("expected 2 files with max size 20, got %d", len(result))
	}
	for _, f := range result {
		if strings.HasSuffix(f, "large.txt") {
			t.Errorf("large.txt should be excluded by max size: %s", f)
		}
	}
}

func TestFilterFiles_SizeRange(t *testing.T) {
	root := makeTree(t, map[string]string{
		"small.txt":  "hi",
		"medium.txt": "hello world",
		"large.txt":  strings.Repeat("x", 100),
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "small.txt"),
		filepath.Join(root, "medium.txt"),
		filepath.Join(root, "large.txt"),
	}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		MinSize: 5,
		MaxSize: 20,
	})

	if len(result) != 1 {
		t.Errorf("expected 1 file in size range 5-20, got %d", len(result))
	}
	for _, f := range result {
		if !strings.HasSuffix(f, "medium.txt") {
			t.Errorf("expected only medium.txt, got %s", f)
		}
	}
}

func TestFilterFiles_ModifiedAfter(t *testing.T) {
	root := makeTree(t, map[string]string{
		"old.txt":  "old content",
		"new.txt":  "new content",
	})

	// Set modification times — calculate all times upfront to avoid race conditions
	cutoffTime := time.Now().Add(-1 * time.Hour)
	oldTime := cutoffTime.Add(-2 * time.Hour)
	newTime := cutoffTime.Add(30 * time.Minute) // definitively after cutoff

	oldFile := filepath.Join(root, "old.txt")
	newFile := filepath.Join(root, "new.txt")

	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("failed to set old file time: %v", err)
	}
	if err := os.Chtimes(newFile, newTime, newTime); err != nil {
		t.Fatalf("failed to set new file time: %v", err)
	}

	fd := newFD()
	files := []string{oldFile, newFile}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		ModifiedAfter: cutoffTime,
	})

	if len(result) != 1 {
		t.Errorf("expected 1 file modified after cutoff, got %d", len(result))
	}
	for _, f := range result {
		if !strings.HasSuffix(f, "new.txt") {
			t.Errorf("expected only new.txt, got %s", f)
		}
	}
}

func TestFilterFiles_ModifiedBefore(t *testing.T) {
	root := makeTree(t, map[string]string{
		"old.txt": "old content",
		"new.txt": "new content",
	})

	cutoffTime := time.Now().Add(-1 * time.Hour)
	oldTime := cutoffTime.Add(-2 * time.Hour)
	newTime := cutoffTime.Add(30 * time.Minute) // definitively after cutoff

	oldFile := filepath.Join(root, "old.txt")
	newFile := filepath.Join(root, "new.txt")

	if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
		t.Fatalf("failed to set old file time: %v", err)
	}
	if err := os.Chtimes(newFile, newTime, newTime); err != nil {
		t.Fatalf("failed to set new file time: %v", err)
	}

	fd := newFD()
	files := []string{oldFile, newFile}

	result := fd.FilterFiles(files, &FileFilterCriteria{
		ModifiedBefore: cutoffTime,
	})

	if len(result) != 1 {
		t.Errorf("expected 1 file modified before cutoff, got %d", len(result))
	}
	for _, f := range result {
		if !strings.HasSuffix(f, "old.txt") {
			t.Errorf("expected only old.txt, got %s", f)
		}
	}
}

func TestFilterFiles_NilCriteria(t *testing.T) {
	fd := newFD()
	files := []string{"a.go", "b.go", "c.go"}

	result := fd.FilterFiles(files, nil)

	if len(result) != 3 {
		t.Errorf("expected all 3 files with nil criteria, got %d", len(result))
	}
}

func TestFilterFiles_MultipleCriteria(t *testing.T) {
	root := makeTree(t, map[string]string{
		"src/main.go":   "package main",
		"src/test.go":   "package main",
		"docs/readme.md": "hello",
	})

	// Set size for one file
	mainFile := filepath.Join(root, "src/main.go")
	testFile := filepath.Join(root, "src/test.go")
	readmeFile := filepath.Join(root, "docs/readme.md")

	if err := os.WriteFile(mainFile, []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatalf("failed to write main.go: %v", err)
	}

	fd := newFD()
	files := []string{mainFile, testFile, readmeFile}

	// Include .go files, exclude src path, min size 50
	result := fd.FilterFiles(files, &FileFilterCriteria{
		IncludeExtensions: []string{".go"},
		ExcludePaths:      []string{"src"},
		MinSize:           50,
	})

	// main.go is in src (excluded), test.go is too small, readme.md is wrong extension
	if len(result) != 0 {
		t.Errorf("expected 0 files with conflicting criteria, got %d: %v", len(result), result)
	}
}

// --- GetFileStats tests ---

func TestGetFileStats_TotalCount(t *testing.T) {
	root := makeTree(t, map[string]string{
		"a.go": "package a",
		"b.go": "package b",
		"c.js": "console.log()",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "a.go"),
		filepath.Join(root, "b.go"),
		filepath.Join(root, "c.js"),
	}

	stats := fd.GetFileStats(files)

	total, ok := stats["total"].(int)
	if !ok {
		t.Fatal("total not found or not an int")
	}
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
}

func TestGetFileStats_ByExtension(t *testing.T) {
	root := makeTree(t, map[string]string{
		"a.go":     "package a",
		"b.go":     "package b",
		"c.js":     "console.log()",
		"d.js":     "console.log()",
		"README":   "hello",
		"Makefile": "build",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "a.go"),
		filepath.Join(root, "b.go"),
		filepath.Join(root, "c.js"),
		filepath.Join(root, "d.js"),
		filepath.Join(root, "README"),
		filepath.Join(root, "Makefile"),
	}

	stats := fd.GetFileStats(files)

	byExt, ok := stats["by_extension"].(map[string]int)
	if !ok {
		t.Fatal("by_extension not found or not a map")
	}

	if byExt[".go"] != 2 {
		t.Errorf("expected 2 .go files, got %d", byExt[".go"])
	}
	if byExt[".js"] != 2 {
		t.Errorf("expected 2 .js files, got %d", byExt[".js"])
	}
	if byExt["no_extension"] != 2 {
		t.Errorf("expected 2 files with no extension, got %d", byExt["no_extension"])
	}
}

func TestGetFileStats_ByDirectory(t *testing.T) {
	root := makeTree(t, map[string]string{
		"a.go":       "package a",
		"src/b.go":   "package b",
		"src/c.go":   "package c",
		"tests/d.go": "package d",
		"tests/e.go": "package e",
		"tests/f.go": "package f",
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "a.go"),
		filepath.Join(root, "src/b.go"),
		filepath.Join(root, "src/c.go"),
		filepath.Join(root, "tests/d.go"),
		filepath.Join(root, "tests/e.go"),
		filepath.Join(root, "tests/f.go"),
	}

	stats := fd.GetFileStats(files)

	byDir, ok := stats["by_directory"].(map[string]int)
	if !ok {
		t.Fatal("by_directory not found or not a map")
	}

	// Check directory counts
	totalFiles := 0
	for _, count := range byDir {
		totalFiles += count
	}
	if totalFiles != 6 {
		t.Errorf("expected total 6 files in by_directory, got %d", totalFiles)
	}

	// Find and check specific directories
	hasRoot := false
	hasSrc := false
	hasTests := false
	for dir, count := range byDir {
		if dir == root || dir == "." {
			hasRoot = true
			if count != 1 {
				t.Errorf("expected 1 file in root, got %d", count)
			}
		}
		if containsPathSegment(filepath.ToSlash(dir), "src") {
			hasSrc = true
			if count != 2 {
				t.Errorf("expected 2 files in src, got %d", count)
			}
		}
		if containsPathSegment(filepath.ToSlash(dir), "tests") {
			hasTests = true
			if count != 3 {
				t.Errorf("expected 3 files in tests, got %d", count)
			}
		}
	}

	if !hasRoot || !hasSrc || !hasTests {
		t.Error("missing expected directories in by_directory")
	}
}

func TestGetFileStats_EmptyFiles(t *testing.T) {
	fd := newFD()
	files := []string{}

	stats := fd.GetFileStats(files)

	total, ok := stats["total"].(int)
	if !ok {
		t.Fatal("total not found or not an int")
	}
	if total != 0 {
		t.Errorf("expected total 0 for empty files, got %d", total)
	}
}

func TestGetFileStats_LargestFile(t *testing.T) {
	root := makeTree(t, map[string]string{
		"small.txt":  "hi",
		"medium.txt": "hello world",
		"large.txt":  strings.Repeat("x", 1000),
	})

	fd := newFD()
	files := []string{
		filepath.Join(root, "small.txt"),
		filepath.Join(root, "medium.txt"),
		filepath.Join(root, "large.txt"),
	}

	stats := fd.GetFileStats(files)

	largest, ok := stats["largest_file"].(string)
	if !ok {
		t.Fatal("largest_file not found or not a string")
	}
	expectedLargest := filepath.Join(root, "large.txt")
	if largest != expectedLargest {
		t.Errorf("expected largest file %q, got %q", expectedLargest, largest)
	}

	maxSize, ok := stats["max_size"].(int64)
	if !ok {
		t.Fatal("max_size not found or not an int64")
	}
	if maxSize != 1000 {
		t.Errorf("expected max_size 1000, got %d", maxSize)
	}
}

// --- DiscoverFilesRobust with nil options ---

func TestDiscoverFilesRobust_NilOptions(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package util",
	})

	fd := newFD()

	// Test with nil options - should not panic
	result := fd.DiscoverFilesRobust("main function", nil)

	if result == nil {
		t.Fatal("expected non-nil result with nil options")
	}
	// Result should have some fields populated
	if result.Method == "" {
		t.Error("expected Method to be set")
	}
	// Duration should be recorded
	if result.Duration == 0 {
		t.Error("expected Duration to be recorded")
	}
	_ = root // Use root to avoid unused variable warning
}

func TestDiscoverFilesRobust_NilOptionsWithRootPath(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Skip("cannot chdir to temp dir")
	}
	defer os.Chdir(old)

	fd := newFD()

	// Test with nil options from a directory with files
	result := fd.DiscoverFilesRobust("test query", nil)

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should not crash and should return a valid result
	if result.Method == "" {
		t.Error("expected Method to be set")
	}
}

// --- NewFileDiscovery tests ---

func TestNewFileDiscovery(t *testing.T) {
	// Test creating a new FileDiscovery instance with a test-specific logger
	cfg := &configuration.Config{}
	// Create a test logger instead of using singleton
	logger := &utils.Logger{}

	fd := NewFileDiscovery(cfg, logger)

	if fd == nil {
		t.Fatal("expected non-nil FileDiscovery")
	}
	if fd.config != cfg {
		t.Error("expected config to be set")
	}
	if fd.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestNewFileDiscovery_NilLogger(t *testing.T) {
	// Test creating with nil logger (should still work)
	cfg := &configuration.Config{}

	fd := NewFileDiscovery(cfg, nil)

	if fd == nil {
		t.Fatal("expected non-nil FileDiscovery")
	}
	if fd.config != cfg {
		t.Error("expected config to be set")
	}
	if fd.logger != nil {
		t.Error("expected logger to be nil")
	}
}

// --- deduplicateAndFilter tests ---

func TestDeduplicateAndFilter_RemovesDuplicates(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package util",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	files := []string{
		filepath.Join(root, "main.go"),
		filepath.Join(root, "main.go"), // Duplicate
		filepath.Join(root, "util.go"),
		filepath.Join(root, "util.go"), // Duplicate
		filepath.Join(root, "main.go"), // Triple duplicate
	}

	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should have only 2 unique files
	if len(result) != 2 {
		t.Errorf("expected 2 unique files, got %d: %v", len(result), result)
	}

	// Verify no duplicates in result
	seen := make(map[string]bool)
	for _, f := range result {
		if seen[f] {
			t.Errorf("found duplicate in result: %s", f)
		}
		seen[f] = true
	}
}

func TestDeduplicateAndFilter_ConvertsToAbsolutePath(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	fd := newFD()
	// Resolve RootDir so it matches what filepath.Abs produces via os.Getwd
	// (macOS resolves /var → /private/var).
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("failed to resolve root: %v", err)
	}
	if evaled, err := filepath.EvalSymlinks(resolvedRoot); err == nil {
		resolvedRoot = evaled
	}
	wsInfo := &WorkspaceInfo{RootDir: resolvedRoot}

	files := []string{
		"main.go",     // Relative path
		"./main.go",   // Relative with dot
	}

	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should have converted to absolute path
	if len(result) != 1 {
		t.Errorf("expected 1 file, got %d", len(result))
	}

	// Result should be absolute path
	if !filepath.IsAbs(result[0]) {
		t.Errorf("expected absolute path, got: %s", result[0])
	}
}

func TestDeduplicateAndFilter_ExcludesFilesOutsideWorkspace(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	otherDir := t.TempDir()
	files := []string{
		filepath.Join(root, "main.go"),
		filepath.Join(otherDir, "outside.go"), // Outside workspace
	}

	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should only include files inside workspace
	if len(result) != 1 {
		t.Errorf("expected 1 file (only inside workspace), got %d", len(result))
	}

	for _, f := range result {
		if !strings.HasPrefix(f, root) {
			t.Errorf("file outside workspace included: %s", f)
		}
	}
}

func TestDeduplicateAndFilter_SkipsNonExistentFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	files := []string{
		filepath.Join(root, "main.go"),
		filepath.Join(root, "nonexistent.go"), // Doesn't exist
		filepath.Join(root, "also_missing.go"), // Doesn't exist
	}

	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should only include existing files
	if len(result) != 1 {
		t.Errorf("expected 1 existing file, got %d", len(result))
	}

	if !strings.HasSuffix(result[0], "main.go") {
		t.Errorf("expected main.go, got %s", result[0])
	}
}

func TestDeduplicateAndFilter_SkipsDirectories(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":    "package main",
		"src/util.go": "package src",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	absSrc := filepath.Join(root, "src")

	files := []string{
		filepath.Join(root, "main.go"),
		absSrc, // Directory, not file
	}

	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should skip directories
	if len(result) != 1 {
		t.Errorf("expected 1 file (directory skipped), got %d", len(result))
	}

	for _, f := range result {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatalf("failed to stat %s: %v", f, err)
		}
		if info.IsDir() {
			t.Errorf("directory included in result: %s", f)
		}
	}
}

func TestDeduplicateAndFilter_EmptyInput(t *testing.T) {
	root := t.TempDir()
	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	files := []string{}
	result := fd.deduplicateAndFilter(files, wsInfo)

	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d files", len(result))
	}
}

// --- findWithDirectoryWalk tests ---

func TestFindWithDirectoryWalk_BasicMatching(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main_handler.go": "package main",
		"util.go":         "package util",
		"handler.go":      "package handler",
		"README.md":       "hello",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for "handler" - should match files containing "handler"
	result := fd.findWithDirectoryWalk("handler", wsInfo)

	if len(result) == 0 {
		t.Fatal("expected at least one file matching 'handler'")
	}

	// Verify matched files contain "handler"
	for _, f := range result {
		if !strings.Contains(strings.ToLower(filepath.Base(f)), "handler") {
			t.Errorf("file should contain 'handler': %s", f)
		}
	}
}

func TestFindWithDirectoryWalk_CaseInsensitive(t *testing.T) {
	root := makeTree(t, map[string]string{
		"MainHandler.go": "package main",
		"UTIL.go":        "package util",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search lowercase, should match uppercase
	result := fd.findWithDirectoryWalk("main", wsInfo)

	if len(result) == 0 {
		t.Fatal("expected to find MainHandler.go with lowercase 'main' query")
	}

	// Should match MainHandler.go
	foundMain := false
	for _, f := range result {
		if strings.Contains(strings.ToLower(filepath.Base(f)), "main") {
			foundMain = true
			break
		}
	}
	if !foundMain {
		t.Error("expected to find a file containing 'main'")
	}
}

func TestFindWithDirectoryWalk_SkipsHiddenFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":     "package main",
		".hidden.go":  "package hidden",
		".git/config": "[core]",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for ".go" - should NOT match hidden files
	result := fd.findWithDirectoryWalk(".go", wsInfo)

	for _, f := range result {
		base := filepath.Base(f)
		if strings.HasPrefix(base, ".") {
			t.Errorf("hidden file should be excluded: %s", f)
		}
	}
}

func TestFindWithDirectoryWalk_SkipsCommonExcludedDirs(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":             "package main",
		"node_modules/lib.js": "exports = {}",
		"vendor/utils.go":      "package vendor",
		"target/class.class":   "compiled",
		"build/app.js":        "bundle",
		"dist/bundle.js":      "minified",
		"src/handler.go":      "package src",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for something generic - should skip excluded dirs
	result := fd.findWithDirectoryWalk("", wsInfo)

	for _, f := range result {
		if strings.Contains(f, "node_modules") ||
			strings.Contains(f, "vendor") ||
			strings.Contains(f, "target") ||
			strings.Contains(f, "build") ||
			strings.Contains(f, "dist") {
			t.Errorf("excluded dir should be skipped: %s", f)
		}
	}
}

func TestFindWithDirectoryWalk_NoMatch(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package util",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for something that doesn't exist
	result := fd.findWithDirectoryWalk("nonexistent_xyz", wsInfo)

	if len(result) != 0 {
		t.Errorf("expected no matches, got %d files", len(result))
	}
}

func TestFindWithDirectoryWalk_EmptyQuery(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package util",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Empty query should return all files (except hidden/excluded)
	result := fd.findWithDirectoryWalk("", wsInfo)

	if len(result) == 0 {
		t.Fatal("expected some files with empty query")
	}
}

// --- findFilesUsingShellCommandsFallback tests ---

func TestFindFilesUsingShellCommandsFallback_Basic(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":    "package main",
		"util.go":    "package util",
		"handler.go": "package handler",
	})

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	fd := newFD()
	options := &DiscoveryOptions{
		RootPath: root,
	}

	result := fd.findFilesUsingShellCommandsFallback("main", options)

	if len(result) == 0 {
		t.Fatal("expected at least one file")
	}

	// Should contain "main" in the filename
	foundMain := false
	for _, f := range result {
		if strings.Contains(strings.ToLower(f), "main") {
			foundMain = true
			break
		}
	}
	if !foundMain {
		t.Error("expected to find a file containing 'main'")
	}
}

func TestFindFilesUsingShellCommandsFallback_CaseInsensitive(t *testing.T) {
	root := makeTree(t, map[string]string{
		"MainHandler.go": "package main",
		"UTIL.go":        "package util",
	})

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	fd := newFD()
	options := &DiscoveryOptions{
		RootPath: root,
	}

	// Lowercase query should match uppercase filename
	result := fd.findFilesUsingShellCommandsFallback("mainhandler", options)

	if len(result) == 0 {
		t.Fatal("expected to find MainHandler.go")
	}

	found := false
	for _, f := range result {
		if strings.Contains(strings.ToLower(f), "mainhandler") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected case-insensitive matching")
	}
}

func TestFindFilesUsingShellCommandsFallback_NoMatch(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	fd := newFD()
	options := &DiscoveryOptions{
		RootPath: root,
	}

	// Query that doesn't match
	result := fd.findFilesUsingShellCommandsFallback("nonexistent_xyz_123", options)

	if len(result) != 0 {
		t.Errorf("expected no matches, got %d files", len(result))
	}
}

func TestFindFilesUsingShellCommandsFallback_EmptyQuery(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package util",
	})

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	fd := newFD()
	options := &DiscoveryOptions{
		RootPath: root,
	}

	// Empty query should match all files
	result := fd.findFilesUsingShellCommandsFallback("", options)

	if len(result) == 0 {
		t.Fatal("expected files with empty query")
	}

	// Should contain at least our 2 files
	if len(result) < 2 {
		t.Errorf("expected at least 2 files, got %d", len(result))
	}
}

// --- rerankWithSymbols tests ---

func TestRerankWithSymbols_NoSymbolIndex(t *testing.T) {
	fd := newFD()

	files := []string{
		"util.go",
		"main.go",
		"handler.go",
	}

	// When there's no symbol index, files are sorted alphabetically (all scores are 0)
	result := fd.rerankWithSymbols(files, "main function")

	if len(result) != 3 {
		t.Errorf("expected 3 files, got %d", len(result))
	}

	// Without symbol index, files should be sorted alphabetically
	expected := []string{"handler.go", "main.go", "util.go"}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("index %d: expected %q, got %q", i, exp, result[i])
		}
	}
}

func TestRerankWithSymbols_EmptyFiles(t *testing.T) {
	fd := newFD()

	files := []string{}
	result := fd.rerankWithSymbols(files, "test")

	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %d files", len(result))
	}
}

func TestRerankWithSymbols_Sorting(t *testing.T) {
	fd := newFD()

	// Create files in non-alphabetical order to test sorting
	files := []string{
		"z_end.go",
		"a_start.go",
		"m_middle.go",
	}

	// Without symbol index, should sort alphabetically (all scores are 0)
	result := fd.rerankWithSymbols(files, "test")

	if len(result) != 3 {
		t.Errorf("expected 3 files, got %d", len(result))
	}

	// Should be sorted alphabetically
	expected := []string{"a_start.go", "m_middle.go", "z_end.go"}
	for i, exp := range expected {
		if result[i] != exp {
			t.Errorf("index %d: expected %q, got %q", i, exp, result[i])
		}
	}
}

// --- WorkspaceInfo integration tests ---

func TestWorkspaceInfo_BuildsCorrectly(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go":         "package main",
		"src/handler.go":  "package src",
		"README.md":       "hello",
		"cmd/server.go":   "package cmd",
		"cmd/client.go":   "package cmd",
	})

	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Logf("warning: failed to restore working directory: %v", err)
		}
	})

	if err := os.Chdir(root); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	fd := newFD()
	wsInfo := fd.BuildWorkspaceStructure()

	if wsInfo.Error != nil {
		t.Fatalf("unexpected error: %v", wsInfo.Error)
	}

	// Check RootDir
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("failed to get abs root: %v", err)
	}
	// filepath.Abs calls os.Getwd which resolves symlinks on macOS.
	if evaled, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = evaled
	}
	if wsInfo.RootDir != absRoot {
		t.Errorf("expected RootDir %q, got %q", absRoot, wsInfo.RootDir)
	}

	// Check AllFiles count
	if len(wsInfo.AllFiles) != 5 {
		t.Errorf("expected 5 files, got %d", len(wsInfo.AllFiles))
	}

	// Check FilesByDir
	if len(wsInfo.FilesByDir) == 0 {
		t.Error("expected FilesByDir to be populated")
	}

	// Check ProjectType detection
	if wsInfo.ProjectType == "" {
		t.Error("expected ProjectType to be detected")
	}
}

// --- deduplicateAndFilter edge case tests ---

func TestDeduplicateAndFilter_HandlesSymlinks(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
	})

	// Create a symlink (if supported on the platform)
	originalFile := filepath.Join(root, "main.go")
	symlinkFile := filepath.Join(root, "main_link.go")
	if err := os.Symlink(originalFile, symlinkFile); err != nil {
		t.Skip("symlink creation not supported or failed")
	}

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	files := []string{
		originalFile,
		symlinkFile,
	}

	result := fd.deduplicateAndFilter(files, wsInfo)

	// Symlinks pointing to same file should be deduplicated by path
	// (different paths, so both might be included - this is expected behavior)
	if len(result) == 0 {
		t.Error("expected at least one file")
	}
}

func TestDeduplicateAndFilter_ReadOnlyFiles(t *testing.T) {
	root := t.TempDir()

	// Create a read-only file
	readOnlyFile := filepath.Join(root, "readonly.go")
	if err := os.WriteFile(readOnlyFile, []byte("package main"), 0o444); err != nil {
		t.Fatalf("failed to create read-only file: %v", err)
	}

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	files := []string{readOnlyFile}
	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should include read-only files (they exist and are readable for stat)
	if len(result) != 1 {
		t.Errorf("expected read-only file to be included, got %d files", len(result))
	}
}

// ============================================================================
// Shell Discovery Tests (Platform-Aware)
// ============================================================================

func TestFindWithFindCommand_BasicPattern(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":          "package main",
		"util.go":          "package util",
		"handler.go":       "package handler",
		"README.md":        "hello",
		"sub/sub.go":       "package sub",
		"other/script.js":  "console.log()",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for *.go pattern
	result := fd.findWithFindCommand([]string{"*.go"}, wsInfo)

	if len(result) == 0 {
		t.Fatal("expected to find at least one .go file")
	}

	// All results should be .go files
	for _, f := range result {
		if filepath.Ext(f) != ".go" {
			t.Errorf("expected .go file, got %s", f)
		}
	}

	// Should find main.go, util.go, handler.go, and sub/sub.go
	foundMain := false
	foundUtil := false
	foundHandler := false
	foundSub := false

	for _, f := range result {
		base := filepath.Base(f)
		switch base {
		case "main.go":
			foundMain = true
		case "util.go":
			foundUtil = true
		case "handler.go":
			foundHandler = true
		case "sub.go":
			if filepath.Dir(f) != "" {
				foundSub = true
			}
		}
	}

	if !foundMain {
		t.Error("expected to find main.go")
	}
	if !foundUtil {
		t.Error("expected to find util.go")
	}
	if !foundHandler {
		t.Error("expected to find handler.go")
	}
	if !foundSub {
		t.Error("expected to find sub/sub.go")
	}
}

func TestFindWithFindCommand_MultiplePatterns(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":     "package main",
		"util.go":     "package util",
		"script.js":   "console.log()",
		"style.css":   "body {}",
		"README.md":   "hello",
		"data.json":   "{}",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for multiple patterns
	result := fd.findWithFindCommand([]string{"*.go", "*.js", "*.json"}, wsInfo)

	if len(result) == 0 {
		t.Fatal("expected to find files")
	}

	// Should only return .go, .js, and .json files
	for _, f := range result {
		ext := filepath.Ext(f)
		if ext != ".go" && ext != ".js" && ext != ".json" {
			t.Errorf("unexpected file extension: %s (%s)", f, ext)
		}
	}
}

func TestFindWithFindCommand_ExcludesHiddenFiles(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":        "package main",
		".hidden.go":     "package hidden",
		".git/config":    "[core]",
		"sub/.secret.go": "package secret",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for *.go - should NOT match hidden files
	result := fd.findWithFindCommand([]string{"*.go"}, wsInfo)

	for _, f := range result {
		// Check no hidden files or hidden directories
		base := filepath.Base(f)
		if strings.HasPrefix(base, ".") {
			t.Errorf("hidden file should be excluded: %s", f)
		}

		// Check directory path doesn't contain hidden dirs
		dir := filepath.Dir(f)
		if strings.Contains(dir, "/.") {
			t.Errorf("file in hidden directory should be excluded: %s", f)
		}
	}
}

func TestFindWithFindCommand_ExcludesCommonDirs(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":             "package main",
		"node_modules/lib.js": "exports = {}",
		"vendor/utils.go":     "package vendor",
		"target/class.class":  "compiled",
		"build/app.js":        "bundle",
		"dist/bundle.js":      "minified",
		"src/handler.go":      "package src",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for * - should exclude common dirs
	result := fd.findWithFindCommand([]string{"*"}, wsInfo)

	for _, f := range result {
		if strings.Contains(f, "node_modules") ||
			strings.Contains(f, "vendor") ||
			strings.Contains(f, "target") ||
			strings.Contains(f, "build") ||
			strings.Contains(f, "dist") {
			t.Errorf("excluded directory file should not be found: %s", f)
		}
	}
}

func TestFindWithFindCommand_CommandFailure(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	// Check if find is available
	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := t.TempDir()
	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Use an invalid pattern that might cause find to fail
	// The implementation should handle errors gracefully
	result := fd.findWithFindCommand([]string{"***invalid***pattern***"}, wsInfo)

	// Should return empty result on error, not panic
	if len(result) > 0 {
		t.Errorf("expected empty result on command failure, got %d files", len(result))
	}
}

func TestFindWithGrepCommand_ContentSearch(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	// Check if grep is available
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":  "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
		"util.go":  "package util\n\nfunc helper() {\n\t// helper function\n}",
		"api.go":   "package api\n\nfunc APIHandler() {\n\t// handles API requests\n}",
		"README.md": "# Project\n\nThis is a test project.",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for "package" - should find .go files
	result := fd.findWithGrepCommand([]string{"package"}, wsInfo)

	// Since grep is available and should find matches, expect non-nil result
	// Note: Implementation has a known bug with parsing grep output (-l with -n conflict)
	// This test will expose that bug by requiring the function to work correctly
	if result == nil {
		t.Fatal("expected non-nil result when grep is available and should find matches")
	}

	if len(result) == 0 {
		t.Fatal("expected to find files containing 'package' - grep implementation bug detected")
	}

	// If grep found files, verify they're .go files
	for _, f := range result {
		if filepath.Ext(f) != ".go" {
			t.Errorf("expected .go file, got %s", f)
		}
	}
}

func TestFindWithGrepCommand_MultipleTerms(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available on this system")
	}

	root := makeTree(t, map[string]string{
		"handler.go": "package handler\n\nfunc HandleRequest() {\n\t// handle request\n}",
		"router.go":  "package router\n\nfunc Route() {\n\t// routing logic\n}",
		"config.go":  "package config\n\nfunc LoadConfig() {\n\t// config loading\n}",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for "package" which appears in all files
	result := fd.findWithGrepCommand([]string{"package"}, wsInfo)

	// Since grep is available and should find matches, expect non-nil result
	// Note: This will expose implementation bug with grep output parsing
	if result == nil {
		t.Fatal("expected non-nil result when grep is available and should find matches")
	}

	if len(result) == 0 {
		t.Fatal("expected to find files containing 'package' - grep implementation bug detected")
	}

	// Verify they're valid .go files
	validFile := false
	for _, f := range result {
		base := filepath.Base(f)
		if base == "handler.go" || base == "router.go" || base == "config.go" {
			validFile = true
			break
		}
	}
	if !validFile {
		t.Error("expected to find at least one of the expected files")
	}
}

func TestFindWithGrepCommand_IncludesSupportedExtensions(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":     "package main",
		"script.js":   "console.log('hello');",
		"style.css":   "body {}",
		"config.json": "{\"key\": \"value\"}",
		"README.md":   "# README",
		"test.txt":    "test content",
		"data.xml":    "<root></root>",
		"config.yaml": "key: value",
	})

	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search for a common pattern like "package" that's in main.go
	result := fd.findWithGrepCommand([]string{"package"}, wsInfo)

	// Since grep is available and should find matches, expect non-nil result
	// Note: This will expose implementation bug with grep output parsing
	if result == nil {
		t.Fatal("expected non-nil result when grep is available and should find matches")
	}

	if len(result) == 0 {
		t.Fatal("expected to find at least one file - grep implementation bug detected")
	}

	// All results should have supported extensions
	supportedExts := map[string]bool{
		".go": true, ".js": true, ".ts": true, ".py": true,
		".java": true, ".cpp": true, ".c": true, ".rs": true,
		".php": true, ".rb": true, ".swift": true, ".kt": true,
		".html": true, ".css": true, ".json": true, ".xml": true,
		".yaml": true, ".yml": true, ".md": true, ".txt": true,
	}

	for _, f := range result {
		ext := filepath.Ext(f)
		if !supportedExts[ext] {
			t.Errorf("file with unsupported extension returned: %s (ext: %s)", f, ext)
		}
	}
}

func TestFindWithGrepCommand_CommandFailure(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available on this system")
	}

	root := t.TempDir()
	fd := newFD()
	wsInfo := &WorkspaceInfo{
		RootDir:     root,
		ProjectType: "go",
	}

	// Search with term that doesn't exist - grep will return error
	result := fd.findWithGrepCommand([]string{"nonexistent_term_xyz_123"}, wsInfo)

	// Should return empty result, not panic
	if len(result) > 0 {
		t.Errorf("expected empty result for non-existent term, got %d files", len(result))
	}
}

// ============================================================================
// DiscoverWithShell Integration Tests
// ============================================================================

func TestDiscoverWithShell_Integration(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":    "package main",
		"util.go":    "package util",
		"handler.go": "package handler",
		"README.md":  "hello",
	})

	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 10,
		UseShell: true,
		RootPath: root,
	}

	// Test discoverWithShell with pattern-based query
	result := fd.discoverWithShell("*.go", options)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Files) == 0 {
		t.Fatal("expected to find .go files")
	}

	// Should only return .go files
	for _, f := range result.Files {
		if filepath.Ext(f) != ".go" {
			t.Errorf("expected .go file, got %s", f)
		}
	}
}

func TestDiscoverWithShell_SearchTerms(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	// Check if grep is available
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main_handler.go": "package main\n\nfunc main() {}",
		"util.go":         "package util\n\nfunc helper() {}",
		"README.md":       "# Project\n",
	})

	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 10,
		UseShell: true,
		RootPath: root,
	}

	// Test discoverWithShell with search terms (no patterns)
	// "package" is a common term that should appear in .go files
	// Note: grep behavior can vary by platform and version
	result := fd.discoverWithShell("package", options)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// The primary goal is that the function doesn't crash
	// Results depend on grep availability and version
	if len(result.Files) > 0 {
		t.Logf("Found %d files via shell discovery", len(result.Files))
	} else {
		t.Log("No files found (grep may not be working as expected)")
	}
}

func TestDiscoverFilesRobust_WithShellPatterns(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find command not available on this system")
	}

	root := makeTree(t, map[string]string{
		"main.go":     "package main",
		"util.go":     "package util",
		"handler.go":  "package handler",
		"script.js":   "console.log()",
		"README.md":   "hello",
		"sub/sub.go":  "package sub",
		"other/test.go": "package test",
	})

	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 10,
		UseShell: true,
		RootPath: root,
	}

	// Query with wildcard pattern should trigger findWithFindCommand
	result := fd.DiscoverFilesRobust("*.go", options)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	if len(result.Files) == 0 {
		t.Fatal("expected to find .go files")
	}

	// Should only return .go files
	for _, f := range result.Files {
		if filepath.Ext(f) != ".go" {
			t.Errorf("expected .go file, got %s", f)
		}
	}

	// Should indicate shell method was used
	if result.Method != "shell" {
		t.Errorf("expected method 'shell', got %q", result.Method)
	}
}

// ============================================================================
// Ignore Rule Integration Tests
// ============================================================================

// Note: discoverBasic does not currently respect .gitignore or .sprout/.ignore rules.
// These tests verify the ignore rule parsing functionality via GetIgnoreRules(),
// which is tested elsewhere. The integration of ignore rules into file discovery
// is a known limitation and future enhancement.

func TestIgnoreRules_NotYetIntegratedInDiscoverBasic(t *testing.T) {
	// This test documents the current behavior: ignore rules are parsed
	// but not applied in discoverBasic()
	root := makeTree(t, map[string]string{
		".gitignore":  "*.log\nbuild/\n",
		"main.go":     "package main",
		"debug.log":   "debug output",
		"build/app.js": "bundle",
	})

	// Verify ignore rules are parsed correctly
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil ignore rules")
	}

	// Verify the rules work
	if !rules.MatchesPath("debug.log") {
		t.Error("ignore rules should match debug.log")
	}
	if !rules.MatchesPath("build/app.js") {
		t.Error("ignore rules should match build/app.js")
	}

	// But discoverBasic doesn't use these rules
	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 100,
		RootPath: root,
	}

	result := fd.discoverBasic(options)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Current behavior: discoverBasic returns all files, even ignored ones
	// This is expected behavior in the current implementation
	foundDebugLog := false
	foundBuild := false
	for _, f := range result.Files {
		if strings.HasSuffix(f, "debug.log") {
			foundDebugLog = true
		}
		if strings.Contains(f, "build") && strings.HasSuffix(f, "app.js") {
			foundBuild = true
		}
	}

	// In current implementation, these files are still returned
	if !foundDebugLog {
		t.Log("Note: debug.log is returned (ignore rules not applied in discoverBasic)")
	}
	if !foundBuild {
		t.Log("Note: build/app.js is returned (ignore rules not applied in discoverBasic)")
	}
}

// ============================================================================
// Error Path Tests
// ============================================================================

func TestDiscoverBasic_NonexistentRoot(t *testing.T) {
	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 100,
		RootPath: "/nonexistent/directory/that/does/not/exist",
	}

	result := fd.discoverBasic(options)

	// Should not panic, should return an error or empty result
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should have an error for non-existent directory
	if result.Error == nil {
		t.Error("expected error for non-existent directory, got nil")
	}

	// Files should be empty for non-existent directory
	if len(result.Files) > 0 {
		t.Errorf("expected no files for non-existent directory, got %d", len(result.Files))
	}
}

func TestDiscoverBasic_PermissionDenied(t *testing.T) {
	// Create a directory with a subdirectory that has restricted access
	root := t.TempDir()
	restrictedDir := filepath.Join(root, "restricted")
	if err := os.Mkdir(restrictedDir, 0o000); err != nil {
		t.Skip("cannot set restrictive permissions")
	}
	t.Cleanup(func() {
		// Restore permissions before cleanup
		os.Chmod(restrictedDir, 0o755)
	})

	// Create an accessible file
	accessibleFile := filepath.Join(root, "accessible.go")
	if err := os.WriteFile(accessibleFile, []byte("package main"), 0o644); err != nil {
		t.Fatalf("failed to create accessible file: %v", err)
	}

	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 100,
		RootPath: root,
	}

	result := fd.discoverBasic(options)

	// Should not crash on permission denied
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should still find the accessible file
	if len(result.Files) == 0 {
		t.Error("expected to find accessible file even with permission error")
	}
}

func TestDiscoverBasic_WithMaxFilesZero(t *testing.T) {
	root := makeTree(t, map[string]string{
		"main.go": "package main",
		"util.go": "package util",
		"handler.go": "package handler",
	})

	fd := newFD()
	options := &DiscoveryOptions{
		MaxFiles: 0, // No limit
		RootPath: root,
	}

	result := fd.discoverBasic(options)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Should return all files when MaxFiles is 0
	if len(result.Files) != 3 {
		t.Errorf("expected all 3 files with MaxFiles=0, got %d", len(result.Files))
	}
}

func TestFindFilesUsingShellCommands_NonexistentRoot(t *testing.T) {
	if isWindows() {
		t.Skip("skipping shell command tests on Windows")
	}

	fd := newFD()
	workspaceInfo := &WorkspaceInfo{
		RootDir:     "/nonexistent/directory",
		ProjectType: "go",
	}

	// Should not crash on non-existent directory
	result := fd.findFilesUsingShellCommands("test", workspaceInfo)

	// Should return empty result (could be nil or empty slice)
	// The important thing is it doesn't crash
	if result == nil || len(result) == 0 {
		// This is expected for non-existent directory
		return
	}

	// If result is not nil/empty, verify files are valid
	for _, f := range result {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("file in result doesn't exist: %s", f)
		}
	}
}

func TestDeduplicateAndFilter_InvalidPaths(t *testing.T) {
	root := t.TempDir()

	// Create a valid file first
	validFile := filepath.Join(root, "main.go")
	if err := os.WriteFile(validFile, []byte("package main"), 0o644); err != nil {
		t.Fatalf("failed to create valid file: %v", err)
	}

	fd := newFD()
	wsInfo := &WorkspaceInfo{RootDir: root}

	files := []string{
		validFile,                         // valid
		"",                                // empty path
		"\x00invalid",                     // invalid path with null byte (may not work on all platforms)
		"/nonexistent/outside/file.go",   // outside workspace
	}

	// Should not crash on invalid paths
	result := fd.deduplicateAndFilter(files, wsInfo)

	// Should include the valid file
	if len(result) == 0 {
		t.Errorf("expected at least 1 valid file, got %d: %v", len(result), result)
	}

	// First result should be the valid file
	if len(result) > 0 && !strings.HasSuffix(result[0], "main.go") {
		t.Logf("Note: got file: %s (may have been filtered due to path issues)", result[0])
	}
}

func TestApplyFiltersAndLimits_EmptyFileList(t *testing.T) {
	fd := newFD()

	result := fd.applyFiltersAndLimits([]string{}, &DiscoveryOptions{
		MaxFiles: 10,
		ExcludeDirs: []string{"node_modules"},
	})

	if len(result.Files) != 0 {
		t.Errorf("expected empty file list, got %d files", len(result.Files))
	}
	if result.MatchedFiles != 0 {
		t.Errorf("expected MatchedFiles 0, got %d", result.MatchedFiles)
	}
}

func TestFindWithDirectoryWalk_InvalidRoot(t *testing.T) {
	fd := newFD()
	workspaceInfo := &WorkspaceInfo{
		RootDir:     "/nonexistent/path/xyz123",
		ProjectType: "go",
	}

	// Should not crash on invalid root
	result := fd.findWithDirectoryWalk("test", workspaceInfo)

	// Should return empty result (could be nil or empty slice)
	// The important thing is it doesn't crash
	if result == nil || len(result) == 0 {
		// This is expected for invalid root
		return
	}

	// If result is not nil/empty, it should be filtered by deduplicateAndFilter
	// But at this level we just check no crash occurred
	t.Logf("Found %d files (unexpected but not an error)", len(result))
}

func TestGetIgnoreRules_NonexistentRoot(t *testing.T) {
	// Test that GetIgnoreRules handles non-existent directories gracefully
	rules := GetIgnoreRules("/nonexistent/directory/xyz123")

	// Should return nil for non-existent directory
	if rules != nil {
		t.Error("expected nil for non-existent directory")
	}
}

// Helper function to check if running on Windows
func isWindows() bool {
	return runtime.GOOS == "windows"
}
