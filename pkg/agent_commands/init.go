package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// InitCommand implements the /init slash command
type InitCommand struct{}

// Name returns the command name
func (i *InitCommand) Name() string {
	return "init"
}

// Description returns the command description
func (i *InitCommand) Description() string {
	return "Generate or regenerate the project context documentation"
}

// Execute runs the init command to generate project context
func (i *InitCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Println("🔧 Generating project context...")
	fmt.Println("🔍 Exploring codebase structure...")

	// Get current directory info
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %v", err)
	}

	// Discover existing context files
	existingContext := i.discoverExistingContext()

	// Analyze actual project structure
	projectName := filepath.Base(wd)
	projectStructure := i.analyzeProjectStructure()
	testingInfo := i.discoverTestingFramework()
	entrypoints := i.findEntrypoints()
	buildInfo := i.analyzeBuildSystem()

	// Get current timestamp with local timezone
	timestamp := time.Now().Format("2006-01-02 15:04:05 MST")

	// Generate comprehensive project context based on actual exploration
	context := i.generateEnhancedContext(projectName, existingContext, projectStructure, testingInfo, entrypoints, buildInfo, timestamp)

	// Write to .project_context.md file
	err = os.WriteFile(".project_context.md", []byte(context), 0644)
	if err != nil {
		return fmt.Errorf("failed to write project context: %v", err)
	}

	fmt.Printf("✅ Project context generated successfully!\n")
	fmt.Printf("📄 File: .project_context.md\n")
	fmt.Printf("🕒 Generated: %s\n", timestamp)
	if len(existingContext) > 0 {
		fmt.Printf("📋 Found existing context files: %s\n", strings.Join(existingContext, ", "))
	}
	fmt.Printf("\nUse \"/help\" to see all available commands\n")

	return nil
}

// discoverExistingContext finds existing context files in the project
func (i *InitCommand) discoverExistingContext() []string {
	contextFiles := []string{
		"CLAUDE.md",
		".claude/project.md",
		".claude/context.md",
		".cursor/markdown/project.md",
		".cursor/markdown/context.md",
		".project_context.md",
		"PROJECT_CONTEXT.md",
		".cursorrules",
		".cursor-rules",
		"README.md",
	}

	var found []string
	for _, file := range contextFiles {
		if _, err := os.Stat(file); err == nil {
			found = append(found, file)
		}
	}

	return found
}

// analyzeProjectStructure explores the actual codebase structure
func (i *InitCommand) analyzeProjectStructure() map[string][]string {
	structure := make(map[string][]string)

	// Find main directories
	dirs := []string{"cmd", "pkg", "internal", "src", "lib", "app", "api", "web", "ui"}
	for _, dir := range dirs {
		if files := i.findGoFiles(dir); len(files) > 0 {
			structure[dir] = files
		}
	}

	// Root level Go files
	if files := i.findGoFiles("."); len(files) > 0 {
		structure["."] = files
	}

	return structure
}

// discoverTestingFramework finds testing files and patterns
func (i *InitCommand) discoverTestingFramework() map[string]interface{} {
	testing := make(map[string]interface{})

	// Find test files
	testFiles := i.findTestFiles()
	testing["test_files"] = testFiles

	// Check for test runners and scripts
	scripts := []string{"test.sh", "test_e2e.sh", "validate.sh", "test_runner.py"}
	var foundScripts []string
	for _, script := range scripts {
		if _, err := os.Stat(script); err == nil {
			foundScripts = append(foundScripts, script)
		}
	}
	testing["test_scripts"] = foundScripts

	// Check for E2E test directory
	if _, err := os.Stat("e2e_test_scripts"); err == nil {
		e2eFiles, _ := filepath.Glob("e2e_test_scripts/*.sh")
		testing["e2e_scripts"] = e2eFiles
	}

	return testing
}

// findEntrypoints discovers main entry points and CLI commands
func (i *InitCommand) findEntrypoints() []string {
	var entrypoints []string

	// Check for main.go
	if _, err := os.Stat("main.go"); err == nil {
		entrypoints = append(entrypoints, "main.go")
	}

	// Check cmd directory for multiple entrypoints
	if entries, err := os.ReadDir("cmd"); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				// Check if directory has main.go or <name>.go
				cmdPath := filepath.Join("cmd", entry.Name())
				if files := i.findGoFiles(cmdPath); len(files) > 0 {
					entrypoints = append(entrypoints, cmdPath)
				}
			} else if strings.HasSuffix(entry.Name(), ".go") {
				entrypoints = append(entrypoints, filepath.Join("cmd", entry.Name()))
			}
		}
	}

	return entrypoints
}

// analyzeBuildSystem checks build configuration and dependencies
func (i *InitCommand) analyzeBuildSystem() map[string]interface{} {
	build := make(map[string]interface{})

	// Read go.mod
	if goModContent, err := os.ReadFile("go.mod"); err == nil {
		build["go_mod"] = string(goModContent)

		// Extract Go version and key dependencies
		lines := strings.Split(string(goModContent), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "go ") {
				build["go_version"] = strings.TrimPrefix(line, "go ")
			}
		}
	}

	// Check for Makefile, Dockerfile, etc.
	buildFiles := []string{"Makefile", "Dockerfile", "docker-compose.yml", ".github/workflows"}
	var foundBuildFiles []string
	for _, file := range buildFiles {
		if _, err := os.Stat(file); err == nil {
			foundBuildFiles = append(foundBuildFiles, file)
		}
	}
	build["build_files"] = foundBuildFiles

	return build
}

// findGoFiles recursively finds Go files in a directory
func (i *InitCommand) findGoFiles(dir string) []string {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue // Skip hidden files/directories
		}

		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			// Don't recurse too deep, just check immediate subdirectories
			if dir == "." {
				subFiles := i.findGoFiles(path)
				files = append(files, subFiles...)
			}
		} else if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, path)
		}
	}

	sort.Strings(files)
	return files
}

// findTestFiles finds all test files in the project
func (i *InitCommand) findTestFiles() []string {
	var testFiles []string

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue walking
		}

		// Skip hidden directories (but not the root .) and vendor/node_modules
		if info.IsDir() && path != "." && (strings.HasPrefix(info.Name(), ".") ||
			info.Name() == "vendor" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}

		if strings.HasSuffix(path, "_test.go") {
			testFiles = append(testFiles, path)
		}

		return nil
	})

	if err == nil {
		sort.Strings(testFiles)
	}

	return testFiles
}

// generateEnhancedContext creates a comprehensive context based on actual code exploration
func (i *InitCommand) generateEnhancedContext(projectName string, existingContext []string,
	structure map[string][]string, testing map[string]interface{},
	entrypoints []string, build map[string]interface{}, timestamp string) string {

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Project Context: %s\n\n", projectName))

	// Overview section - check if we have CLAUDE.md to extract real overview
	overview := i.extractOverviewFromExisting(existingContext)
	if overview != "" {
		sb.WriteString("## Overview\n")
		sb.WriteString(overview)
		sb.WriteString("\n\n")
	}

	// Discovered project structure
	sb.WriteString("## Project Structure\n")
	sb.WriteString("*Auto-discovered from codebase exploration*\n\n")

	if len(entrypoints) > 0 {
		sb.WriteString("**Entry Points:**\n")
		for _, entry := range entrypoints {
			sb.WriteString(fmt.Sprintf("- `%s`\n", entry))
		}
		sb.WriteString("\n")
	}

	if len(structure) > 0 {
		sb.WriteString("**Code Organization:**\n")
		for dir, files := range structure {
			if dir == "." {
				sb.WriteString("**Root level:**\n")
			} else {
				sb.WriteString(fmt.Sprintf("**%s/:** \n", dir))
			}
			for _, file := range files {
				if len(files) > 10 {
					sb.WriteString(fmt.Sprintf("- %d Go files\n", len(files)))
					break
				}
				sb.WriteString(fmt.Sprintf("  - `%s`\n", file))
			}
		}
		sb.WriteString("\n")
	}

	// Testing information
	if testFiles, ok := testing["test_files"].([]string); ok && len(testFiles) > 0 {
		sb.WriteString("## Testing Framework\n")
		sb.WriteString(fmt.Sprintf("**Test Files:** %d discovered\n", len(testFiles)))
		if len(testFiles) <= 10 {
			for _, file := range testFiles {
				sb.WriteString(fmt.Sprintf("- `%s`\n", file))
			}
		} else {
			// Show first few and count
			for i, file := range testFiles[:5] {
				sb.WriteString(fmt.Sprintf("- `%s`\n", file))
				if i == 4 {
					sb.WriteString(fmt.Sprintf("- ... and %d more\n", len(testFiles)-5))
				}
			}
		}
		sb.WriteString("\n")
	}

	if scripts, ok := testing["test_scripts"].([]string); ok && len(scripts) > 0 {
		sb.WriteString("**Test Scripts:**\n")
		for _, script := range scripts {
			sb.WriteString(fmt.Sprintf("- `%s`\n", script))
		}
		sb.WriteString("\n")
	}

	if e2eScripts, ok := testing["e2e_scripts"].([]string); ok && len(e2eScripts) > 0 {
		sb.WriteString("**E2E Test Scripts:**\n")
		for _, script := range e2eScripts {
			sb.WriteString(fmt.Sprintf("- `%s`\n", script))
		}
		sb.WriteString("\n")
	}

	// Build system information
	if goVersion, ok := build["go_version"].(string); ok {
		sb.WriteString("## Build System\n")
		sb.WriteString(fmt.Sprintf("**Go Version:** %s\n", goVersion))

		if buildFiles, ok := build["build_files"].([]string); ok && len(buildFiles) > 0 {
			sb.WriteString("**Build Files:**\n")
			for _, file := range buildFiles {
				sb.WriteString(fmt.Sprintf("- `%s`\n", file))
			}
		}
		sb.WriteString("\n")
	}

	// Existing context files
	if len(existingContext) > 0 {
		sb.WriteString("## Existing Documentation\n")
		sb.WriteString("*Found existing context files that may contain additional details:*\n")
		for _, file := range existingContext {
			sb.WriteString(fmt.Sprintf("- `%s`\n", file))
		}
		sb.WriteString("\n")
	}

	// Go module information
	if goMod, ok := build["go_mod"].(string); ok && goMod != "" {
		sb.WriteString("## Dependencies\n")
		sb.WriteString("```\n")
		sb.WriteString(goMod)
		sb.WriteString("```\n\n")
	}

	// Generation timestamp
	sb.WriteString("## Generated\n")
	sb.WriteString(fmt.Sprintf("Context generated on %s by `/init` command with codebase exploration\n", timestamp))

	return sb.String()
}

// extractOverviewFromExisting tries to extract overview from existing context files
func (i *InitCommand) extractOverviewFromExisting(existingFiles []string) string {
	// Priority order for extracting overview
	priority := []string{"CLAUDE.md", "README.md", ".claude/project.md", "PROJECT_CONTEXT.md"}

	for _, priorityFile := range priority {
		for _, existingFile := range existingFiles {
			if existingFile == priorityFile {
				if content, err := agent_tools.ReadFile(context.Background(), existingFile); err == nil {
					// Extract overview section or first few paragraphs
					lines := strings.Split(content, "\n")
					var overview strings.Builder
					inOverview := false

					for _, line := range lines {
						trimmed := strings.TrimSpace(line)

						// Look for overview section
						if strings.Contains(strings.ToLower(trimmed), "overview") && strings.HasPrefix(trimmed, "#") {
							inOverview = true
							continue
						}

						// Stop at next major section
						if inOverview && strings.HasPrefix(trimmed, "#") &&
							!strings.Contains(strings.ToLower(trimmed), "overview") {
							break
						}

						if inOverview && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
							overview.WriteString(line + "\n")
						}

						// If we haven't found overview section, take first substantial paragraph
						if !inOverview && overview.Len() == 0 && len(trimmed) > 50 && !strings.HasPrefix(trimmed, "#") {
							overview.WriteString(trimmed + "\n")
						}

						// Limit overview length
						if overview.Len() > 500 {
							break
						}
					}

					if overview.Len() > 0 {
						return strings.TrimSpace(overview.String())
					}
				}
			}
		}
	}

	return ""
}
