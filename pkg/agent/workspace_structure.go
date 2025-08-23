package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// Note: WorkspaceInfo is defined in types.go in this package

// buildWorkspaceStructure creates comprehensive workspace analysis
func buildWorkspaceStructure(logger *utils.Logger) (*WorkspaceInfo, error) {
	logger.Logf("Building comprehensive workspace structure...")

	info := &WorkspaceInfo{FilesByDir: make(map[string][]string), RelevantFiles: make(map[string]string)}

	if _, err := os.Stat("go.mod"); err == nil {
		info.ProjectType = "go"
	} else if _, err := os.Stat("package.json"); err == nil {
		info.ProjectType = "javascript"
	} else if _, err := os.Stat("requirements.txt"); err == nil || hasFile("setup.py") {
		info.ProjectType = "python"
	} else if _, err := os.Stat("Cargo.toml"); err == nil {
		info.ProjectType = "rust"
	} else if _, err := os.Stat("pom.xml"); err == nil {
		info.ProjectType = "java"
	} else {
		info.ProjectType = "other"
	}

	logger.Logf("Detected project type: %s", info.ProjectType)

	err := filepath.Walk(".", func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") || strings.Contains(path, "node_modules") || strings.Contains(path, "vendor") || strings.Contains(path, "__pycache__") {
			if fileInfo.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !fileInfo.IsDir() && isSourceFile(path) {
			dir := filepath.Dir(path)
			info.AllFiles = append(info.AllFiles, path)
			info.FilesByDir[dir] = append(info.FilesByDir[dir], path)
			if dir == "." {
				info.RootFiles = append(info.RootFiles, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}
	logger.Logf("Found %d source files across %d directories", len(info.AllFiles), len(info.FilesByDir))
	return info, nil
}
