package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alantheprice/ledit/pkg/utils"
)

// detectProjectInfo gathers basic project information for LLM analysis
func detectProjectInfo(logger *utils.Logger) ProjectInfo {
	info := ProjectInfo{}
	commonFiles := []string{"go.mod", "package.json", "requirements.txt", "pyproject.toml", "Makefile", "Dockerfile", "README.md"}
	for _, file := range commonFiles {
		if hasFile(file) {
			info.AvailableFiles = append(info.AvailableFiles, file)
			switch file {
			case "go.mod":
				info.HasGoMod = true
			case "package.json":
				info.HasPackageJSON = true
			case "requirements.txt":
				info.HasRequirements = true
			case "Makefile":
				info.HasMakefile = true
			}
		}
	}
	if files, err := getBasicFileListing(logger); err == nil && len(files) > 0 {
		count := 0
		for _, file := range files {
			if count >= 5 {
				break
			}
			info.AvailableFiles = append(info.AvailableFiles, file)
			count++
		}
	}
	return info
}

// getBasicFileListing returns a simple list of files without full analysis
func getBasicFileListing(logger *utils.Logger) ([]string, error) {
	var files []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Logf("Error walking path %s: %v", path, err)
			return err
		}
		if strings.HasPrefix(filepath.Base(path), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		skipDirs := []string{"node_modules", "vendor", "target", "build", "dist", "__pycache__"}
		for _, skip := range skipDirs {
			if strings.Contains(path, skip) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !info.IsDir() && isSourceFile(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
