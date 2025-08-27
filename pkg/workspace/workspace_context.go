package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/git"
	"github.com/alantheprice/ledit/pkg/utils"
)

// fileTreeNode represents a node in the file system tree structure.
type fileTreeNode struct {
	Name     string
	IsFile   bool
	Children map[string]*fileTreeNode
}

// buildFileTree constructs a tree structure from a list of file paths.
func buildFileTree(filePaths []string) *fileTreeNode {
	root := &fileTreeNode{
		Name:     ".", // Represents the root of the project
		IsFile:   false,
		Children: make(map[string]*fileTreeNode),
	}

	for _, path := range filePaths {
		parts := strings.Split(path, string(os.PathSeparator))
		currentNode := root
		for i, part := range parts {
			if _, ok := currentNode.Children[part]; !ok {
				currentNode.Children[part] = &fileTreeNode{
					Name:     part,
					IsFile:   false, // Assume directory until proven file
					Children: make(map[string]*fileTreeNode),
				}
			}
			currentNode = currentNode.Children[part]
			if i == len(parts)-1 {
				currentNode.IsFile = true // Mark as file if it's the last part
			}
		}
	}
	return root
}

// printFileTree recursively prints the file tree structure with indentation.
func printFileTree(node *fileTreeNode, b *strings.Builder, prefix string, isLast bool) {
	if node.Name == "." && len(node.Children) == 0 {
		// If it's an empty root, don't print anything
		return
	}

	// Don't print the root node itself, just its children
	if node.Name != "." {
		b.WriteString(prefix)
		if isLast {
			b.WriteString("└── ")
			prefix += "    "
		} else {
			b.WriteString("├── ")
			prefix += "│   "
		}

		b.WriteString(node.Name)
		if !node.IsFile {
			b.WriteString("/") // Append slash for directories
		}
		b.WriteString("\n")
	}

	// Sort children keys for consistent output
	var sortedChildNames []string
	for name := range node.Children {
		sortedChildNames = append(sortedChildNames, name)
	}
	sort.Strings(sortedChildNames)

	for i, name := range sortedChildNames {
		child := node.Children[name]
		printFileTree(child, b, prefix, i == len(sortedChildNames)-1) // FIX: Changed &b to b
	}
}

// getWorkspaceInfo formats the workspace information for the LLM.
// It lists all files, provides full content for selected files, and summaries for others.
func getWorkspaceInfo(workspace WorkspaceFile, fullContextFiles, summaryContextFiles []string, projectGoals ProjectGoals, cfg *config.Config) string {
	logger := utils.GetLogger(false) // Get logger instance
	var b strings.Builder
	b.WriteString("--- Start of full content from workspace ---\n")

	// Add Project Insights (high-signal, up front)
	if (workspace.ProjectInsights != ProjectInsights{}) {
		b.WriteString("--- Project Insights ---\n")
		appendIf := func(name, val string) {
			if strings.TrimSpace(val) != "" {
				b.WriteString(fmt.Sprintf("%s: %s\n", name, val))
			}
		}
		appendIf("Primary Frameworks", workspace.ProjectInsights.PrimaryFrameworks)
		appendIf("Key Dependencies", workspace.ProjectInsights.KeyDependencies)
		appendIf("Build System", workspace.ProjectInsights.BuildSystem)
		appendIf("Test Strategy", workspace.ProjectInsights.TestStrategy)
		appendIf("Architecture", workspace.ProjectInsights.Architecture)
		appendIf("Monorepo", workspace.ProjectInsights.Monorepo)
		appendIf("CI Providers", workspace.ProjectInsights.CIProviders)
		appendIf("Runtime Targets", workspace.ProjectInsights.RuntimeTargets)
		appendIf("Deployment Targets", workspace.ProjectInsights.DeploymentTargets)
		appendIf("Package Managers", workspace.ProjectInsights.PackageManagers)
		appendIf("Repo Layout", workspace.ProjectInsights.RepoLayout)
		b.WriteString("\n")
	}

	// Add Git Repository Information
	b.WriteString("--- Git Repository Information ---\n")
	remoteURL, err := git.GetGitRemoteURL()
	if err == nil && remoteURL != "" {
		b.WriteString(fmt.Sprintf("Git Remote URL: %s\n", remoteURL))
	} else if err != nil {
		b.WriteString(fmt.Sprintf("Could not retrieve Git remote URL: %v\n", err))
	} else {
		b.WriteString("No Git remote configured.\n")
	}
	b.WriteString("This provides information about the current Git repository.\n\n")

	// Add Git Status Information
	branch, uncommitted, staged, statusErr := git.GetGitStatus()
	if statusErr == nil {
		b.WriteString("--- Git Status Information ---\n")
		b.WriteString(fmt.Sprintf("Current Branch: %s\n", branch))
		b.WriteString(fmt.Sprintf("Uncommitted Changes: %d\n", uncommitted))
		b.WriteString(fmt.Sprintf("Staged Changes: %d\n", staged))

		// Add detailed information about uncommitted changes
		if uncommitted > 0 {
			uncommittedChanges, diffErr := git.GetUncommittedChanges()
			if diffErr == nil && uncommittedChanges != "" {
				b.WriteString(fmt.Sprintf("Uncommitted Changes Diff:\n%s\n", uncommittedChanges))
			} else if diffErr != nil {
				b.WriteString(fmt.Sprintf("Could not retrieve uncommitted changes diff: %v\n", diffErr))
			}
		}

		// Add detailed information about staged changes
		if staged > 0 {
			stagedChanges, diffErr := git.GetStagedChanges()
			if diffErr == nil && stagedChanges != "" {
				b.WriteString(fmt.Sprintf("Staged Changes Diff:\n%s\n", stagedChanges))
			} else if diffErr != nil {
				b.WriteString(fmt.Sprintf("Could not retrieve staged changes diff: %v\n", diffErr))
			}
		}

		b.WriteString("This provides an overview of the current Git status and changes.\n\n")
	} else {
		b.WriteString("--- Git Status Information ---\n")
		b.WriteString(fmt.Sprintf("Could not retrieve Git status: %v\n", statusErr))
		b.WriteString("This may indicate no changes, or an issue with Git.\n\n")
	}

	// Add Project Goals if available
	if projectGoals.Mission != "" || projectGoals.PrimaryFunctions != "" || projectGoals.SuccessMetrics != "" {
		b.WriteString("--- Project Goals ---\n")
		if projectGoals.Mission != "" {
			b.WriteString(fmt.Sprintf("Mission: %s\n", projectGoals.Mission))
		}
		if projectGoals.PrimaryFunctions != "" {
			b.WriteString(fmt.Sprintf("Primary Functions: %s\n", projectGoals.PrimaryFunctions))
		}
		if projectGoals.SuccessMetrics != "" {
			b.WriteString(fmt.Sprintf("Success Metrics: %s\n", projectGoals.SuccessMetrics))
		}
		b.WriteString("\n")
	}

	// Add Code Style Preferences
	b.WriteString("--- Code Style Preferences ---\n")
	b.WriteString(fmt.Sprintf("Function Size: %s\n", cfg.CodeStyle.FunctionSize))
	b.WriteString(fmt.Sprintf("File Size: %s\n", cfg.CodeStyle.FileSize))
	b.WriteString(fmt.Sprintf("Naming Conventions: %s\n", cfg.CodeStyle.NamingConventions))
	b.WriteString(fmt.Sprintf("Error Handling: %s\n", cfg.CodeStyle.ErrorHandling))
	b.WriteString(fmt.Sprintf("Testing Approach: %s\n", cfg.CodeStyle.TestingApproach))
	b.WriteString(fmt.Sprintf("Modularity: %s\n", cfg.CodeStyle.Modularity))
	b.WriteString("\n")

	// Convert slices to maps for efficient lookup
	fullContextMap := make(map[string]bool)
	for _, f := range fullContextFiles {
		fullContextMap[f] = true
	}

	summaryContextMap := make(map[string]bool)
	for _, f := range summaryContextFiles {
		summaryContextMap[f] = true
	}

	// 1. Efficient file listing with semantic grouping
	b.WriteString("--- Workspace Structure (Token-Efficient) ---\n")
	var allFilePaths []string
	for filePath := range workspace.Files {
		allFilePaths = append(allFilePaths, filePath)
	}
	sort.Strings(allFilePaths)

	// Group files by type and directory for better organization
	fileGroups := make(map[string][]string)
	for _, filePath := range allFilePaths {
		// Extract the primary directory or file type
		parts := strings.Split(filePath, "/")
		var groupKey string
		if len(parts) <= 1 {
			groupKey = "root"
		} else {
			// Use the first directory as group key
			groupKey = parts[0]
		}
		fileGroups[groupKey] = append(fileGroups[groupKey], filePath)
	}

	// Sort groups and print efficiently
	var sortedGroups []string
	for group := range fileGroups {
		sortedGroups = append(sortedGroups, group)
	}
	sort.Strings(sortedGroups)

	for _, group := range sortedGroups {
		files := fileGroups[group]
		sort.Strings(files) // Sort files within group

		b.WriteString(fmt.Sprintf("%s/ (%d files):\n", group, len(files)))
		for _, file := range files {
			b.WriteString(fmt.Sprintf("  %s\n", file))
		}
		b.WriteString("\n")
	}

	// 2. Prioritized context with intelligent content selection
	b.WriteString("--- Prioritized Context (Token-Optimized) ---\n\n")

	// Calculate content budget based on available context
	const maxContextTokens = 10000 // Conservative limit for context
	usedTokens := 0

	// Full Context Files (high priority, limited)
	b.WriteString("### High-Priority Files (Full Content):\n")
	fullContextAdded := false
	for _, filePath := range allFilePaths {
		if fullContextMap[filePath] && usedTokens < maxContextTokens {
			fileInfo, exists := workspace.Files[filePath]
			if !exists {
				continue
			}

			// Skip if file would exceed token budget
			if fileInfo.TokenCount > 4500 {
				b.WriteString(fmt.Sprintf("📄 %s (large file - summary only)\n", filePath))
				b.WriteString(fmt.Sprintf("   Summary: %s\n", fileInfo.Summary))
				if fileInfo.Exports != "" {
					b.WriteString(fmt.Sprintf("   Exports: %s\n", fileInfo.Exports))
				}
				b.WriteString("\n")
				fullContextAdded = true
				continue
			}

			// Convert relative path to absolute path
			absPath := filePath
			if !filepath.IsAbs(filePath) {
				cwd, err := os.Getwd()
				if err != nil {
					b.WriteString(fmt.Sprintf("⚠️  Could not get current working directory: %v\n", err))
					continue
				}
				absPath = filepath.Join(cwd, filePath)
			}

			content, err := os.ReadFile(absPath)
			if err != nil {
				b.WriteString(fmt.Sprintf("⚠️  Could not read %s: %v\n", filePath, err))
				continue
			}

			// Estimate tokens for this content
			contentTokens := len(content) / 3 // Rough approximation
			if usedTokens+contentTokens > maxContextTokens {
				// Switch to summary mode
				b.WriteString(fmt.Sprintf("📄 %s (summary - token limit reached)\n", filePath))
				b.WriteString(fmt.Sprintf("   Summary: %s\n", fileInfo.Summary))
				b.WriteString("\n")
				fullContextAdded = true
				break
			}

			lang := getLanguageFromFilename(filePath)
			b.WriteString(fmt.Sprintf("📄 %s\n", filePath))
			b.WriteString(fmt.Sprintf("```%s\n%s\n```\n", lang, string(content)))
			usedTokens += contentTokens
			fullContextAdded = true
		}
	}
	if !fullContextAdded {
		b.WriteString("No files selected for full context.\n\n")
	}

	// Summary Context Files (compact format)
	b.WriteString("### Supporting Files (Summaries):\n")
	summaryContextAdded := false // Limit to prevent token explosion
	const maxSummaries = 50
	summaryCount := 0

	for _, filePath := range allFilePaths {
		if summaryCount >= maxSummaries {
			remaining := 0
			for _, remainingPath := range allFilePaths[summaryCount:] {
				if summaryContextMap[remainingPath] && !fullContextMap[remainingPath] {
					remaining++
				}
			}
			if remaining > 0 {
				b.WriteString(fmt.Sprintf("... and %d more files (truncated for token efficiency)\n", remaining))
			}
			break
		}

		if summaryContextMap[filePath] && !fullContextMap[filePath] {
			fileInfo, exists := workspace.Files[filePath]
			if !exists {
				continue
			}

			// Compact format to save tokens
			summaryLine := fmt.Sprintf("📁 %s: %s", filePath, fileInfo.Summary)
			if fileInfo.Exports != "" {
				summaryLine += fmt.Sprintf(" (Exports: %s)", fileInfo.Exports)
			}
			if len(fileInfo.SecurityConcerns) > 0 {
				summaryLine += fmt.Sprintf(" ⚠️ %s", strings.Join(fileInfo.SecurityConcerns, ", "))
			}
			b.WriteString(summaryLine + "\n")

			summaryContextAdded = true
			summaryCount++
		}
	}
	if !summaryContextAdded {
		b.WriteString("No additional files selected for summary context.\n\n")
	}
	b.WriteString("--- End of full content from workspace ---\n")
	logger.Log(b.String())

	// Return a brief summary for the console
	var summary strings.Builder
	summary.WriteString("Workspace context has been loaded and logged.\n")
	summary.WriteString(fmt.Sprintf("- %d files in workspace\n", len(allFilePaths)))
	summary.WriteString(fmt.Sprintf("- %d files selected for full context\n", len(fullContextFiles)))
	summary.WriteString(fmt.Sprintf("- %d files selected for summary context\n", len(summaryContextFiles)))

	return b.String()
}

// GetWorkspaceTree returns a formatted string representation of the file tree from the workspace.
func GetWorkspaceTree() (string, error) {
	ws, err := LoadWorkspaceFile() // Load the workspace
	if err != nil {
		return "", fmt.Errorf("failed to load workspace file: %w", err)
	}
	return GetFormattedFileTree(ws)
}

// getLanguageFromFilename infers the programming language from the file extension.
func getLanguageFromFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cpp", ".hpp":
		return "cpp"
	case ".sh", ".bash":
		return "bash"
	case ".md":
		return "markdown"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".xml":
		return "xml"
	case ".sql":
		return "sql"
	case ".rb":
		return "ruby"
	case ".swift":
		return "swift"
	case ".kt":
		return "kotlin"
	case ".scala":
		return "scala"
	case ".rs":
		return "rust"
	case ".dart":
		return "dart"
	case ".pl", ".pm":
		return "perl"
	case ".lua":
		return "lua"
	case ".vim":
		return "vimscript"
	case ".toml":
		return "toml"
	default:
		return "text"
	}
}

// GetProgressiveWorkspaceContext attempts multiple context loading strategies with fallbacks
func GetProgressiveWorkspaceContext(instructions string, cfg *config.Config) string {
	logger := utils.GetLogger(cfg.SkipPrompt)

	// Try minimal context first
	minimal := GetMinimalWorkspaceContext(instructions, cfg)
	if minimal != "" && !strings.Contains(minimal, "No workspace context available") {
		return minimal
	}

	// Try directory structure if minimal fails
	dirContext := getDirectoryStructureContext(".")
	if dirContext != "" {
		logger.LogProcessStep("Using directory structure context as fallback")
		return dirContext
	}

	// Fallback to intent-based suggestions
	logger.LogProcessStep("Using intent-based context as final fallback")
	return generateContextFromIntent(instructions)
}

// getDirectoryStructureContext creates basic directory structure context
func getDirectoryStructureContext(rootDir string) string {
	var b strings.Builder

	// Walk the directory tree and build a simple structure
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		// Skip hidden directories and common build artifacts
		if strings.HasPrefix(info.Name(), ".") ||
			info.Name() == "node_modules" ||
			info.Name() == "target" ||
			info.Name() == "__pycache__" {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Only show first 3 levels to avoid overwhelming context
		depth := strings.Count(path, string(os.PathSeparator))
		if depth > 2 {
			return nil
		}

		relPath, _ := filepath.Rel(rootDir, path)
		if relPath == "." {
			b.WriteString("Project root:\n")
			return nil
		}

		// Add indentation based on depth
		indent := strings.Repeat("  ", depth)
		if info.IsDir() {
			b.WriteString(fmt.Sprintf("%s%s/\n", indent, info.Name()))
		} else {
			b.WriteString(fmt.Sprintf("%s%s\n", indent, info.Name()))
		}

		return nil
	})

	if err != nil || b.Len() == 0 {
		return ""
	}

	return fmt.Sprintf("Directory structure:\n%s", b.String())
}

// generateContextFromIntent creates context based on user intent keywords
func generateContextFromIntent(intent string) string {
	intentLower := strings.ToLower(intent)

	var suggestions []string

	if strings.Contains(intentLower, "monorepo") {
		suggestions = append(suggestions, "- Expected structure: backend/, frontend/, shared/")
		suggestions = append(suggestions, "- Monorepo typically needs package managers and build scripts")
	}

	if strings.Contains(intentLower, "react") || strings.Contains(intentLower, "vite") {
		suggestions = append(suggestions, "- React + Vite setup needs: package.json, vite.config.js, src/")
		suggestions = append(suggestions, "- Frontend typically in: frontend/ or client/")
	}

	if strings.Contains(intentLower, "go") || strings.Contains(intentLower, "echo") {
		suggestions = append(suggestions, "- Go backend needs: go.mod, main.go, handlers/")
		suggestions = append(suggestions, "- Backend typically in: backend/ or server/")
	}

	if strings.Contains(intentLower, "sqlite") || strings.Contains(intentLower, "database") {
		suggestions = append(suggestions, "- Database setup needs: migrations/, models/, db config")
	}

	if len(suggestions) == 0 {
		return "Empty workspace - ready for project setup"
	}

	return fmt.Sprintf("Empty workspace context:\n%s", strings.Join(suggestions, "\n"))
}

// GetMinimalWorkspaceContext generates a lightweight context with only summaries and exports from workspace.json
// This approach significantly reduces token usage and forces the LLM to make targeted file reads
func GetMinimalWorkspaceContext(instructions string, cfg *config.Config) string {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("--- Loading ultra-minimal workspace context ---")

	workspace, err := validateAndUpdateWorkspace("./", cfg)
	if err != nil {
		logger.Logf("Error loading workspace: %v. Continuing with empty context.\n", err)
		return "No workspace context available. Use read_file tool to load specific files as needed."
	}

	var b strings.Builder

	// Ultra-minimal: Essential project info
	if workspace.ProjectGoals.Mission != "" {
		// Truncate long goals to keep it concise
		goal := workspace.ProjectGoals.Mission
		if len(goal) > 80 {
			goal = goal[:77] + "..."
		}
		b.WriteString(fmt.Sprintf("Project: %s\n", goal))
	} else {
		b.WriteString("Project: Unknown\n")
	}

	// Monorepo-aware project info
	if workspace.MonorepoType != "" && workspace.MonorepoType != "single" {
		b.WriteString(fmt.Sprintf(" | Monorepo: %s with %d projects", workspace.MonorepoType, len(workspace.Projects)))
	} else {
		b.WriteString(fmt.Sprintf(" | Type: %s project", workspace.MonorepoType))
	}

	insights := []string{}
	if workspace.ProjectInsights.PrimaryFrameworks != "" {
		insights = append(insights, fmt.Sprintf("Tech: %s", workspace.ProjectInsights.PrimaryFrameworks))
	}
	if workspace.ProjectInsights.Architecture != "" {
		insights = append(insights, fmt.Sprintf("Arch: %s", workspace.ProjectInsights.Architecture))
	}
	if workspace.ProjectInsights.RuntimeTargets != "" {
		insights = append(insights, fmt.Sprintf("Runtime: %s", workspace.ProjectInsights.RuntimeTargets))
	}

	if len(insights) > 0 {
		b.WriteString(fmt.Sprintf(" | %s", strings.Join(insights, " | ")))
	}
	b.WriteString("\n")

	// Show essential directories and key files for project context
	b.WriteString("\nStructure:\n")

	// Show detected projects first if it's a monorepo
	if len(workspace.Projects) > 0 {
		var projectNames []string
		for name := range workspace.Projects {
			projectNames = append(projectNames, name)
		}
		sort.Strings(projectNames)

		for _, name := range projectNames {
			project := workspace.Projects[name]
			b.WriteString(fmt.Sprintf("📦 %s/ (%s %s)\n", project.Path, project.Language, project.Type))
		}
	}

	var sortedFiles []string
	for filePath := range workspace.Files {
		sortedFiles = append(sortedFiles, filePath)
	}
	sort.Strings(sortedFiles)

	// Essential files that provide immediate project context
	essentialFiles := map[string]bool{
		"main.go":        true, // Entry point
		"go.mod":         true, // Dependencies and module
		"package.json":   true, // Node.js projects
		"pyproject.toml": true, // Python projects
		"Cargo.toml":     true, // Rust projects
		"README.md":      true, // Project documentation
	}

	// Collect directories and essential files
	var dirs []string
	var keyFiles []string

	for _, filePath := range sortedFiles {
		parts := strings.Split(filePath, "/")
		if len(parts) > 0 {
			topLevel := parts[0]
			if strings.Contains(topLevel, ".") { // It's a file in root
				if essentialFiles[topLevel] {
					keyFiles = append(keyFiles, topLevel)
				}
			} else { // It's a directory
				dirPath := topLevel + "/"
				// Only add if not already in list
				found := false
				for _, existing := range dirs {
					if existing == dirPath {
						found = true
						break
					}
				}
				if !found {
					dirs = append(dirs, dirPath)
				}
			}
		}
	}

	// Sort both lists
	sort.Strings(dirs)
	sort.Strings(keyFiles)

	// Show directories first
	for _, dir := range dirs {
		b.WriteString(fmt.Sprintf("📁 %s\n", dir))
	}

	// Show essential files
	for _, file := range keyFiles {
		b.WriteString(fmt.Sprintf("📄 %s\n", file))
	}

	// Show count of additional files if many exist
	totalRootFiles := 0
	for _, filePath := range sortedFiles {
		if !strings.Contains(filePath, "/") {
			totalRootFiles++
		}
	}
	additionalFiles := totalRootFiles - len(keyFiles)
	if additionalFiles > 0 {
		b.WriteString(fmt.Sprintf("📄 ... (%d more files)\n", additionalFiles))
	}

	// Minimal instruction
	b.WriteString("\nTip: Use tools to explore specific files as needed.\n")

	return b.String()
}

// GetFormattedFileTree generates a string representation of the file tree from the workspace.
func GetFormattedFileTree(ws WorkspaceFile) (string, error) {
	var allFilePaths []string
	for filePath := range ws.Files {
		allFilePaths = append(allFilePaths, filePath)
	}
	sort.Strings(allFilePaths) // Sort for consistent output

	rootNode := buildFileTree(allFilePaths)
	var b strings.Builder
	// Print the root node's children, starting with no prefix and not as the last child of a non-existent parent
	// The root node itself is represented by ".", so we iterate its children directly.
	var sortedRootChildNames []string
	for name := range rootNode.Children {
		sortedRootChildNames = append(sortedRootChildNames, name)
	}
	sort.Strings(sortedRootChildNames)

	for i, name := range sortedRootChildNames {
		child := rootNode.Children[name]
		printFileTree(child, &b, "", i == len(sortedRootChildNames)-1)
	}
	return b.String(), nil
}

// GetFullWorkspaceSummary generates the full workspace information string for the LLM,
// including all files as summary context.
func GetFullWorkspaceSummary(ws WorkspaceFile, codeStyle config.CodeStylePreferences, cfg *config.Config, logger *utils.Logger) (string, error) {
	var allFilePaths []string
	for filePath := range ws.Files {
		allFilePaths = append(allFilePaths, filePath)
	}
	sort.Strings(allFilePaths) // Ensure consistent order

	var allFilesAsSummaries []string
	for _, file := range allFilePaths {
		allFilesAsSummaries = append(allFilesAsSummaries, file)
	}
	// Pass a generic instruction for the embedding model to select files for a "full" summary.
	// The embedding model will decide which files are most relevant for a general overview.
	return getWorkspaceInfo(ws, nil, allFilesAsSummaries, ws.ProjectGoals, cfg), nil
}
