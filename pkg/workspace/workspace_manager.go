package workspace

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/security"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
)

// processResult is used to pass analysis results from goroutines back to the main thread.
type processResult struct {
	relativePath            string
	summary                 string
	exports                 string
	hash                    string
	references              string
	tokenCount              int
	securityConcerns        []string // kept for compatibility; will remain empty
	ignoredSecurityConcerns []string // kept for compatibility; will remain empty
	err                     error
}

// fileToProcess holds information about a file that needs to be analyzed locally.
type fileToProcess struct {
	path         string
	relativePath string
	content      string
	hash         string
}

var (
	textExtensions = map[string]bool{
		".txt": true, ".go": true, ".py": true, ".js": true, ".jsx": true, ".java": true,
		".c": true, ".cpp": true, ".h": true, ".hpp": true, ".md": true,
		".json": true, ".yaml": true, ".yml": true, ".sh": true, ".bash": true,
		".sql": true, ".html": true, ".css": true, ".xml": true, ".csv": true,
		".ts": true, ".tsx": true, ".php": true, ".rb": true, ".swift": true,
		".kt": true, ".scala": true, ".rs": true, ".dart": true, ".pl": true,
		".pm": true, ".lua": true, ".vim": true, ".toml": true,
	}
)

// buildSyntacticOverview creates a compact, deterministic overview string for LLM context.
func buildSyntacticOverview(ws WorkspaceFile) string {
	var b strings.Builder
	b.WriteString("Languages: ")
	b.WriteString(strings.Join(ws.Languages, ", "))
	b.WriteString("\n")
	if ws.BuildCommand != "" {
		b.WriteString(fmt.Sprintf("Build: %s\n", ws.BuildCommand))
	}
	if ws.TestCommand != "" {
		b.WriteString(fmt.Sprintf("Test: %s\n", ws.TestCommand))
	}
	if len(ws.BuildRunners) > 0 {
		b.WriteString(fmt.Sprintf("Build runners: %s\n", strings.Join(ws.BuildRunners, ", ")))
	}
	if len(ws.TestRunnerPaths) > 0 {
		b.WriteString(fmt.Sprintf("Test configs: %s\n", strings.Join(ws.TestRunnerPaths, ", ")))
	}
	// Include any existing insights succinctly
	if (ws.ProjectInsights != ProjectInsights{}) {
		b.WriteString("Insights: ")
		parts := []string{}
		appendIf := func(name, val string) {
			if strings.TrimSpace(val) != "" {
				parts = append(parts, fmt.Sprintf("%s=%s", name, val))
			}
		}
		appendIf("frameworks", ws.ProjectInsights.PrimaryFrameworks)
		appendIf("ci", ws.ProjectInsights.CIProviders)
		appendIf("pkg", ws.ProjectInsights.PackageManagers)
		appendIf("runtime", ws.ProjectInsights.RuntimeTargets)
		appendIf("deploy", ws.ProjectInsights.DeploymentTargets)
		appendIf("monorepo", ws.ProjectInsights.Monorepo)
		appendIf("layout", ws.ProjectInsights.RepoLayout)
		if len(parts) > 0 {
			b.WriteString(strings.Join(parts, "; "))
			b.WriteString("\n")
		}
	}
	b.WriteString("\nFiles (path, overview, exports, references):\n")
	const maxFiles = 400
	var files []string
	for p := range ws.Files {
		files = append(files, p)
	}
	sort.Strings(files)
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	for _, p := range files {
		fi := ws.Files[p]
		b.WriteString(p)
		b.WriteString("\n  overview: ")
		b.WriteString(fi.Summary)
		if strings.TrimSpace(fi.Exports) != "" {
			b.WriteString("\n  exports: ")
			b.WriteString(fi.Exports)
		}
		if strings.TrimSpace(fi.References) != "" {
			b.WriteString("\n  references: ")
			b.WriteString(fi.References)
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

// detectProjectInsightsHeuristics scans the repo to infer insights without LLM.
func detectProjectInsightsHeuristics(rootDir string, ws WorkspaceFile) ProjectInsights {
	ins := ProjectInsights{}

	// Monorepo heuristics
	if exists(filepath.Join(rootDir, "pnpm-workspace.yaml")) || exists(filepath.Join(rootDir, "pnpm-workspace.yml")) ||
		exists(filepath.Join(rootDir, "lerna.json")) || exists(filepath.Join(rootDir, "nx.json")) ||
		exists(filepath.Join(rootDir, "turbo.json")) || exists(filepath.Join(rootDir, "go.work")) {
		ins.Monorepo = "yes"
	} else {
		// multiple package.json or go.mod in subdirs
		pkgCount := 0
		gomodCount := 0
		filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			base := filepath.Base(path)
			if base == "package.json" {
				pkgCount++
			}
			if base == "go.mod" {
				gomodCount++
			}
			return nil
		})
		if pkgCount > 1 || gomodCount > 1 {
			ins.Monorepo = "yes"
		} else {
			ins.Monorepo = "no"
		}
	}

	// CI providers
	ci := []string{}
	if exists(filepath.Join(rootDir, ".github", "workflows")) {
		ci = append(ci, "GitHub Actions")
	}
	if exists(filepath.Join(rootDir, ".gitlab-ci.yml")) {
		ci = append(ci, "GitLab CI")
	}
	if exists(filepath.Join(rootDir, ".circleci", "config.yml")) {
		ci = append(ci, "CircleCI")
	}
	if exists(filepath.Join(rootDir, ".azure-pipelines.yml")) {
		ci = append(ci, "Azure Pipelines")
	}
	if exists(filepath.Join(rootDir, ".drone.yml")) {
		ci = append(ci, "Drone")
	}
	if exists(filepath.Join(rootDir, ".travis.yml")) {
		ci = append(ci, "TravisCI")
	}
	ins.CIProviders = strings.Join(ci, ", ")

	// Package managers
	pm := []string{}
	if exists(filepath.Join(rootDir, "package-lock.json")) {
		pm = append(pm, "npm")
	}
	if exists(filepath.Join(rootDir, "yarn.lock")) {
		pm = append(pm, "yarn")
	}
	if exists(filepath.Join(rootDir, "pnpm-lock.yaml")) {
		pm = append(pm, "pnpm")
	}
	if exists(filepath.Join(rootDir, "go.mod")) {
		pm = append(pm, "go modules")
	}
	if exists(filepath.Join(rootDir, "requirements.txt")) || exists(filepath.Join(rootDir, "Pipfile")) || exists(filepath.Join(rootDir, "poetry.lock")) || exists(filepath.Join(rootDir, "pyproject.toml")) {
		pm = append(pm, "pip/poetry")
	}
	if exists(filepath.Join(rootDir, "Cargo.toml")) {
		pm = append(pm, "cargo")
	}
	if exists(filepath.Join(rootDir, "Gemfile")) {
		pm = append(pm, "bundler")
	}
	ins.PackageManagers = strings.Join(pm, ", ")

	// Runtime targets based on languages
	rts := []string{}
	langset := map[string]bool{}
	for _, l := range ws.Languages {
		langset[l] = true
	}
	if langset["javascript"] || langset["typescript"] {
		rts = append(rts, "Node.js", "Browser")
	}
	if langset["python"] {
		rts = append(rts, "Python")
	}
	if langset["java"] || langset["kotlin"] {
		rts = append(rts, "JVM")
	}
	if langset["go"] {
		rts = append(rts, "Go")
	}
	if langset["rust"] {
		rts = append(rts, "Rust")
	}
	ins.RuntimeTargets = strings.Join(uniqueStrings(rts), ", ")

	// Deployment targets
	dt := []string{}
	if exists(filepath.Join(rootDir, "Dockerfile")) || exists(filepath.Join(rootDir, "docker-compose.yml")) || exists(filepath.Join(rootDir, "docker-compose.yaml")) {
		dt = append(dt, "Docker")
	}
	// Kubernetes manifests
	k8s := false
	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if strings.Contains(strings.ToLower(base), "deployment.yaml") || strings.Contains(strings.ToLower(base), "deployment.yml") || strings.Contains(strings.ToLower(base), "kustomization.yaml") {
			k8s = true
		}
		return nil
	})
	if k8s {
		dt = append(dt, "Kubernetes")
	}
	if exists(filepath.Join(rootDir, "serverless.yml")) || exists(filepath.Join(rootDir, "serverless.yaml")) {
		dt = append(dt, "Serverless")
	}
	if exists(filepath.Join(rootDir, "main.tf")) || exists(filepath.Join(rootDir, "terraform")) {
		dt = append(dt, "Terraform")
	}
	ins.DeploymentTargets = strings.Join(uniqueStrings(dt), ", ")

	// Repo layout
	layouts := []string{}
	if exists(filepath.Join(rootDir, "apps")) && exists(filepath.Join(rootDir, "packages")) {
		layouts = append(layouts, "apps+packages")
	}
	if exists(filepath.Join(rootDir, "cmd")) {
		layouts = append(layouts, "cmd/")
	}
	if exists(filepath.Join(rootDir, "internal")) {
		layouts = append(layouts, "internal/")
	}
	if exists(filepath.Join(rootDir, "src")) {
		layouts = append(layouts, "src/")
	}
	ins.RepoLayout = strings.Join(layouts, ", ")

	// Build system and test strategy
	bs := []string{}
	if exists(filepath.Join(rootDir, "Makefile")) {
		bs = append(bs, "make")
	}
	if exists(filepath.Join(rootDir, "justfile")) {
		bs = append(bs, "just")
	}
	if exists(filepath.Join(rootDir, "Taskfile.yml")) || exists(filepath.Join(rootDir, "Taskfile.yaml")) {
		bs = append(bs, "task")
	}
	if exists(filepath.Join(rootDir, "package.json")) {
		bs = append(bs, "npm scripts")
	}
	if exists(filepath.Join(rootDir, "build.gradle")) || exists(filepath.Join(rootDir, "pom.xml")) {
		bs = append(bs, "gradle/maven")
	}
	if exists(filepath.Join(rootDir, "Cargo.toml")) {
		bs = append(bs, "cargo")
	}
	ins.BuildSystem = strings.Join(uniqueStrings(bs), ", ")

	ts := []string{}
	if exists(filepath.Join(rootDir, "jest.config.js")) || exists(filepath.Join(rootDir, "jest.config.ts")) {
		ts = append(ts, "jest")
	}
	if exists(filepath.Join(rootDir, "vitest.config.ts")) || exists(filepath.Join(rootDir, "vitest.config.js")) {
		ts = append(ts, "vitest")
	}
	if exists(filepath.Join(rootDir, "pytest.ini")) {
		ts = append(ts, "pytest")
	}
	if exists(filepath.Join(rootDir, "go.mod")) {
		ts = append(ts, "go test")
	}
	if exists(filepath.Join(rootDir, "Cargo.toml")) {
		ts = append(ts, "cargo test")
	}
	ins.TestStrategy = strings.Join(uniqueStrings(ts), ", ")

	// Primary frameworks / key dependencies via package.json
	pkgs := map[string]struct{}{}
	pkgPath := filepath.Join(rootDir, "package.json")
	if exists(pkgPath) {
		var pkg map[string]any
		if b, err := os.ReadFile(pkgPath); err == nil {
			_ = json.Unmarshal(b, &pkg)
			for _, k := range []string{"dependencies", "devDependencies"} {
				if m, ok := pkg[k].(map[string]any); ok {
					for name := range m {
						pkgs[name] = struct{}{}
					}
				}
			}
		}
	}
	fw := []string{}
	a := []string{}
	addIf := func(dep string, label string) {
		if _, ok := pkgs[dep]; ok {
			fw = append(fw, label)
		}
	}
	addIf("react", "React")
	addIf("next", "Next.js")
	addIf("vue", "Vue")
	addIf("nuxt", "Nuxt")
	addIf("@angular/core", "Angular")
	addIf("svelte", "Svelte")
	addIf("express", "Express")
	addIf("koa", "Koa")
	addIf("nestjs", "NestJS")
	addIf("fastify", "Fastify")
	// Build tools
	addIf("vite", "Vite")
	addIf("webpack", "Webpack")
	addIf("rollup", "Rollup")
	// Testing
	addIf("jest", "Jest")
	addIf("vitest", "Vitest")
	ins.PrimaryFrameworks = strings.Join(uniqueStrings(fw), ", ")
	// Key deps: show top frameworks/build tools we found
	key := append([]string{}, fw...)
	key = append(key, a...)
	key = append(key, intersectKeys(pkgs, []string{"axios", "redux", "react-router", "rxjs", "lodash"})...)
	ins.KeyDependencies = strings.Join(uniqueStrings(key), ", ")

	return ins
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if s == "" {
			continue
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func intersectKeys(m map[string]struct{}, candidates []string) []string {
	out := []string{}
	for _, c := range candidates {
		if _, ok := m[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// detectBuildCommand attempts to autogenerate a build command based on project type.
// It checks for Go projects (presence of .go files) and Node.js projects (presence of package.json).
func detectBuildCommand(rootDir string) string {
	// Check for Go project
	goFilesFound := false
	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip common ignored directories
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "build" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
			goFilesFound = true
			return fmt.Errorf("found go file") // Use a custom error to stop walking
		}
		return nil
	})
	if goFilesFound {
		return "go build ."
	}

	// Check for JavaScript/Node.js project
	packageJSONPath := filepath.Join(rootDir, "package.json")
	if _, err := os.Stat(packageJSONPath); err == nil {
		content, err := os.ReadFile(packageJSONPath)
		if err != nil {
			return "" // Cannot read package.json
		}

		var pkgJSON map[string]interface{}
		if err := json.Unmarshal(content, &pkgJSON); err != nil {
			return "" // Cannot parse package.json
		}

		if scripts, ok := pkgJSON["scripts"].(map[string]interface{}); ok {
			if _, hasBuild := scripts["build"]; hasBuild {
				return "npm run build"
			}
			if _, hasStart := scripts["start"]; hasStart {
				return "npm start"
			}
		}
	}

	return "" // Cannot determine build command
}

// validateAndUpdateWorkspace checks the current file system against the workspace.json file,
// analyzes new or changed files, removes deleted files, and saves the updated workspace.
func validateAndUpdateWorkspace(rootDir string, cfg *config.Config) (WorkspaceFile, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	workspace, err := LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.LogProcessStep("No existing workspace file found. Creating a new one.")
			workspace = WorkspaceFile{Files: make(map[string]WorkspaceFileInfo)}
		} else {
			return WorkspaceFile{}, fmt.Errorf("failed to load workspace file: %w", err)
		}
	}

	currentFiles := make(map[string]bool)
	ignoreRules := GetIgnoreRules(rootDir)

	var filesToAnalyzeList []fileToProcess
	newFilesCount := 0
	newFilesTopDirs := make(map[string]int) // Map to store count of new files per top-level directory

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relativePath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		if ignoreRules != nil && ignoreRules.MatchesPath(relativePath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// More robust file type checking
		ext := strings.ToLower(filepath.Ext(path))
		if !textExtensions[ext] {
			return nil
		}

		currentFiles[relativePath] = true

		content, err := os.ReadFile(path)
		if err != nil {
			logger.Logf("Warning: could not read file %s: %v. Skipping.\n", path, err)
			return nil
		}

		fileContent := string(content)
		newHash := generateFileHash(fileContent)

		existingFileInfo, exists := workspace.Files[relativePath]
		isChanged := exists && existingFileInfo.Hash != newHash
		isNew := !exists

		// Always analyze new or changed files locally
		if !isNew && !isChanged {
			return nil
		}

		filesToAnalyzeList = append(filesToAnalyzeList, fileToProcess{
			path:         path,
			relativePath: relativePath,
			content:      fileContent,
			hash:         newHash,
		})

		if isNew {
			newFilesCount++
			// Determine top-level directory
			parts := strings.Split(relativePath, string(os.PathSeparator))
			if len(parts) > 0 {
				topDir := parts[0]
				newFilesTopDirs[topDir]++
			}
		}

		return nil
	})

	if err != nil {
		return workspace, err
	}

	// --- Warning and Confirmation for too many new files ---
	if newFilesCount > 500 {
		var topDirsList []string
		for dir := range newFilesTopDirs {
			topDirsList = append(topDirsList, dir)
		}
		sort.Strings(topDirsList) // Sort for consistent output

		var topDirsMessage strings.Builder
		topDirsMessage.WriteString("The following top-level directories contain new files:\n")
		for _, dir := range topDirsList {
			topDirsMessage.WriteString(fmt.Sprintf("  - %s (%d new files)\n", dir, newFilesTopDirs[dir]))
		}

		warningMessage := fmt.Sprintf(
			"WARNING: %d new files have been detected in your workspace.\n"+
				"This might indicate that a large directory (e.g., node_modules, build) is not being correctly ignored.\n"+
				"%s\n"+
				"Do you want to proceed with analyzing these new files? (This may take a long time)",
			newFilesCount, topDirsMessage.String(),
		)

		// Make confirmation non-required so it defaults to 'true' in non-interactive mode
		if !logger.AskForConfirmation(warningMessage, true, false) { // non-required confirmation, defaults to true
			return WorkspaceFile{}, fmt.Errorf("workspace update cancelled by user due to too many new files")
		}
	}
	// --- End of Warning and Confirmation ---

	if len(filesToAnalyzeList) > 0 {
		logger.LogProcessStep(fmt.Sprintf("Waiting for analysis of %d files to complete...", len(filesToAnalyzeList)))
	}

	// Process files in batches to avoid pressure
	batchSize := cfg.FileBatchSize
	if batchSize <= 0 {
		batchSize = 10 // Default fallback
	}

	var allResults []processResult

	for i := 0; i < len(filesToAnalyzeList); i += batchSize {
		end := i + batchSize
		if end > len(filesToAnalyzeList) {
			end = len(filesToAnalyzeList)
		}
		batch := filesToAnalyzeList[i:end]

		logger.LogProcessStep(fmt.Sprintf("Processing batch %d/%d (%d files)...", (i/batchSize)+1, (len(filesToAnalyzeList)+batchSize-1)/batchSize, len(batch)))

		var wg sync.WaitGroup
		resultsChan := make(chan processResult, len(batch))

		// Limit concurrency within each batch
		maxConcurrent := cfg.MaxConcurrentRequests
		if maxConcurrent <= 0 {
			maxConcurrent = 3 // Default fallback
		}
		sem := make(chan struct{}, maxConcurrent)

		for _, file := range batch {
			wg.Add(1)
			go func(f fileToProcess, cfg *config.Config) {
				defer wg.Done()

				// Acquire semaphore
				sem <- struct{}{}
				defer func() { <-sem }()

				var fileSummary, fileExports, fileReferences string
				var llmErr error

				if len(f.content) > 0 {
					logger.Logf("Analyzing %s for workspace (local syntactic overview)...", f.path)
					fileSummary, fileExports, fileReferences, llmErr = getSummary(f.content, f.path, cfg)
				}

				// Perform security checks if enabled
				finalSecurityConcerns := []string{}
				finalIgnoredSecurityConcerns := []string{}

				if cfg.EnableSecurityChecks && len(f.content) > 0 {
					// Import the security package
					// We need to perform security checks on the file content
					securityConcerns, ignoredSecurityConcerns, skipLLMSummarization := security.CheckFileSecurity(
						f.relativePath,
						f.content,
						true,       // isNew - assume new for security checks
						true,       // isChanged - assume changed for security checks
						[]string{}, // existingSecurityConcerns - none for local analysis
						[]string{}, // existingIgnoredSecurityConcerns - none for local analysis
						cfg,
					)

					finalSecurityConcerns = securityConcerns
					finalIgnoredSecurityConcerns = ignoredSecurityConcerns

					// If security concerns found and local summarization should be skipped
					if skipLLMSummarization && len(securityConcerns) > 0 {
						// Clear the summary to prevent processing of sensitive content
						fileSummary = ""
						fileExports = ""
						fileReferences = ""
						logger.LogProcessStep(fmt.Sprintf("Skipped local summarization for %s due to security concerns", f.relativePath))
					}
				}

				resultsChan <- processResult{
					relativePath:            f.relativePath,
					summary:                 fileSummary,
					exports:                 fileExports,
					references:              fileReferences,
					hash:                    f.hash,
					tokenCount:              0,
					securityConcerns:        finalSecurityConcerns,
					ignoredSecurityConcerns: finalIgnoredSecurityConcerns,
					err:                     llmErr,
				}
			}(file, cfg)
		}

		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Collect results from this batch
		for result := range resultsChan {
			allResults = append(allResults, result)
		}

		// Add delay between batches to avoid pressure
		if cfg.RequestDelayMs > 0 && i+batchSize < len(filesToAnalyzeList) {
			logger.LogProcessStep(fmt.Sprintf("Waiting %dms before next batch...", cfg.RequestDelayMs))
			time.Sleep(time.Duration(cfg.RequestDelayMs) * time.Millisecond)
		}
	}

	// Process all collected results
	for _, result := range allResults {
		if result.err != nil {
			logger.Logf("Warning: could not analyze file %s: %v. Proceeding with empty summary/exports.\n", result.relativePath, result.err)
		}
		workspace.Files[result.relativePath] = WorkspaceFileInfo{
			Hash:                    result.hash,
			Summary:                 result.summary,
			Exports:                 result.exports,
			References:              result.references,
			TokenCount:              result.tokenCount,
			SecurityConcerns:        result.securityConcerns,
			IgnoredSecurityConcerns: result.ignoredSecurityConcerns,
		}
	}

	for filePath := range workspace.Files {
		if _, exists := currentFiles[filePath]; !exists {
			logger.LogProcessStep(fmt.Sprintf("File %s has been removed. Removing from workspace...", filePath))
			delete(workspace.Files, filePath)
		}
	}

	// Detect and cache simple workspace context
	detectWorkspaceContext(&workspace, rootDir, logger)

	if err := saveWorkspaceFile(workspace); err != nil {
		return workspace, err
	}

	return workspace, nil
}

// detectWorkspaceContext populates lightweight workspace context fields
func detectWorkspaceContext(workspace *WorkspaceFile, rootDir string, logger *utils.Logger) {
	langs := detectLanguages(rootDir)
	if len(langs) > 0 {
		workspace.Languages = langs
	}
	if workspace.BuildCommand == "" {
		logger.LogProcessStep("--- Attempting to autogenerate build command ---")
		if bc := detectBuildCommand(rootDir); bc != "" {
			workspace.BuildCommand = bc
			logger.LogProcessStep(fmt.Sprintf("--- Autogenerated build command: '%s' ---", bc))
		} else {
			logger.LogProcessStep("--- Could not autogenerate build command. Will attempt again next time. ---")
		}
	}
	if workspace.TestCommand == "" {
		if tc := detectTestCommand(rootDir); tc != "" {
			workspace.TestCommand = tc
			logger.LogProcessStep(fmt.Sprintf("--- Detected test command: '%s' ---", tc))
		}
	}
	br, tr := detectRunnerPaths(rootDir)
	if len(br) > 0 {
		workspace.BuildRunners = br
	}
	if len(tr) > 0 {
		workspace.TestRunnerPaths = tr
	}
}

func detectLanguages(rootDir string) []string {
	seen := map[string]bool{}
	langs := []string{}
	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "build" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		lang := ""
		switch ext {
		case ".go":
			lang = "go"
		case ".ts", ".tsx":
			lang = "typescript"
		case ".js", ".jsx":
			lang = "javascript"
		case ".py":
			lang = "python"
		case ".rb":
			lang = "ruby"
		case ".rs":
			lang = "rust"
		case ".java":
			lang = "java"
		case ".kt":
			lang = "kotlin"
		case ".cs":
			lang = "csharp"
		case ".php":
			lang = "php"
		case ".scala":
			lang = "scala"
		case ".swift":
			lang = "swift"
		}
		if lang != "" && !seen[lang] {
			seen[lang] = true
			langs = append(langs, lang)
		}
		return nil
	})
	sort.Strings(langs)
	return langs
}

func detectTestCommand(rootDir string) string {
	// Heuristics based on common ecosystem markers
	if exists(filepath.Join(rootDir, "go.mod")) {
		return "go test ./..."
	}
	if exists(filepath.Join(rootDir, "package.json")) {
		// Prefer npm test if script exists
		b, err := os.ReadFile(filepath.Join(rootDir, "package.json"))
		if err == nil {
			var pkg map[string]any
			if json.Unmarshal(b, &pkg) == nil {
				if s, ok := pkg["scripts"].(map[string]any); ok {
					if _, ok := s["test"]; ok {
						return "npm test --silent"
					}
				}
			}
		}
		return "npm test --silent"
	}
	if exists(filepath.Join(rootDir, "pyproject.toml")) || exists(filepath.Join(rootDir, "pytest.ini")) {
		return "pytest -q"
	}
	if exists(filepath.Join(rootDir, "Cargo.toml")) {
		return "cargo test"
	}
	return ""
}

func detectRunnerPaths(rootDir string) ([]string, []string) {
	var buildRunners, testRunners []string
	// Common build runners
	candidates := []string{"Makefile", "justfile", "Taskfile.yml", "Taskfile.yaml"}
	for _, c := range candidates {
		p := filepath.Join(rootDir, c)
		if exists(p) {
			buildRunners = append(buildRunners, c)
		}
	}
	// Common test configs
	tests := []string{"pytest.ini", "jest.config.js", "jest.config.ts", "vitest.config.ts", "vitest.config.js"}
	for _, c := range tests {
		p := filepath.Join(rootDir, c)
		if exists(p) {
			testRunners = append(testRunners, c)
		}
	}
	sort.Strings(buildRunners)
	sort.Strings(testRunners)
	return buildRunners, testRunners
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// GetWorkspaceContext orchestrates the workspace loading, analysis, and context generation process.
func GetWorkspaceContext(instructions string, cfg *config.Config) string {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("--- Loading in workspace data ---")
	// UI shimmer: workspace context building
	ui.PublishStatus("Building workspace context (files, syntactic overviews)…")
	workspaceFilePath := "./.ledit/workspace.json"

	if err := os.MkdirAll(filepath.Dir(workspaceFilePath), os.ModePerm); err != nil {
		logger.Logf("Error creating .ledit directory for WORKSPACE: %v. Continuing without it.\n", err)
		return ""
	}

	workspace, err := validateAndUpdateWorkspace("./", cfg)
	if err != nil {
		logger.Logf("Error loading/updating content from WORKSPACE: %v. Continuing without it.\n", err)
		return ""
	}

	// Seed insights from heuristics
	heur := detectProjectInsightsHeuristics("./", workspace)
	mergeInsights := func(dst *ProjectInsights, src ProjectInsights) {
		if dst.PrimaryFrameworks == "" {
			dst.PrimaryFrameworks = src.PrimaryFrameworks
		}
		if dst.KeyDependencies == "" {
			dst.KeyDependencies = src.KeyDependencies
		}
		if dst.BuildSystem == "" {
			dst.BuildSystem = src.BuildSystem
		}
		if dst.TestStrategy == "" {
			dst.TestStrategy = src.TestStrategy
		}
		if dst.Architecture == "" {
			dst.Architecture = src.Architecture
		}
		if dst.Monorepo == "" || dst.Monorepo == "unknown" {
			dst.Monorepo = src.Monorepo
		}
		if dst.CIProviders == "" {
			dst.CIProviders = src.CIProviders
		}
		if dst.RuntimeTargets == "" {
			dst.RuntimeTargets = src.RuntimeTargets
		}
		if dst.DeploymentTargets == "" {
			dst.DeploymentTargets = src.DeploymentTargets
		}
		if dst.PackageManagers == "" {
			dst.PackageManagers = src.PackageManagers
		}
		if dst.RepoLayout == "" {
			dst.RepoLayout = src.RepoLayout
		}
	}
	mergeInsights(&workspace.ProjectInsights, heur)

	// Autogenerate Project Goals/Insights if empty using syntactic overview
	overview := buildSyntacticOverview(workspace)
	if (workspace.ProjectGoals == ProjectGoals{}) {
		logger.LogProcessStep("--- Autogenerating project goals from syntactic overview ---")
		generatedGoals, goalErr := GetProjectGoals(cfg, overview)
		if goalErr != nil {
			logger.Logf("Warning: Failed to autogenerate project goals: %v.\n", goalErr)
		} else {
			workspace.ProjectGoals = generatedGoals
			// Cache a baseline hash for change detection
			if workspace.GoalsBaseline == nil {
				workspace.GoalsBaseline = map[string]string{}
			}
			workspace.GoalsBaseline["syntactic_overview_hash"] = generateFileHash(overview)
		}
	}
	if (workspace.ProjectInsights == ProjectInsights{}) { // regenerate only when empty (or later heuristic)
		logger.LogProcessStep("--- Autogenerating project insights from syntactic overview ---")
		generatedInsights, insErr := GetProjectInsights(cfg, overview)
		if insErr != nil {
			logger.Logf("Warning: Failed to autogenerate project insights: %v.\n", insErr)
		} else {
			// prefer LLM, but keep heuristic values when LLM leaves fields empty
			mergeInsights(&generatedInsights, workspace.ProjectInsights)
			workspace.ProjectInsights = generatedInsights
			// Cache a baseline hash for change detection
			if workspace.InsightsBaseline == nil {
				workspace.InsightsBaseline = map[string]string{}
			}
			workspace.InsightsBaseline["syntactic_overview_hash"] = generateFileHash(overview)
		}
	}
	if err := saveWorkspaceFile(workspace); err != nil {
		logger.Logf("Warning: Failed to save workspace metadata: %v\n", err)
	}

	// Use embedding-based file selection by default
	var fullContextFiles, summaryContextFiles []string
	var fileSelectionErr error

	logger.LogProcessStep("--- Using embedding-based file selection ---")
	ui.PublishStatus("Selecting relevant files via embeddings…")
	fullContextFiles, summaryContextFiles, fileSelectionErr = GetFilesForContextUsingEmbeddings(instructions, workspace, cfg, logger)
	if fileSelectionErr != nil {
		logger.Logf("Warning: could not determine which files to load for context using embeddings: %v. Proceeding with all summaries.\n", fileSelectionErr)
		var allFilesAsSummaries []string
		for file := range workspace.Files {
			allFilesAsSummaries = append(allFilesAsSummaries, file)
		}
		return getWorkspaceInfo(workspace, nil, allFilesAsSummaries, workspace.ProjectGoals, cfg)
	}

	// Force summaries-only in the initial context so downstream flows must call read_file for full content
	if len(fullContextFiles) > 0 {
		logger.LogProcessStep("--- Forcing summaries-only initial context (full file content requires read_file tools) ---")
		fullContextFiles = nil
	}

	if len(fullContextFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("--- Selected the following files for full context: %s ---", strings.Join(fullContextFiles, ", ")))
	}
	if len(summaryContextFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("--- Selected the following files for summary context: %s ---", strings.Join(summaryContextFiles, ", ")))
	}
	if len(fullContextFiles) == 0 && len(summaryContextFiles) == 0 {
		logger.LogProcessStep("--- No files were selected as relevant for context. ---")
	}

	for _, file := range fullContextFiles {
		ui.PublishStatus("Reading selected files for full context…")
		fileInfo, exists := workspace.Files[file]
		if !exists {
			logger.Logf("Warning: file %s selected for full context not found in workspace. Skipping.\n", file)
			continue
		}
		if fileInfo.Summary == "File is too large to analyze." {
			logger.LogUserInteraction(fmt.Sprintf("----- ERROR!!! -----:\n\n The file %s is too large to include in full context. Please pass it directly if needed.\n", file))
			continue
		}
		if fileInfo.Summary == "Skipped due to confirmed security concerns." {
			logger.LogUserInteraction(fmt.Sprintf("----- WARNING!!! -----:\n\n The file %s was selected for full context but was skipped due to confirmed security concerns. Its content will not be provided to the LLM.\n", file))
			continue
		}
	}

	return getWorkspaceInfo(workspace, fullContextFiles, summaryContextFiles, workspace.ProjectGoals, cfg)
}
