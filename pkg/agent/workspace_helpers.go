package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

func findGoFiles(dir string) ([]string, error) {
	var goFiles []string
	cmd := exec.Command("find", dir, "-name", "*.go", "-type", "f")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line != "" && !strings.Contains(line, "vendor/") && !strings.Contains(line, ".git/") {
			goFiles = append(goFiles, strings.TrimPrefix(line, "./"))
		}
	}
	return goFiles, nil
}

func countLines(filePath string) int {
	cmd := exec.Command("wc", "-l", filePath)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(output))
	if len(parts) > 0 {
		if lines, err := strconv.Atoi(parts[0]); err == nil {
			return lines
		}
	}
	return 0
}

func findPackageDirectories(dir string) []string {
	var pkgDirs []string
	cmd := exec.Command("find", dir, "-name", "*.go", "-type", "f", "-exec", "dirname", "{}", ";")
	output, err := cmd.Output()
	if err != nil {
		return pkgDirs
	}
	seen := make(map[string]bool)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		d := strings.TrimPrefix(line, "./")
		if d != "" && !seen[d] && !strings.Contains(d, "vendor/") && !strings.Contains(d, ".git/") {
			seen[d] = true
			pkgDirs = append(pkgDirs, d)
		}
	}
	return pkgDirs
}

func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	sourceExts := []string{".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".rs", ".rb", ".php", ".scala", ".kt"}
	for _, sourceExt := range sourceExts {
		if ext == sourceExt {
			return true
		}
	}
	return false
}

func getRecentlyModifiedSourceFiles(workspaceInfo *WorkspaceInfo, logger *utils.Logger) []string {
	if len(workspaceInfo.AllFiles) == 0 {
		return []string{}
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo
	for _, file := range workspaceInfo.AllFiles {
		if stat, err := os.Stat(file); err == nil {
			files = append(files, fileInfo{path: file, modTime: stat.ModTime()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	var result []string
	for i, file := range files {
		if i >= 5 {
			break
		}
		result = append(result, file.path)
	}
	return result
}

func getCommonEntryPointFiles(projectType string, logger *utils.Logger) []string {
	switch projectType {
	case "go":
		return []string{"main.go", "cmd/main.go", "app/main.go"}
	case "javascript":
		return []string{"index.js", "app.js", "server.js", "src/index.js"}
	case "python":
		return []string{"main.py", "app.py", "__init__.py", "src/main.py"}
	case "java":
		return []string{"Main.java", "App.java", "src/main/java/Main.java"}
	case "rust":
		return []string{"main.rs", "lib.rs", "src/main.rs", "src/lib.rs"}
	default:
		return []string{"README.md", "index.*", "main.*", "app.*"}
	}
}

// buildBasicFileContext concatenates contents of context files into a string for prompts
func buildBasicFileContext(contextFiles []string, logger *utils.Logger) string {
	var b strings.Builder
	for _, file := range contextFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			logger.Logf("Could not read %s for context: %v", file, err)
			continue
		}
		const max = 4000
		s := string(content)
		if len(s) > max {
			s = s[:max] + "\n... [truncated]"
		}
		b.WriteString(fmt.Sprintf("\n\n## File: %s\n````\n%s\n````\n", file, s))
	}
	return b.String()
}

// analyzeWorkspacePatterns analyzes codebase patterns for planning
func analyzeWorkspacePatterns(logger *utils.Logger) *WorkspacePatterns {
	patterns := &WorkspacePatterns{
		AverageFileSize:    0,
		ModularityLevel:    "medium",
		GoSpecificPatterns: make(map[string]string),
	}
	goFiles, err := findGoFiles(".")
	if err != nil {
		logger.Logf("Warning: Could not analyze workspace patterns: %v", err)
		patterns.AverageFileSize = 200
		patterns.PreferredPackageSize = 500
		patterns.ModularityLevel = "high"
		patterns.GoSpecificPatterns["file_organization"] = "prefer_small_focused_files"
		patterns.GoSpecificPatterns["package_structure"] = "pkg_separation"
		return patterns
	}
	totalLines := 0
	largeFiles := 0
	for _, file := range goFiles {
		lines := countLines(file)
		totalLines += lines
		if lines > 500 {
			largeFiles++
		}
	}
	if len(goFiles) > 0 {
		patterns.AverageFileSize = totalLines / len(goFiles)
	}
	if largeFiles > len(goFiles)/3 {
		patterns.ModularityLevel = "low"
		patterns.GoSpecificPatterns["refactoring_preference"] = "break_large_files"
	} else {
		patterns.ModularityLevel = "high"
		patterns.GoSpecificPatterns["refactoring_preference"] = "maintain_separation"
	}
	pkgDirs := findPackageDirectories(".")
	if len(pkgDirs) > 3 {
		patterns.GoSpecificPatterns["package_structure"] = "highly_modular"
	} else {
		patterns.GoSpecificPatterns["package_structure"] = "simple_structure"
	}
	patterns.PreferredPackageSize = patterns.AverageFileSize * 3
	logger.Logf("Workspace Analysis: Avg file size: %d, Modularity: %s, Large files: %d/%d",
		patterns.AverageFileSize, patterns.ModularityLevel, largeFiles, len(goFiles))
	return patterns
}

func isLargeFileRefactoringTask(userIntent string, contextFiles []string, logger *utils.Logger) bool {
	intentLower := strings.ToLower(userIntent)
	refactoringKeywords := []string{"refactor", "split", "break down", "reorganize", "move", "extract"}
	hasRefactoringIntent := false
	for _, keyword := range refactoringKeywords {
		if strings.Contains(intentLower, keyword) {
			hasRefactoringIntent = true
			break
		}
	}
	if !hasRefactoringIntent {
		return false
	}
	for _, file := range contextFiles {
		if lines := countLines(file); lines > 1000 {
			logger.Logf("Detected large file refactoring task: %s has %d lines", file, lines)
			return true
		}
	}
	return false
}

func extractSourceFileFromIntent(userIntent string, contextFiles []string) string {
	intentLower := strings.ToLower(userIntent)
	if strings.Contains(intentLower, "cmd/agent.go") {
		return "cmd/agent.go"
	}
	for _, file := range contextFiles {
		if countLines(file) > 1000 {
			return file
		}
	}
	return ""
}

func analyzeFunctionsInFile(filePath string, logger *utils.Logger) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		logger.Logf("Could not read file %s for function analysis: %v", filePath, err)
		return "Could not analyze functions in source file"
	}
	lines := strings.Split(string(content), "\n")
	var functions, types, structs []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") && !strings.Contains(trimmed, "//") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				funcName := parts[1]
				if idx := strings.Index(funcName, "("); idx > 0 {
					funcName = funcName[:idx]
				}
				functions = append(functions, fmt.Sprintf("Line %d: func %s", i+1, funcName))
			}
		}
		if strings.HasPrefix(trimmed, "type ") && !strings.Contains(trimmed, "//") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				typeName := parts[1]
				typeKind := parts[2]
				if typeKind == "struct" {
					structs = append(structs, fmt.Sprintf("Line %d: type %s struct", i+1, typeName))
				} else {
					types = append(types, fmt.Sprintf("Line %d: type %s %s", i+1, typeName, typeKind))
				}
			}
		}
	}
	var result []string
	if len(functions) > 0 {
		result = append(result, fmt.Sprintf("FUNCTIONS (%d found):", len(functions)))
		limit := len(functions)
		if limit > 20 {
			limit = 20
		}
		for i := 0; i < limit; i++ {
			result = append(result, "- "+functions[i])
		}
		if len(functions) > 20 {
			result = append(result, fmt.Sprintf("... and %d more functions", len(functions)-20))
		}
	}
	if len(structs) > 0 {
		result = append(result, fmt.Sprintf("\nSTRUCTS (%d found):", len(structs)))
		for _, s := range structs {
			result = append(result, "- "+s)
		}
	}
	if len(types) > 0 {
		result = append(result, fmt.Sprintf("\nTYPES (%d found):", len(types)))
		for _, t := range types {
			result = append(result, "- "+t)
		}
	}
	if len(result) == 0 {
		return "No functions, types, or structs found for extraction"
	}
	return strings.Join(result, "\n")
}

func generateRefactoringStrategy(userIntent string, contextFiles []string, patterns *WorkspacePatterns, logger *utils.Logger) string {
	strategy := []string{
		"INTELLIGENT REFACTORING STRATEGY:",
		fmt.Sprintf("- Workspace prefers files with ~%d lines (current average)", patterns.AverageFileSize),
		fmt.Sprintf("- Modularity level: %s", patterns.ModularityLevel),
	}
	for _, file := range contextFiles {
		lines := countLines(file)
		if lines > 1000 {
			strategy = append(strategy, fmt.Sprintf("- File %s (%d lines) should be broken into ~%d smaller files",
				file, lines, (lines/patterns.PreferredPackageSize)+1))
		}
	}
	strategy = append(strategy, []string{
		"",
		"GO BEST PRACTICES FOR REFACTORING:",
		"1. Group related types and functions into logical packages",
		"2. Separate interfaces from implementations",
		"3. Create focused files: types.go, handlers.go, utils.go, etc.",
		"4. Maintain clear import dependencies",
		"5. Use meaningful package and file names",
		"",
		"EXECUTION APPROACH:",
		"- Create step-by-step plan with dependency order",
		"- Move types first, then interfaces, then implementations",
		"- Update imports in dependent files",
		"- Verify compilation after each major step",
	}...)
	return strings.Join(strategy, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
