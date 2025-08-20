package agent

import "testing"

func TestJoinLines(t *testing.T) {
	if got := joinLines(nil); got != "" {
		t.Fatalf("nil -> %q", got)
	}
	if got := joinLines([]string{}); got != "" {
		t.Fatalf("empty -> %q", got)
	}
	if got := joinLines([]string{"a"}); got != "a" {
		t.Fatalf("one -> %q", got)
	}
	if got := joinLines([]string{"a", "b"}); got != "a\nb" {
		t.Fatalf("two -> %q", got)
	}
}

func TestBuildIntentAnalysisPrompt_Basic(t *testing.T) {
	p := BuildIntentAnalysisPrompt("Refactor", "go", []string{"a.go", "b.go"})
	if p == "" || !containsAll(p, []string{"User Intent: Refactor", "Project Type: go", "Total Files: 2", "a.go", "b.go"}) {
		t.Fatalf("unexpected prompt: %q", p)
	}
}

func TestBuildProgressEvaluationPrompt_EmbedsContext(t *testing.T) {
	ctx := "Current plan: 3 files"
	p := BuildProgressEvaluationPrompt(ctx)
	if p == "" || !containsAll(p, []string{"Current plan: 3 files", "AVAILABLE ACTIONS & WHEN TO USE THEM", "SMART DECISION MAKING"}) {
		t.Fatalf("unexpected prompt: %q", p)
	}
}

func TestBuildQuestionAgentSystemPrompt(t *testing.T) {
	s := BuildQuestionAgentSystemPrompt()
	if s == "" || !containsAll(s, []string{"You are an intelligent assistant", "read_file", "run_shell_command", "search_web"}) {
		t.Fatalf("unexpected system prompt: %q", s)
	}
}

// small helper to assert multiple substrings
func containsAll(hay string, needles []string) bool {
	for _, n := range needles {
		if !contains(hay, n) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool { return (len(sub) == 0) || (indexOf(s, sub) >= 0) })()
}

func indexOf(s, sub string) int {
	// simple substring search to avoid importing strings to keep tests compact
	if sub == "" {
		return 0
	}
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
