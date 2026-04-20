package filediscovery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		".ledit/.ignore": "secret.txt\n",
	})
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("secret.txt") {
		t.Error("expected secret.txt to be ignored via .ledit/.ignore")
	}
}

func TestGetIgnoreRules_BothFiles(t *testing.T) {
	root := makeTree(t, map[string]string{
		".gitignore":     "*.log\n",
		".ledit/.ignore": "dist/\n",
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
	// Create .gitignore but not .ledit directory - should not error
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}
	if !rules.MatchesPath("app.log") {
		t.Error("expected app.log to be ignored")
	}
}

func TestGetIgnoreRules_LeditPriority(t *testing.T) {
	// Test that .ledit/.ignore rules are applied in addition to .gitignore
	// Both should be combined
	root := makeTree(t, map[string]string{
		".gitignore":     "*.log\n",
		".ledit/.ignore": "secret.txt\n*.tmp\n",
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
		t.Error("expected secret.txt to be ignored (from .ledit/.ignore)")
	}
	if !rules.MatchesPath("temp.tmp") {
		t.Error("expected temp.tmp to be ignored (from .ledit/.ignore)")
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
