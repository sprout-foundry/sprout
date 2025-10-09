package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	gitutil "github.com/alantheprice/ledit/pkg/git"
)

// baseContextSpec represents the JSON structure injected into the conversation
type baseContextSpec struct {
	RepoRoot         string              `json:"repo_root"`
	ProjectTypes     []string            `json:"project_types"`
	Files            map[string][]string `json:"files"`
	Entrypoints      []string            `json:"entrypoints"`
	TestsPresent     bool                `json:"tests_present"`
	BuildSuggestions []commandSuggestion `json:"build_suggestions,omitempty"`
	TestSuggestions  []commandSuggestion `json:"test_suggestions,omitempty"`
}

type commandSuggestion struct {
	ProjectType string `json:"project_type"`
	Command     string `json:"command"`
	Available   bool   `json:"available"`
}

// BuildBaseContextJSON scans the workspace and returns a minimal JSON manifest
// to speed up discovery. It is conservative and ignores common build/vendor directories.
func BuildBaseContextJSON() string {
	// Allow disabling via environment variable if needed
	if os.Getenv("LEDIT_BASE_CONTEXT_DISABLE") != "" {
		return ""
	}

	root := detectRepoRoot()

	// Project type markers
	projectMarkers := map[string]string{
		"go.mod":           "go",
		"package.json":     "node",
		"pyproject.toml":   "python",
		"requirements.txt": "python",
		"Cargo.toml":       "rust",
		"pom.xml":          "java",
		"build.gradle":     "java",
	}

	// Ignore directories
	ignoreDirs := map[string]struct{}{
		".git": {}, "node_modules": {}, "vendor": {}, "dist": {}, "build": {},
		".cache": {}, ".venv": {}, "target": {}, "out": {}, ".next": {},
	}

	// Category buckets
	files := map[string][]string{
		"go":     {},
		"ts":     {},
		"js":     {},
		"docs":   {},
		"config": {},
		"other":  {},
	}

	entrypoints := make([]string, 0, 16)
	entrySeen := map[string]struct{}{}
	projTypesSet := map[string]struct{}{}
	testsPresent := false

	// Additional marker booleans for suggestions
	var (
		pnpmLockDetected bool
		yarnLockDetected bool
		npmLockDetected  bool
		mavenDetected    bool
		gradleDetected   bool
		gradlewDetected  bool
	)

	// Cap total files to ~200
	totalCap := 200
	added := 0

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Skip ignored directories
		if d.IsDir() {
			name := d.Name()
			if _, ok := ignoreDirs[name]; ok {
				return filepath.SkipDir
			}
			return nil
		}

		rel := path
		if abs, err := filepath.Abs(path); err == nil {
			if r, err := filepath.Abs(root); err == nil {
				if rel2, err := filepath.Rel(r, abs); err == nil {
					rel = rel2
				}
			}
		}

		base := filepath.Base(rel)
		// Detect project types
		if t, ok := projectMarkers[base]; ok {
			projTypesSet[t] = struct{}{}
		}

		// Track tool/pm specific markers
		switch strings.ToLower(base) {
		case "pnpm-lock.yaml":
			pnpmLockDetected = true
		case "yarn.lock":
			yarnLockDetected = true
		case "package-lock.json":
			npmLockDetected = true
		case "pom.xml":
			mavenDetected = true
		case "build.gradle":
			gradleDetected = true
		case "gradlew":
			gradlewDetected = true
		}

		// Detect tests
		lower := strings.ToLower(base)
		if strings.HasSuffix(lower, "_test.go") ||
			strings.HasSuffix(lower, ".test.ts") || strings.HasSuffix(lower, ".spec.ts") ||
			strings.HasSuffix(lower, ".test.tsx") || strings.HasSuffix(lower, ".spec.tsx") ||
			strings.HasSuffix(lower, ".test.js") || strings.HasSuffix(lower, ".spec.js") ||
			strings.HasSuffix(lower, "_test.py") || strings.HasPrefix(lower, "test_") {
			testsPresent = true
		}

		// Entrypoints (common)
		if isEntrypoint(rel, base) {
			if _, ok := entrySeen[rel]; !ok {
				entrypoints = append(entrypoints, filepath.ToSlash(rel))
				entrySeen[rel] = struct{}{}
			}
		}

		// Categorize
		if added >= totalCap {
			return nil
		}
		cat := categorizeFile(rel, base)
		files[cat] = append(files[cat], filepath.ToSlash(rel))
		added++
		return nil
	})

	// Project types slice
	var projectTypes []string
	for t := range projTypesSet {
		projectTypes = append(projectTypes, t)
	}
	if len(projectTypes) == 0 {
		projectTypes = []string{"unknown"}
	}

	// Build command suggestions based on detected project types
	buildS, testS := suggestCommands(projectTypes, pnpmLockDetected, yarnLockDetected, npmLockDetected, mavenDetected, gradleDetected, gradlewDetected)

	spec := baseContextSpec{
		RepoRoot:         filepath.ToSlash(root),
		ProjectTypes:     projectTypes,
		Files:            files,
		Entrypoints:      entrypoints,
		TestsPresent:     testsPresent,
		BuildSuggestions: buildS,
		TestSuggestions:  testS,
	}
	b, _ := json.Marshal(spec)

	// Best-effort save to .ledit/base_context.json for inspection
	// Errors are intentionally ignored to avoid disrupting agent flow.
	ledPath := filepath.Join(root, ".ledit")
	_ = os.MkdirAll(ledPath, os.ModePerm)
	_ = os.WriteFile(filepath.Join(ledPath, "base_context.json"), b, 0644)

	return string(b)
}

// GetOrBuildBaseContext returns a cached base context for the session,
// building it once if needed.
func (a *Agent) GetOrBuildBaseContext() string {
	if a == nil {
		return ""
	}
	a.baseContextOnce.Do(func() {
		a.baseContextJSON = BuildBaseContextJSON()
	})
	return a.baseContextJSON
}

func detectRepoRoot() string {
	if root, err := gitutil.GetGitRootDir(); err == nil && strings.TrimSpace(root) != "" {
		return root
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func isEntrypoint(rel, base string) bool {
	lower := strings.ToLower(base)
	if lower == "main.go" || lower == "app.go" {
		return true
	}
	if lower == "index.ts" || lower == "index.tsx" || lower == "index.js" || lower == "app.js" || lower == "server.js" {
		return true
	}
	// cmd/*/main.go patterns
	if strings.HasSuffix(lower, "/main.go") && strings.Contains(strings.ToLower(rel), "/cmd/") {
		return true
	}
	return false
}

func categorizeFile(rel, base string) string {
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "ts"
	case ".js", ".jsx":
		return "js"
	case ".md", ".rst":
		return "docs"
	case ".yml", ".yaml":
		return "config"
	}
	// Specific config filenames
	lname := strings.ToLower(base)
	switch lname {
	case "go.mod", "package.json", "yarn.lock", "pnpm-lock.yaml", "docker-compose.yml", "dockerfile":
		return "config"
	}
	return "other"
}

func suggestCommands(projectTypes []string, pnpmLock, yarnLock, npmLock, maven, gradle, gradlew bool) ([]commandSuggestion, []commandSuggestion) {
	var build []commandSuggestion
	var test []commandSuggestion

	has := func(bin string) bool { return which(bin) }

	for _, pt := range projectTypes {
		switch pt {
		case "go":
			build = append(build, commandSuggestion{ProjectType: pt, Command: "go build ./...", Available: has("go")})
			test = append(test, commandSuggestion{ProjectType: pt, Command: "go test ./... -v", Available: has("go")})
		case "node":
			pm, bin := pickNodePM(pnpmLock, yarnLock, npmLock)
			build = append(build, commandSuggestion{ProjectType: pt, Command: pm + " run build", Available: has(bin)})
			// keep tests quiet if possible for CI-friendly output
			cmd := pm + " test -s"
			if pm == "yarn" {
				cmd = "yarn test --silent"
			}
			test = append(test, commandSuggestion{ProjectType: pt, Command: cmd, Available: has(bin)})
		case "python":
			// Prefer pytest if available, else python -m pytest
			if has("pytest") {
				test = append(test, commandSuggestion{ProjectType: pt, Command: "pytest -q", Available: true})
			} else {
				test = append(test, commandSuggestion{ProjectType: pt, Command: "python -m pytest -q", Available: has("python")})
			}
		case "rust":
			build = append(build, commandSuggestion{ProjectType: pt, Command: "cargo build", Available: has("cargo")})
			test = append(test, commandSuggestion{ProjectType: pt, Command: "cargo test", Available: has("cargo")})
		case "java":
			// Prefer mvn if POM detected, else gradle/gradlew
			if maven {
				build = append(build, commandSuggestion{ProjectType: pt, Command: "mvn -q -DskipTests=false -e -B verify", Available: has("mvn")})
				test = append(test, commandSuggestion{ProjectType: pt, Command: "mvn -q -B test", Available: has("mvn")})
			} else if gradlew {
				build = append(build, commandSuggestion{ProjectType: pt, Command: "./gradlew build", Available: true})
				test = append(test, commandSuggestion{ProjectType: pt, Command: "./gradlew test", Available: true})
			} else if gradle {
				build = append(build, commandSuggestion{ProjectType: pt, Command: "gradle build", Available: has("gradle")})
				test = append(test, commandSuggestion{ProjectType: pt, Command: "gradle test", Available: has("gradle")})
			}
		}
	}

	return build, test
}

func which(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func pickNodePM(pnpmLock, yarnLock, npmLock bool) (pm string, bin string) {
	switch {
	case pnpmLock:
		return "pnpm", "pnpm"
	case yarnLock:
		return "yarn", "yarn"
	case npmLock:
		return "npm", "npm"
	default:
		// default to npm
		return "npm", "npm"
	}
}
