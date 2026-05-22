package commands

import (
	"reflect"
	"strings"
	"testing"
)

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
		{"models", "model", 1},
		{"commit", "cmoit", 2},
		{"help", "hpl", 2},
	}
	for _, c := range cases {
		got := levenshtein(c.a, c.b)
		if got != c.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSuggestCommands_KnownTypos(t *testing.T) {
	r := NewCommandRegistry()
	cases := []struct {
		typo       string
		wantsFirst string // expected first suggestion
	}{
		{"cmoit", "commit"},
		{"comit", "commit"},
		{"hellp", "help"},
		{"modls", "model"},
		{"prvders", "provider"},
		{"sttus", "status"},
	}
	for _, c := range cases {
		t.Run(c.typo, func(t *testing.T) {
			suggestions := r.SuggestCommands(c.typo, 3)
			if len(suggestions) == 0 {
				t.Fatalf("expected at least one suggestion for %q, got none", c.typo)
			}
			if suggestions[0] != c.wantsFirst {
				t.Errorf("SuggestCommands(%q)[0] = %q, want %q (full: %v)",
					c.typo, suggestions[0], c.wantsFirst, suggestions)
			}
		})
	}
}

func TestSuggestCommands_PrefixWinsOverEditDistance(t *testing.T) {
	r := NewCommandRegistry()
	suggestions := r.SuggestCommands("comm", 3)
	if len(suggestions) == 0 {
		t.Fatal("expected at least one suggestion for prefix 'comm'")
	}
	if suggestions[0] != "commit" {
		t.Errorf("prefix 'comm' should resolve to 'commit' first, got %v", suggestions)
	}
}

func TestSuggestCommands_TooDifferent_ReturnsNothing(t *testing.T) {
	r := NewCommandRegistry()
	suggestions := r.SuggestCommands("xyzqqqq", 3)
	if len(suggestions) != 0 {
		t.Errorf("unrelated input should return no suggestions, got %v", suggestions)
	}
}

func TestSuggestCommands_EmptyInput(t *testing.T) {
	r := NewCommandRegistry()
	if got := r.SuggestCommands("", 3); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}

func TestSuggestCommands_RespectMaxSuggestions(t *testing.T) {
	r := NewCommandRegistry()
	// 's' matches many commands by prefix; cap result count.
	suggestions := r.SuggestCommands("s", 2)
	if len(suggestions) > 2 {
		t.Errorf("expected at most 2 suggestions, got %d (%v)", len(suggestions), suggestions)
	}
}

func TestAliases_ExecuteResolvesAlias(t *testing.T) {
	r := NewCommandRegistry()
	_, exists := r.GetCommand("m")
	if !exists {
		t.Fatal("alias /m should resolve to a command")
	}
	cmd, _ := r.GetCommand("m")
	if cmd.Name() != "model" {
		t.Errorf("/m should resolve to model, got %s", cmd.Name())
	}
}

func TestAliases_DocumentedSet(t *testing.T) {
	r := NewCommandRegistry()
	cases := map[string]string{
		"m": "model",
		"p": "provider",
		"x": "exit",
		"q": "exit",
		"?": "help",
		"h": "help",
	}
	for alias, canonical := range cases {
		cmd, ok := r.GetCommand(alias)
		if !ok {
			t.Errorf("alias %q not registered", alias)
			continue
		}
		if cmd.Name() != canonical {
			t.Errorf("alias %q resolves to %q, want %q", alias, cmd.Name(), canonical)
		}
	}
}

func TestAliasesOf_ReturnsAllShortFormsForCanonical(t *testing.T) {
	r := NewCommandRegistry()
	aliases := r.AliasesOf("exit")
	// Should include both x and q.
	found := map[string]bool{}
	for _, a := range aliases {
		found[a] = true
	}
	if !found["x"] || !found["q"] {
		t.Errorf("AliasesOf(exit) should include x and q, got %v", aliases)
	}
}

func TestCompletionCandidates_IncludesCanonicalAndAliases(t *testing.T) {
	r := NewCommandRegistry()
	candidates := r.CompletionCandidates()
	want := []string{"help", "model", "provider", "exit", "m", "p", "x", "q", "h", "?"}
	for _, w := range want {
		if !sliceContains(candidates, w) {
			t.Errorf("CompletionCandidates missing %q (full list: %v)", w, candidates)
		}
	}
	// Verify it's sorted so cycle order is deterministic.
	if !isSorted(candidates) {
		t.Errorf("CompletionCandidates should be sorted, got %v", candidates)
	}
}

func TestExecute_UnknownCommandIncludesSuggestion(t *testing.T) {
	r := NewCommandRegistry()
	err := r.Execute("/cmoit", nil)
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error should suggest a command: %v", err)
	}
	if !strings.Contains(err.Error(), "commit") {
		t.Errorf("error should mention 'commit' as suggestion: %v", err)
	}
}

func TestExecute_UnknownCommandWithNoCloseMatchOmitsSuggestion(t *testing.T) {
	r := NewCommandRegistry()
	err := r.Execute("/xyzqqqq", nil)
	if err == nil {
		t.Fatal("expected error for unknown command, got nil")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error should NOT suggest for unrelated input: %v", err)
	}
}

func sliceContains(s []string, target string) bool {
	for _, v := range s {
		if v == target {
			return true
		}
	}
	return false
}

func isSorted(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

// Compile-time sanity: reflect.DeepEqual is the std-lib import-checker.
var _ = reflect.DeepEqual
