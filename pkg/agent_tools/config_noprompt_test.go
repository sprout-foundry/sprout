package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tool code runs mid-turn while the REPL owns stdin. configuration.NewManager
// runs the interactive first-run provider picker when the configured
// provider looks unauthenticated; called from a tool, that prompt waits on
// a stdin nobody can answer and every tool call (shell_command, git, …)
// times out. Tools must load config with NewManagerSilent. The prompt only
// fires on a real terminal, so this guards the call sites directly.
func TestToolsNeverUseInteractiveConfigManager(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "configuration.NewManager()") {
			t.Errorf("%s calls configuration.NewManager(), which can prompt on stdin mid-tool-call; use configuration.NewManagerSilent()", f)
		}
	}
}
