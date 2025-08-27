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
	"github.com/alantheprice/ledit/pkg/filediscovery"
	"github.com/alantheprice/ledit/pkg/security"
	"github.com/alantheprice/ledit/pkg/text"
	ui "github.com/alantheprice/ledit/pkg/ui"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspaceinfo"
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
func buildSyntacticOverview(ws workspaceinfo.WorkspaceFile) string {
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
	if (ws.ProjectInsights != workspaceinfo.ProjectInsights{}) {
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
func detectProjectInsightsHeuristics(rootDir string, ws workspaceinfo.WorkspaceFile) workspaceinfo.ProjectInsights {
	ins := workspaceinfo.ProjectInsights{}

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
// detectMonorepoStructure analyzes the workspace to identify monorepo projects
func detectMonorepoStructure(rootDir string) (map[string]workspaceinfo.ProjectInfo, string) {
	projects := make(map[string]workspaceinfo.ProjectInfo)
	monorepoType := "single"

	// Common monorepo directory patterns
	commonProjectDirs := []string{
		"frontend", "backend", "api", "web", "server",
		"apps", "packages", "services", "libs", "shared", "common",
		"client", "admin", "dashboard", "mobile",
	}

	// Check for monorepo tool config files
	monorepoToolFiles := []string{
		"lerna.json", "rush.json", "nx.json", "pnpm-workspace.yaml",
		"workspace.json", "angular.json", ".yarnrc.yml",
	}

	var foundProjects []string
	hasMonorepoTools := false

	// Check for monorepo tools first
	for _, toolFile := range monorepoToolFiles {
		if _, err := os.Stat(filepath.Join(rootDir, toolFile)); err == nil {
			hasMonorepoTools = true
			monorepoType = "multi"
			break
		}
	}

	// Walk through directories to find projects
	filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}

		// Skip deep nesting and common ignore dirs
		relPath, _ := filepath.Rel(rootDir, path)
		depth := strings.Count(relPath, string(os.PathSeparator))
		if depth > 2 || shouldSkipDir(d.Name()) {
			return filepath.SkipDir
		}

		// Skip root directory
		if path == rootDir {
			return nil
		}

		project := analyzeProjectDirectory(path, rootDir)
		if project.Language != "" || project.Framework != "" {
			projectName := filepath.Base(path)
			projects[projectName] = project
			foundProjects = append(foundProjects, projectName)
		}

		return nil
	})

	// Determine monorepo type based on findings
	if len(foundProjects) > 1 || hasMonorepoTools {
		monorepoType = "multi"
	} else if len(foundProjects) == 1 {
		// Check if the single project is in a subdirectory suggesting monorepo structure
		for _, project := range projects {
			for _, commonDir := range commonProjectDirs {
				if strings.Contains(strings.ToLower(project.Path), commonDir) {
					monorepoType = "hybrid" // Single project but in monorepo-style structure
					break
				}
			}
		}
	}

	return projects, monorepoType
}

// analyzeProjectDirectory analyzes a directory to determine if it's a project and what type
func analyzeProjectDirectory(dirPath, rootDir string) workspaceinfo.ProjectInfo {
	relPath, _ := filepath.Rel(rootDir, dirPath)
	projectName := filepath.Base(dirPath)

	project := workspaceinfo.ProjectInfo{
		Path: relPath,
		Name: projectName,
	}

	// Check for different project types
	if hasFile(dirPath, "package.json") {
		project = analyzeNodeProject(dirPath, project)
	} else if hasFile(dirPath, "go.mod") {
		project = analyzeGoProject(dirPath, project)
	} else if hasFile(dirPath, "pyproject.toml") || hasFile(dirPath, "requirements.txt") || hasFile(dirPath, "setup.py") {
		project = analyzePythonProject(dirPath, project)
	} else if hasFile(dirPath, "Cargo.toml") {
		project = analyzeRustProject(dirPath, project)
	} else if hasFile(dirPath, "pom.xml") || hasFile(dirPath, "build.gradle") {
		project = analyzeJavaProject(dirPath, project)
	}

	// Infer project type from directory name and contents
	project.Type = inferProjectType(projectName, dirPath)

	return project
}

// Helper functions for project analysis
func analyzeNodeProject(dirPath string, project workspaceinfo.ProjectInfo) workspaceinfo.ProjectInfo {
	project.Language = "javascript"
	project.PackageManager = detectNodePackageManager(dirPath)

	// Read package.json to get more info
	packagePath := filepath.Join(dirPath, "package.json")
	if content, err := os.ReadFile(packagePath); err == nil {
		var pkg map[string]interface{}
		if json.Unmarshal(content, &pkg) == nil {
			// Detect framework
			if deps, ok := pkg["dependencies"].(map[string]interface{}); ok {
				project.Framework = detectJSFramework(deps)
				if strings.Contains(project.Framework, "typescript") {
					project.Language = "typescript"
				}
			}

			// Get commands
			if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
				if _, ok := scripts["build"]; ok {
					project.BuildCommand = fmt.Sprintf("cd %s && %s run build", project.Path, project.PackageManager)
				}
				if _, ok := scripts["test"]; ok {
					project.TestCommand = fmt.Sprintf("cd %s && %s run test", project.Path, project.PackageManager)
				}
				if _, ok := scripts["dev"]; ok {
					project.DevCommand = fmt.Sprintf("cd %s && %s run dev", project.Path, project.PackageManager)
				} else if _, ok := scripts["start"]; ok {
					project.DevCommand = fmt.Sprintf("cd %s && %s run start", project.Path, project.PackageManager)
				}
			}

			// Get entry points
			if main, ok := pkg["main"].(string); ok {
				project.EntryPoints = append(project.EntryPoints, main)
			}
		}
	}

	project.ConfigFiles = findConfigFiles(dirPath, []string{"package.json", "tsconfig.json", "vite.config.*", "webpack.config.*", "next.config.*"})
	return project
}

func analyzeGoProject(dirPath string, project workspaceinfo.ProjectInfo) workspaceinfo.ProjectInfo {
	project.Language = "go"
	project.PackageManager = "go mod"
	project.BuildCommand = fmt.Sprintf("cd %s && go build", project.Path)
	project.TestCommand = fmt.Sprintf("cd %s && go test ./...", project.Path)

	// Detect Go framework
	if goMod, err := os.ReadFile(filepath.Join(dirPath, "go.mod")); err == nil {
		content := string(goMod)
		if strings.Contains(content, "github.com/gin-gonic/gin") {
			project.Framework = "gin"
		} else if strings.Contains(content, "github.com/labstack/echo") {
			project.Framework = "echo"
		} else if strings.Contains(content, "github.com/gorilla/mux") {
			project.Framework = "gorilla/mux"
		}
	}

	project.ConfigFiles = findConfigFiles(dirPath, []string{"go.mod", "go.sum"})
	return project
}

func analyzePythonProject(dirPath string, project workspaceinfo.ProjectInfo) workspaceinfo.ProjectInfo {
	project.Language = "python"

	if hasFile(dirPath, "pyproject.toml") {
		project.PackageManager = "pip" // Could be poetry, but pip is universal
		project.BuildCommand = fmt.Sprintf("cd %s && python -m build", project.Path)
	} else if hasFile(dirPath, "setup.py") {
		project.PackageManager = "pip"
		project.BuildCommand = fmt.Sprintf("cd %s && python setup.py build", project.Path)
	}

	project.TestCommand = fmt.Sprintf("cd %s && python -m pytest", project.Path)

	// Detect Python framework
	if requirementsPath := filepath.Join(dirPath, "requirements.txt"); hasFile(dirPath, "requirements.txt") {
		if content, err := os.ReadFile(requirementsPath); err == nil {
			reqs := string(content)
			if strings.Contains(reqs, "fastapi") {
				project.Framework = "fastapi"
			} else if strings.Contains(reqs, "flask") {
				project.Framework = "flask"
			} else if strings.Contains(reqs, "django") {
				project.Framework = "django"
			}
		}
	}

	project.ConfigFiles = findConfigFiles(dirPath, []string{"pyproject.toml", "requirements.txt", "setup.py", "setup.cfg"})
	return project
}

func analyzeRustProject(dirPath string, project workspaceinfo.ProjectInfo) workspaceinfo.ProjectInfo {
	project.Language = "rust"
	project.PackageManager = "cargo"
	project.BuildCommand = fmt.Sprintf("cd %s && cargo build", project.Path)
	project.TestCommand = fmt.Sprintf("cd %s && cargo test", project.Path)
	project.ConfigFiles = findConfigFiles(dirPath, []string{"Cargo.toml", "Cargo.lock"})
	return project
}

func analyzeJavaProject(dirPath string, project workspaceinfo.ProjectInfo) workspaceinfo.ProjectInfo {
	project.Language = "java"

	if hasFile(dirPath, "pom.xml") {
		project.PackageManager = "maven"
		project.BuildCommand = fmt.Sprintf("cd %s && mvn compile", project.Path)
		project.TestCommand = fmt.Sprintf("cd %s && mvn test", project.Path)
		project.ConfigFiles = append(project.ConfigFiles, "pom.xml")
	} else if hasFile(dirPath, "build.gradle") {
		project.PackageManager = "gradle"
		project.BuildCommand = fmt.Sprintf("cd %s && ./gradlew build", project.Path)
		project.TestCommand = fmt.Sprintf("cd %s && ./gradlew test", project.Path)
		project.ConfigFiles = findConfigFiles(dirPath, []string{"build.gradle", "settings.gradle"})
	}

	return project
}

// Helper utility functions
func inferProjectType(dirName, dirPath string) string {
	dirNameLower := strings.ToLower(dirName)

	// Check directory name patterns
	if strings.Contains(dirNameLower, "frontend") || strings.Contains(dirNameLower, "client") || strings.Contains(dirNameLower, "web") || strings.Contains(dirNameLower, "ui") {
		return "frontend"
	}
	if strings.Contains(dirNameLower, "backend") || strings.Contains(dirNameLower, "api") || strings.Contains(dirNameLower, "server") {
		return "backend"
	}
	if strings.Contains(dirNameLower, "shared") || strings.Contains(dirNameLower, "common") || strings.Contains(dirNameLower, "lib") {
		return "shared"
	}
	if strings.Contains(dirNameLower, "service") {
		return "service"
	}
	if strings.Contains(dirNameLower, "package") {
		return "library"
	}

	// Check for frontend indicators in the directory
	frontendIndicators := []string{"public/index.html", "src/App.js", "src/App.tsx", "src/main.ts", "index.html"}
	for _, indicator := range frontendIndicators {
		if hasFile(dirPath, indicator) {
			return "frontend"
		}
	}

	// Check for backend indicators
	backendIndicators := []string{"main.go", "app.py", "server.js", "index.js"}
	for _, indicator := range backendIndicators {
		if hasFile(dirPath, indicator) {
			return "backend"
		}
	}

	return "library" // Default fallback
}

func detectNodePackageManager(dirPath string) string {
	if hasFile(dirPath, "yarn.lock") {
		return "yarn"
	}
	if hasFile(dirPath, "pnpm-lock.yaml") {
		return "pnpm"
	}
	return "npm" // Default
}

func detectJSFramework(deps map[string]interface{}) string {
	if _, hasReact := deps["react"]; hasReact {
		if _, hasNext := deps["next"]; hasNext {
			return "next.js"
		}
		return "react"
	}
	if _, hasVue := deps["vue"]; hasVue {
		return "vue"
	}
	if _, hasAngular := deps["@angular/core"]; hasAngular {
		return "angular"
	}
	if _, hasSvelte := deps["svelte"]; hasSvelte {
		return "svelte"
	}
	if _, hasExpress := deps["express"]; hasExpress {
		return "express"
	}
	if _, hasNest := deps["@nestjs/core"]; hasNest {
		return "nestjs"
	}

	// Check for TypeScript
	if _, hasTS := deps["typescript"]; hasTS {
		return "typescript"
	}

	return "javascript"
}

func hasFile(dirPath, fileName string) bool {
	// Support glob patterns like "vite.config.*"
	if strings.Contains(fileName, "*") {
		matches, _ := filepath.Glob(filepath.Join(dirPath, fileName))
		return len(matches) > 0
	}

	_, err := os.Stat(filepath.Join(dirPath, fileName))
	return err == nil
}

func findConfigFiles(dirPath string, patterns []string) []string {
	var found []string
	for _, pattern := range patterns {
		if hasFile(dirPath, pattern) {
			found = append(found, pattern)
		}
	}
	return found
}

func shouldSkipDir(dirName string) bool {
	skipDirs := []string{
		"node_modules", "vendor", ".git", "build", "dist", "target",
		"__pycache__", ".venv", "venv", ".env", ".next", ".nuxt",
		"coverage", ".coverage", "tmp", "temp", ".tmp",
	}

	for _, skip := range skipDirs {
		if dirName == skip {
			return true
		}
	}

	return strings.HasPrefix(dirName, ".")
}

// detectRootBuildCommand detects build commands for monorepo root
func detectRootBuildCommand(rootDir string, projects map[string]workspaceinfo.ProjectInfo) string {
	// Check for common monorepo build tools
	if hasFile(rootDir, "lerna.json") {
		return "lerna run build"
	}
	if hasFile(rootDir, "nx.json") {
		return "nx run-many --target=build --all"
	}
	if hasFile(rootDir, "rush.json") {
		return "rush build"
	}
	if hasFile(rootDir, "package.json") {
		// Check for npm workspace or yarn workspace
		if content, err := os.ReadFile(filepath.Join(rootDir, "package.json")); err == nil {
			var pkg map[string]interface{}
			if json.Unmarshal(content, &pkg) == nil {
				if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
					if _, hasBuildAll := scripts["build:all"]; hasBuildAll {
						return "npm run build:all"
					}
					if _, hasBuild := scripts["build"]; hasBuild {
						return "npm run build"
					}
				}
				// Check for workspaces
				if _, ok := pkg["workspaces"]; ok {
					return "npm run build --workspaces"
				}
			}
		}
	}

	// Fallback: generate a command to build all detected projects
	var buildCmds []string
	for _, project := range projects {
		if project.BuildCommand != "" {
			buildCmds = append(buildCmds, project.BuildCommand)
		}
	}

	if len(buildCmds) > 0 {
		return strings.Join(buildCmds, " && ")
	}

	return ""
}

// detectRootTestCommand detects test commands for monorepo root
func detectRootTestCommand(rootDir string, projects map[string]workspaceinfo.ProjectInfo) string {
	// Check for common monorepo test tools
	if hasFile(rootDir, "lerna.json") {
		return "lerna run test"
	}
	if hasFile(rootDir, "nx.json") {
		return "nx run-many --target=test --all"
	}
	if hasFile(rootDir, "rush.json") {
		return "rush test"
	}
	if hasFile(rootDir, "package.json") {
		if content, err := os.ReadFile(filepath.Join(rootDir, "package.json")); err == nil {
			var pkg map[string]interface{}
			if json.Unmarshal(content, &pkg) == nil {
				if scripts, ok := pkg["scripts"].(map[string]interface{}); ok {
					if _, hasTestAll := scripts["test:all"]; hasTestAll {
						return "npm run test:all"
					}
					if _, hasTest := scripts["test"]; hasTest {
						return "npm run test"
					}
				}
			}
		}
	}

	// Fallback: generate a command to test all detected projects
	var testCmds []string
	for _, project := range projects {
		if project.TestCommand != "" {
			testCmds = append(testCmds, project.TestCommand)
		}
	}

	if len(testCmds) > 0 {
		return strings.Join(testCmds, " && ")
	}

	return ""
}

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

	// Check for Python project
	pythonFilesFound := false
	requirementsTxt := filepath.Join(rootDir, "requirements.txt")
	pyprojectToml := filepath.Join(rootDir, "pyproject.toml")
	setupPy := filepath.Join(rootDir, "setup.py")

	// Check for Python project indicators
	if _, err := os.Stat(requirementsTxt); err == nil {
		pythonFilesFound = true
	} else if _, err := os.Stat(pyprojectToml); err == nil {
		pythonFilesFound = true
	} else if _, err := os.Stat(setupPy); err == nil {
		pythonFilesFound = true
	} else {
		// Check for .py files in root or common directories
		filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skip common ignored directories
			if d.IsDir() && (d.Name() == "__pycache__" || d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "venv" || d.Name() == ".env") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".py") {
				pythonFilesFound = true
				return fmt.Errorf("found python file") // Use a custom error to stop walking
			}
			return nil
		})
	}

	if pythonFilesFound {
		// Try to detect common Python build/test commands
		if _, err := os.Stat(pyprojectToml); err == nil {
			// Modern Python with pyproject.toml
			return "python -m build" // or could be "poetry build" but this is more universal
		} else if _, err := os.Stat(setupPy); err == nil {
			// Traditional setup.py
			return "python setup.py build"
		} else {
			// Just Python files, likely a script-based project - run tests if available
			if _, err := os.Stat(filepath.Join(rootDir, "test")); err == nil {
				return "python -m pytest"
			} else if _, err := os.Stat(filepath.Join(rootDir, "tests")); err == nil {
				return "python -m pytest"
			} else {
				return "python -m py_compile *.py" // Basic syntax check
			}
		}
	}

	return "" // Cannot determine build command
}

// validateAndUpdateWorkspace checks the current file system against the workspace.json file,
// analyzes new or changed files, removes deleted files, and saves the updated workspace.
func validateAndUpdateWorkspace(rootDir string, cfg *config.Config) (workspaceinfo.WorkspaceFile, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)

	workspace, err := workspaceinfo.LoadWorkspaceFile()
	if err != nil {
		if os.IsNotExist(err) {
			logger.LogProcessStep("No existing workspace file found. Creating a new one.")
			workspace = workspaceinfo.WorkspaceFile{Files: make(map[string]workspaceinfo.WorkspaceFileInfo)}
		} else {
			return workspaceinfo.WorkspaceFile{}, fmt.Errorf("failed to load workspace file: %w", err)
		}
	}

	currentFiles := make(map[string]bool)
	ignoreRules := filediscovery.GetIgnoreRules(rootDir)

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
			return workspaceinfo.WorkspaceFile{}, fmt.Errorf("workspace update cancelled by user due to too many new files")
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
					fileSummary, fileExports, fileReferences, llmErr = text.GetSummary(f.content, f.path, cfg)
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
		workspace.Files[result.relativePath] = workspaceinfo.WorkspaceFileInfo{
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

	if err := workspaceinfo.SaveWorkspaceFile(workspace); err != nil {
		return workspace, err
	}

	return workspace, nil
}

// detectWorkspaceContext populates lightweight workspace context fields
func detectWorkspaceContext(workspace *workspaceinfo.WorkspaceFile, rootDir string, logger *utils.Logger) {
	langs := detectLanguages(rootDir)
	if len(langs) > 0 {
		workspace.Languages = langs
	}

	// Detect monorepo structure
	logger.LogProcessStep("--- Analyzing workspace structure for monorepo projects ---")
	projects, monorepoType := detectMonorepoStructure(rootDir)

	if len(projects) > 0 {
		workspace.Projects = projects
		workspace.MonorepoType = monorepoType
		logger.LogProcessStep(fmt.Sprintf("--- Detected %s workspace with %d projects ---", monorepoType, len(projects)))

		// Log detected projects
		for name, project := range projects {
			logger.LogProcessStep(fmt.Sprintf("   📦 %s: %s %s project at %s", name, project.Language, project.Type, project.Path))
		}

		// For monorepos, try to detect root-level build commands
		if monorepoType == "multi" {
			if workspace.RootBuildCommand == "" {
				if rootBC := detectRootBuildCommand(rootDir, projects); rootBC != "" {
					workspace.RootBuildCommand = rootBC
					logger.LogProcessStep(fmt.Sprintf("--- Detected root build command: '%s' ---", rootBC))
				}
			}
			if workspace.RootTestCommand == "" {
				if rootTC := detectRootTestCommand(rootDir, projects); rootTC != "" {
					workspace.RootTestCommand = rootTC
					logger.LogProcessStep(fmt.Sprintf("--- Detected root test command: '%s' ---", rootTC))
				}
			}
		}
	} else {
		workspace.MonorepoType = "single"
	}

	// Legacy single-project build command detection for backward compatibility
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
	ui.ShowProgressWithDetails("🔄 Preparing workspace...", "Building workspace context (files, syntactic overviews)…")
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
	mergeInsights := func(dst *workspaceinfo.ProjectInsights, src workspaceinfo.ProjectInsights) {
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
	if workspace.ProjectGoals == (workspaceinfo.ProjectGoals{}) {
		logger.LogProcessStep("--- Autogenerating project goals from syntactic overview ---")
		// Simple heuristic for goals
		workspace.ProjectGoals = workspaceinfo.ProjectGoals{
			Mission: "Analyze and edit the codebase based on user instructions.",
		}
	}
	if workspace.ProjectInsights == (workspaceinfo.ProjectInsights{}) { // regenerate only when empty (or later heuristic)
		logger.LogProcessStep("--- Autogenerating project insights from syntactic overview ---")
		// Insights are already populated by heuristics
	}
	if err := workspaceinfo.SaveWorkspaceFile(workspace); err != nil {
		logger.Logf("Warning: Failed to save workspace metadata: %v\n", err)
	}

	// Use simple keyword-based file selection with limits to avoid overwhelming context
	maxFullContextFiles := 3 // Default: Very focused full context for most relevant files
	if cfg.FromAgent {
		// Check if this is a documentation task by looking for documentation keywords
		if strings.Contains(strings.ToLower(instructions), "document") ||
			strings.Contains(strings.ToLower(instructions), "docs") ||
			strings.Contains(strings.ToLower(instructions), "api_docs") {
			maxFullContextFiles = 2 // Reduce for documentation tasks to save tokens
		} else {
			maxFullContextFiles = 3 // Keep 3 files for other agent runs
		}
	} else {
		maxFullContextFiles = 7 // Standard limit for direct code command runs
	}

	// Reduce summary context for documentation tasks
	maxSummaryContextFiles := 20 // Default: Broader summary context for exploration
	if cfg.FromAgent && (strings.Contains(strings.ToLower(instructions), "document") ||
		strings.Contains(strings.ToLower(instructions), "docs") ||
		strings.Contains(strings.ToLower(instructions), "api_docs")) {
		maxSummaryContextFiles = 10 // Reduce summary files for documentation tasks
	}

	const minScoreForFullContext = 3 // Only files with very high relevance scores get full context

	var fileScores []struct {
		file  string
		score int
	}

	logger.LogProcessStep("--- Using hybrid keyword-based file selection (focused full context + broad summaries) ---")
	ui.PublishStatus("Selecting relevant files via keywords…")
	keywords := text.ExtractKeywords(instructions)

	// Calculate scores for all files
	for file, info := range workspace.Files {
		score := 0
		content := info.Summary + " " + info.Exports + " " + file
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(content), strings.ToLower(kw)) {
				score++
			}
		}
		if score > 0 {
			fileScores = append(fileScores, struct {
				file  string
				score int
			}{file, score})
		}
	}

	// Sort by score (highest first)
	sort.Slice(fileScores, func(i, j int) bool {
		return fileScores[i].score > fileScores[j].score
	})

	// Select top files based on limits and score thresholds
	var fullContextFiles, summaryContextFiles []string
	for _, fs := range fileScores {
		if fs.score >= minScoreForFullContext && len(fullContextFiles) < maxFullContextFiles {
			fullContextFiles = append(fullContextFiles, fs.file)
		} else if fs.score > 0 && len(summaryContextFiles) < maxSummaryContextFiles {
			summaryContextFiles = append(summaryContextFiles, fs.file)
		}
	}

	if len(fullContextFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("--- Selected the following files for full context: %s ---", strings.Join(fullContextFiles, ", ")))
	}
	if len(fullContextFiles) >= maxFullContextFiles {
		logger.LogProcessStep(fmt.Sprintf("--- Limited to top %d files for full context (score >= %d) ---", maxFullContextFiles, minScoreForFullContext))
	}
	if len(summaryContextFiles) > 0 {
		logger.LogProcessStep(fmt.Sprintf("--- Selected the following files for summary context: %s ---", strings.Join(summaryContextFiles, ", ")))
	}
	if len(summaryContextFiles) >= maxSummaryContextFiles {
		logger.LogProcessStep(fmt.Sprintf("--- Limited to top %d files for summary context to avoid overwhelming LLM ---", maxSummaryContextFiles))
	}
	if len(fullContextFiles) == 0 && len(summaryContextFiles) == 0 {
		logger.LogProcessStep("--- No files were selected as relevant for context. ---")
	}

	for _, file := range fullContextFiles {
		ui.ShowProgressWithDetails("📝 Reading files...", "Reading selected files for full context…")
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
