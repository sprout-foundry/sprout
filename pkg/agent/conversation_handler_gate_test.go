package agent

import "testing"

func TestIsSelfReviewGatePersonaEnabled(t *testing.T) {
	tests := []struct {
		name     string
		persona  string
		expected bool
	}{
		{name: "orchestrator", persona: "orchestrator", expected: true},
		{name: "coder", persona: "coder", expected: true},
		{name: "repo_orchestrator", persona: "repo_orchestrator", expected: true},
		{name: "case normalized", persona: " Coder ", expected: true},
		{name: "case insensitive Orchestrator", persona: "Orchestrator", expected: true},
		{name: "case insensitive ORCHESTRATOR", persona: "ORCHESTRATOR", expected: true},
		{name: "case insensitive OrcHestRator", persona: "OrcHestRator", expected: true},
		{name: "case insensitive repo_orchestrator", persona: "Repo_Orchestrator", expected: true},
		{name: "case insensitive CODER", persona: "CODER", expected: true},
		{name: "general disabled", persona: "general", expected: false},
		{name: "web scraper disabled", persona: "web_scraper", expected: false},
		{name: "empty disabled", persona: "", expected: false},
		{name: "tester disabled", persona: "tester", expected: false},
		{name: "debugger disabled", persona: "debugger", expected: false},
		{name: "random persona disabled", persona: "some_random_persona", expected: false},
		{name: "whitespace trimmed orchestrator", persona: "  orchestrator  ", expected: true},
		{name: "whitespace trimmed tester", persona: "  tester  ", expected: false},
		{name: "refactor disabled", persona: "refactor", expected: false},
		{name: "code_reviewer disabled", persona: "code_reviewer", expected: false},
		{name: "researcher disabled", persona: "researcher", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSelfReviewGatePersonaEnabled(tc.persona); got != tc.expected {
				t.Fatalf("isSelfReviewGatePersonaEnabled(%q) = %v, expected %v", tc.persona, got, tc.expected)
			}
		})
	}
}

func TestHasCodeLikeTrackedFiles(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected bool
	}{
		{name: "go file", files: []string{"pkg/main.go"}, expected: true},
		{name: "dockerfile", files: []string{"Dockerfile"}, expected: true},
		{name: "markdown only", files: []string{"README.md"}, expected: false},
		{name: "text only", files: []string{"notes.txt"}, expected: false},
		{name: "mixed includes code", files: []string{"docs/plan.md", "api/schema.yaml"}, expected: true},
		{name: "empty list", files: nil, expected: false},
		{name: "empty slice", files: []string{}, expected: false},
		{name: "python file", files: []string{"script.py"}, expected: true},
		{name: "javascript file", files: []string{"app.js"}, expected: true},
		{name: "typescript file", files: []string{"app.ts"}, expected: true},
		{name: "unsupported extension", files: []string{"readme.txt"}, expected: false},
		{name: "more unsupported extensions", files: []string{"photo.png", "data.pdf"}, expected: false},
		{name: "makefile", files: []string{"Makefile"}, expected: true},
		{name: "justfile", files: []string{"justfile"}, expected: true},
		{name: "CMakeLists.txt", files: []string{"CMakeLists.txt"}, expected: true},
		{name: "build.gradle", files: []string{"build.gradle"}, expected: true},
		{name: "mixed case extension .PY", files: []string{"script.PY"}, expected: true},
		{name: "mixed case extension .GO", files: []string{"main.GO"}, expected: true},
		{name: "path with directories", files: []string{"src/internal/handler.go"}, expected: true},
		{name: "path with dirs unsupported", files: []string{"docs/notes/readme.txt"}, expected: false},
		{name: "no extension", files: []string{"README", "LICENSE"}, expected: false},
		{name: "Dockerfile in subdir", files: []string{"infra/Dockerfile"}, expected: true},
		{name: "mixed list with code", files: []string{"notes.txt", "src/main.go", "data.csv"}, expected: true},
		{name: "mixed list no code", files: []string{"notes.txt", "photo.png", "data.csv"}, expected: false},
		{name: "empty string entry skipped", files: []string{"", "  "}, expected: false},
		{name: "empty string with code", files: []string{"", "main.go"}, expected: true},
		{name: "shell script", files: []string{"deploy.sh"}, expected: true},
		{name: "rust file", files: []string{"lib.rs"}, expected: true},
		{name: "sql file", files: []string{"schema.sql"}, expected: true},
		{name: "yaml file", files: []string{"config.yaml"}, expected: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasCodeLikeTrackedFiles(tc.files); got != tc.expected {
				t.Fatalf("hasCodeLikeTrackedFiles(%v) = %v, expected %v", tc.files, got, tc.expected)
			}
		})
	}
}
