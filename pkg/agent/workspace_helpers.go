package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/utils"
)

// Note: WorkspaceInfo is defined in types.go in this package

func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".java", ".c", ".cpp", ".rs", ".rb", ".php", ".scala", ".kt", ".md", ".txt":
		return true
	default:
		return false
	}
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

func buildBasicFileContext(contextFiles []string, logger *utils.Logger) string {
	return buildBasicFileContextCapped(contextFiles, logger, 8, 40000)
}

func buildBasicFileContextCapped(contextFiles []string, logger *utils.Logger, maxFiles int, maxBytes int) string {
	if maxFiles <= 0 {
		maxFiles = 8
	}
	if maxBytes <= 0 {
		maxBytes = 40000
	}
	seen := map[string]bool{}
	var files []string
	for _, f := range contextFiles {
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		files = append(files, f)
		if len(files) >= maxFiles {
			break
		}
	}
	type result struct {
		path    string
		content string
	}
	outCh := make(chan result, len(files))
	sem := make(chan struct{}, 4)
	for _, f := range files {
		sem <- struct{}{}
		go func(path string) {
			defer func() { <-sem }()
			b, err := os.ReadFile(path)
			if err != nil {
				logger.Logf("Could not read %s for context: %v", path, err)
				outCh <- result{path: path, content: ""}
				return
			}
			outCh <- result{path: path, content: string(b)}
		}(f)
	}
	// drain
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}
	close(outCh)
	var b strings.Builder
	used := 0
	for r := range outCh {
		if r.content == "" {
			continue
		}
		s := r.content
		if used+len(s) > maxBytes {
			remain := maxBytes - used
			if remain <= 0 {
				break
			}
			if remain < len(s) {
				s = s[:remain] + "\n... [truncated]"
			}
		}
		const perFileMax = 4000
		if len(s) > perFileMax {
			s = s[:perFileMax] + "\n... [truncated]"
		}
		b.WriteString(fmt.Sprintf("\n\n## File: %s\n````\n%s\n````\n", r.path, s))
		used += len(s)
		if used >= maxBytes {
			break
		}
	}
	return b.String()
}
