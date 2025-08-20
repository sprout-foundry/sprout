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
	if projectGoals.OverallGoal != "" || projectGoals.KeyFeatures != "" || projectGoals.TargetAudience != "" || projectGoals.TechnicalVision != "" {
		b.WriteString("--- Project Goals ---\n")
		if projectGoals.OverallGoal != "" {
			b.WriteString(fmt.Sprintf("Overall Goal: %s\n", projectGoals.OverallGoal))
		}
		if projectGoals.KeyFeatures != "" {
			b.WriteString(fmt.Sprintf("Key Features: %s\n", projectGoals.KeyFeatures))
		}
		if projectGoals.TargetAudience != "" {
			b.WriteString(fmt.Sprintf("Target Audience: %s\n", projectGoals.TargetAudience))
		}
		if projectGoals.TechnicalVision != "" {
			b.WriteString(fmt.Sprintf("Technical Vision: %s\n", projectGoals.TechnicalVision))
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
			if fileInfo.TokenCount > 1000 { // Use actual token count
				b.WriteString(fmt.Sprintf("📄 %s (large file - summary only)\n", filePath))
				b.WriteString(fmt.Sprintf("   Summary: %s\n", fileInfo.Summary))
				if fileInfo.Exports != "" {
					b.WriteString(fmt.Sprintf("   Exports: %s\n", fileInfo.Exports))
				}
				b.WriteString("\n")
				fullContextAdded = true
				continue
			}

			content, err := os.ReadFile(filePath)
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
	summaryContextAdded := false
	const maxSummaries = 10 // Limit to prevent token explosion
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

// GetMinimalWorkspaceContext generates a lightweight context with only summaries and exports from workspace.json
// This approach significantly reduces token usage and forces the LLM to make targeted file reads
func GetMinimalWorkspaceContext(instructions string, cfg *config.Config) string {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("--- Loading minimal workspace context (summaries and exports only) ---")

	workspace, err := validateAndUpdateWorkspace("./", cfg)
	if err != nil {
		logger.Logf("Error loading workspace: %v. Continuing with empty context.\n", err)
		return "No workspace context available. Use read_file tool to load specific files as needed."
	}

	var b strings.Builder
	b.WriteString("=== MINIMAL WORKSPACE CONTEXT ===\n")
	b.WriteString("IMPORTANT: This context contains only file summaries and public function exports.\n")
	b.WriteString("NO full file contents are provided. Use the read_file tool to load specific files when needed.\n\n")

	// Add Project Insights (compact)
	if (workspace.ProjectInsights != ProjectInsights{}) {
		b.WriteString("Insights: ")
		parts := []string{}
		appendIf := func(name, val string) {
			if strings.TrimSpace(val) != "" {
				parts = append(parts, fmt.Sprintf("%s=%s", name, val))
			}
		}
		appendIf("frameworks", workspace.ProjectInsights.PrimaryFrameworks)
		appendIf("ci", workspace.ProjectInsights.CIProviders)
		appendIf("pkg", workspace.ProjectInsights.PackageManagers)
		appendIf("runtime", workspace.ProjectInsights.RuntimeTargets)
		appendIf("deploy", workspace.ProjectInsights.DeploymentTargets)
		appendIf("monorepo", workspace.ProjectInsights.Monorepo)
		appendIf("layout", workspace.ProjectInsights.RepoLayout)
		if len(parts) > 0 {
			b.WriteString(strings.Join(parts, "; "))
		}
		b.WriteString("\n\n")
	}

	// Add Git Repository Information (minimal)
	remoteURL, err := git.GetGitRemoteURL()
	if err == nil && remoteURL != "" {
		b.WriteString(fmt.Sprintf("Git Remote: %s\n", remoteURL))
	}

	branch, uncommitted, staged, statusErr := git.GetGitStatus()
	if statusErr == nil {
		b.WriteString(fmt.Sprintf("Git Status: Branch=%s, Uncommitted=%d, Staged=%d\n", branch, uncommitted, staged))
	}
	b.WriteString("\n")

	// Add Project Goals (if available)
	if workspace.ProjectGoals.OverallGoal != "" {
		b.WriteString("=== PROJECT GOALS ===\n")
		if workspace.ProjectGoals.OverallGoal != "" {
			b.WriteString(fmt.Sprintf("Goal: %s\n", workspace.ProjectGoals.OverallGoal))
		}
		if workspace.ProjectGoals.KeyFeatures != "" {
			b.WriteString(fmt.Sprintf("Features: %s\n", workspace.ProjectGoals.KeyFeatures))
		}
		b.WriteString("\n")
	}

	// Build minimal file structure with summaries and exports
	b.WriteString("=== FILE STRUCTURE WITH SUMMARIES AND EXPORTS ===\n")
	b.WriteString("Use this information to identify which files to read with read_file tool.\n\n")

	// Sort files for consistent output
	var sortedFiles []string
	for filePath := range workspace.Files {
		sortedFiles = append(sortedFiles, filePath)
	}
	sort.Strings(sortedFiles)

	for _, filePath := range sortedFiles {
		fileInfo := workspace.Files[filePath]

		b.WriteString(fmt.Sprintf("📁 %s\n", filePath))

		// Add summary (critical for understanding what the file does)
		if fileInfo.Summary != "" && fileInfo.Summary != "File is too large to analyze." {
			b.WriteString(fmt.Sprintf("   Summary: %s\n", fileInfo.Summary))
		} else if fileInfo.Summary == "File is too large to analyze." {
			b.WriteString("   Summary: Large file - use read_file with offset/limit if needed\n")
		}

		// Add exports (critical for understanding available functions)
		if fileInfo.Exports != "" && fileInfo.Exports != "None" {
			b.WriteString(fmt.Sprintf("   Public Functions: %s\n", fileInfo.Exports))
		}

		// Add references if available (shows dependencies)
		if fileInfo.References != "" {
			b.WriteString(fmt.Sprintf("   Uses: %s\n", fileInfo.References))
		}

		b.WriteString("\n")
	}

	b.WriteString("=== INSTRUCTIONS FOR LLM ===\n")
	b.WriteString("1. Use the summaries and exports above to identify relevant files\n")
	b.WriteString("2. Use read_file tool to load ONLY the specific files you need to understand\n")
	b.WriteString("3. Focus on making minimal changes - prefer modifying existing functions over creating new ones\n")
	b.WriteString("4. When changing model usage, look for assignments like 'modelName := cfg.OrchestrationModel'\n")
	b.WriteString("5. Make the smallest change that solves the specific problem described\n\n")

	// Log the full minimal context for debugging
	logger.Log(b.String())

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
