package agent

import (
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
