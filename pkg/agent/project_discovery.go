package agent

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectInfo struct {
	Path        string   // Absolute path to project root
	Name        string   // Directory name or project name from AGENTS.md
	Description string   // First paragraph from AGENTS.md if present
	HasAgentsMd bool     // Whether project has AGENTS.md
	HasGitRepo  bool     // Whether project has .git directory
	Languages   []string // Detected languages (from file extensions, go.mod, package.json, etc.)
	RelPath     string   // Relative path from home directory
}

var skipDirs = map[string]bool{
	"node_modules": true, ".cache": true, ".local": true, ".config": true,
	".ssh": true, ".gnupg": true, ".aws": true, ".kube": true,
	".vscode": true, ".idea": true, ".Trash": true,
	"Library": true, "Applications": true, "Desktop": true,
	"Downloads": true, "Music": true, "Pictures": true, "Videos": true,
	"__pycache__": true, ".git": true, ".hg": true, ".svn": true,
	".npm": true, ".yarn": true, ".pnp": true, ".next": true,
	".turbo": true, "dist": true, "build": true, "out": true,
	"target": true, "vendor": true, ".tox": true, ".venv": true,
	"venv": true, "env": true, ".env": true, ".direnv": true,
	".maildir": true, "Maildir": true, "calibre": true,
}

var languagePatterns = map[string][]string{
	"Go":         {"go.mod"},
	"TypeScript": {"package.json", "tsconfig.json"},
	"JavaScript": {"package.json"},
	"Rust":       {"Cargo.toml"},
	"Python":     {"requirements.txt", "setup.py", "pyproject.toml", "Pipfile", "poetry.lock"},
	"Java":       {"pom.xml", "build.gradle", "build.gradle.kts"},
	"C#":         {"*.csproj"},
	"C++":        {"CMakeLists.txt", "Makefile"},
	"Ruby":       {"Gemfile", "Rakefile"},
	"PHP":        {"composer.json"},
	"Swift":      {"Package.swift"},
	"Dart":       {"pubspec.yaml"},
	"Kotlin":     {"build.gradle.kts", "build.gradle"},
	"Elixir":     {"mix.exs"},
	"Haskell":    {"stack.yaml", "cabal.project"},
	"Scala":      {"build.sbt"},
}

func DiscoverProjects(homeDir string, maxDepth int) ([]ProjectInfo, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	var projects []ProjectInfo

	walkFunc := func(path string, info os.FileInfo, err error) error {
		select {
		case <-timeoutCtx.Done():
			return timeoutCtx.Err()
		default:
		}

		if err != nil {
			return nil
		}

		if !info.IsDir() {
			return nil
		}

		baseName := filepath.Base(path)
		if skipDirs[baseName] {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(homeDir, path)
		if err != nil {
			return nil
		}

		if relPath == "." {
			return nil
		}

		depth := strings.Count(relPath, string(filepath.Separator))
		if depth > maxDepth {
			return filepath.SkipDir
		}

		hasGit := hasGitRepo(path)
		hasAgentsMd := hasAgentsMdFile(path)
		name := filepath.Base(path)
		description := ""

		if hasAgentsMd {
			agentsPath := filepath.Join(path, "AGENTS.md")
			parsedName, parsedDesc := ParseAgentsMd(agentsPath)
			if parsedName != "" {
				name = parsedName
			}
			if parsedDesc != "" {
				description = parsedDesc
			}
		}

		languages := DetectLanguages(path)

		// Only include directories that look like real projects
		if !hasGit && !hasAgentsMd && len(languages) == 0 {
			return nil
		}

		project := ProjectInfo{
			Path:        path,
			Name:        name,
			Description: description,
			HasAgentsMd: hasAgentsMd,
			HasGitRepo:  hasGit,
			Languages:   languages,
			RelPath:     relPath,
		}

		projects = append(projects, project)

		return nil
	}

	err := filepath.Walk(homeDir, walkFunc)

	sortProjects(projects)

	if len(projects) > 50 {
		projects = projects[:50]
	}

	return projects, err
}

func ParseAgentsMd(path string) (name string, description string) {
	file, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var headingFound bool
	var descriptionFound bool
	var inHeadingContent bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "# ") {
			name = strings.TrimPrefix(line, "# ")
			headingFound = true
			inHeadingContent = true
			continue
		}

		if inHeadingContent {
			if line == "" {
				inHeadingContent = false
				continue
			}
			descriptionFound = true
			description = line
			break
		}

		if headingFound && !descriptionFound && line != "" {
			descriptionFound = true
			description = line
			break
		}
	}

	return name, description
}

func DetectLanguages(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	foundLanguages := make(map[string]bool)

	for _, entry := range entries {
		name := entry.Name()

		for lang, patterns := range languagePatterns {
			for _, pattern := range patterns {
				if matched, _ := filepath.Match(pattern, name); matched {
					foundLanguages[lang] = true
					break
				}
			}
		}
	}

	languages := make([]string, 0, len(foundLanguages))
	for lang := range foundLanguages {
		languages = append(languages, lang)
	}

	return languages
}

func hasGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func hasAgentsMdFile(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	return err == nil
}

func sortProjects(projects []ProjectInfo) {
	sort.Slice(projects, func(i, j int) bool {
		scoreI := projectScore(projects[i])
		scoreJ := projectScore(projects[j])
		if scoreI != scoreJ {
			return scoreI > scoreJ // Higher score first
		}
		return strings.ToLower(projects[i].Path) < strings.ToLower(projects[j].Path)
	})
}

func projectScore(p ProjectInfo) int {
	score := 0

	if p.HasAgentsMd {
		score += 2
	}

	if p.HasGitRepo {
		score += 1
	}

	return score
}
