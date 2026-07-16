package agent

import (
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// SplitChainedCommand tests
// ---------------------------------------------------------------------------

func TestSplitChainedCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single command",
			input:    "echo hello",
			expected: []string{"echo hello"},
		},
		{
			name:     "double ampersand",
			input:    "echo hi && echo bye",
			expected: []string{"echo hi", "echo bye"},
		},
		{
			name:     "double pipe",
			input:    "cmd1 || cmd2",
			expected: []string{"cmd1", "cmd2"},
		},
		{
			name:     "semicolon",
			input:    "cmd1; cmd2",
			expected: []string{"cmd1", "cmd2"},
		},
		{
			name:     "pipe",
			input:    "cmd1 | cmd2",
			expected: []string{"cmd1", "cmd2"},
		},
		{
			name:     "mixed separators",
			input:    "cmd1 && cmd2 || cmd3; cmd4 | cmd5",
			expected: []string{"cmd1", "cmd2", "cmd3", "cmd4", "cmd5"},
		},
		{
			name:     "ampersand inside double quotes is preserved",
			input:    `echo "a && b" && echo done`,
			expected: []string{`echo "a && b"`, "echo done"},
		},
		{
			name:     "ampersand inside single quotes is preserved",
			input:    `echo 'a && b' && echo done`,
			expected: []string{`echo 'a && b'`, "echo done"},
		},
		{
			name:     "pipe inside quotes is preserved",
			input:    `grep "a|b|c" | head`,
			expected: []string{`grep "a|b|c"`, "head"},
		},
		{
			name:     "empty input",
			input:    "",
			expected: nil,
		},
		{
			name:     "trailing separator",
			input:    "echo hi;",
			expected: []string{"echo hi"},
		},
		{
			name:     "leading separator",
			input:    "; echo hi",
			expected: []string{"echo hi"},
		},
		{
			name:     "multiple spaces around separators",
			input:    "cmd1   &&   cmd2   ||   cmd3",
			expected: []string{"cmd1", "cmd2", "cmd3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tools.SplitChainedCommand(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("SplitChainedCommand(%q) = %v (len=%d), want %v (len=%d)",
					tt.input, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("part[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// EvaluateCommandPolicy tests
// ---------------------------------------------------------------------------

func TestEvaluateCommandPolicy_NilAndEmpty(t *testing.T) {
	// nil policies
	action, pattern, matched := EvaluateCommandPolicy("git push", nil)
	if matched {
		t.Fatalf("nil policies should not match: action=%s pattern=%s", action, pattern)
	}

	// empty rules
	policies := &configuration.CommandPolicies{Rules: []configuration.CommandRule{}}
	action, pattern, matched = EvaluateCommandPolicy("git push", policies)
	if matched {
		t.Fatalf("empty rules should not match: action=%s pattern=%s", action, pattern)
	}
}

func TestEvaluateCommandPolicy_BasicMatching(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "git push origin main", Action: configuration.CommandPolicyAllow},
		},
	}

	action, pattern, matched := EvaluateCommandPolicy("git push origin main", policies)
	if !matched {
		t.Fatal("exact match should succeed")
	}
	if action != configuration.CommandPolicyAllow {
		t.Errorf("action = %s, want allow", action)
	}
	if pattern != "git push origin main" {
		t.Errorf("pattern = %s, want 'git push origin main'", pattern)
	}
}

func TestEvaluateCommandPolicy_GlobMatching(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "git push*", Action: configuration.CommandPolicyAsk},
		},
	}

	// Should match
	action, _, matched := EvaluateCommandPolicy("git push origin main", policies)
	if !matched {
		t.Fatal("glob 'git push*' should match 'git push origin main'")
	}
	if action != configuration.CommandPolicyAsk {
		t.Errorf("action = %s, want ask", action)
	}

	// Should not match
	_, _, matched = EvaluateCommandPolicy("git pull origin main", policies)
	if matched {
		t.Fatal("glob 'git push*' should not match 'git pull origin main'")
	}
}

func TestEvaluateCommandPolicy_CaseInsensitive(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "git push*", Action: configuration.CommandPolicyAsk},
		},
	}

	// Uppercase command
	action, _, matched := EvaluateCommandPolicy("GIT PUSH ORIGIN MAIN", policies)
	if !matched {
		t.Fatal("matching should be case-insensitive: GIT PUSH ORIGIN MAIN")
	}
	if action != configuration.CommandPolicyAsk {
		t.Errorf("action = %s, want ask", action)
	}

	// Mixed case command
	action, _, matched = EvaluateCommandPolicy("Git Push Origin Main", policies)
	if !matched {
		t.Fatal("matching should be case-insensitive: Git Push Origin Main")
	}

	// Uppercase pattern
	policies2 := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "GIT PUSH*", Action: configuration.CommandPolicyDeny},
		},
	}
	action, _, matched = EvaluateCommandPolicy("git push origin main", policies2)
	if !matched {
		t.Fatal("uppercase pattern should match lowercase command")
	}
	if action != configuration.CommandPolicyDeny {
		t.Errorf("action = %s, want deny", action)
	}
}

func TestEvaluateCommandPolicy_NoMatch(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "kubectl delete*", Action: configuration.CommandPolicyDeny},
		},
	}

	_, _, matched := EvaluateCommandPolicy("git push origin main", policies)
	if matched {
		t.Fatal("no rule should match 'git push origin main'")
	}
}

func TestEvaluateCommandPolicy_ChainedDenyWins(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "kubectl delete*", Action: configuration.CommandPolicyDeny},
		},
	}

	action, _, matched := EvaluateCommandPolicy("echo hi && kubectl delete pod mypod", policies)
	if !matched {
		t.Fatal("should match the kubectl subcommand")
	}
	if action != configuration.CommandPolicyDeny {
		t.Errorf("action = %s, want deny (deny wins)", action)
	}
}

func TestEvaluateCommandPolicy_ChainedAskWinsOverAllow(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo*", Action: configuration.CommandPolicyAllow},
			{Pattern: "git push*", Action: configuration.CommandPolicyAsk},
		},
	}

	action, _, matched := EvaluateCommandPolicy("echo hi && git push", policies)
	if !matched {
		t.Fatal("should match subcommands")
	}
	if action != configuration.CommandPolicyAsk {
		t.Errorf("action = %s, want ask (ask > allow)", action)
	}
}

func TestEvaluateCommandPolicy_ChainedAllAllow(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo*", Action: configuration.CommandPolicyAllow},
		},
	}

	action, _, matched := EvaluateCommandPolicy("echo a && echo b", policies)
	if !matched {
		t.Fatal("should match echo subcommands")
	}
	if action != configuration.CommandPolicyAllow {
		t.Errorf("action = %s, want allow", action)
	}
}

func TestEvaluateCommandPolicy_FirstMatchWins(t *testing.T) {
	// Two rules match the same command; first rule wins.
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "git push*", Action: configuration.CommandPolicyAllow, Reason: "first rule"},
			{Pattern: "git push*", Action: configuration.CommandPolicyDeny, Reason: "second rule"},
		},
	}

	action, pattern, matched := EvaluateCommandPolicy("git push origin main", policies)
	if !matched {
		t.Fatal("should match")
	}
	if action != configuration.CommandPolicyAllow {
		t.Errorf("action = %s, want allow (first match wins)", action)
	}
	if pattern != "git push*" {
		t.Errorf("pattern = %s, want 'git push*'", pattern)
	}
}

func TestEvaluateCommandPolicy_DenyWinsOverAllowInChained(t *testing.T) {
	// echo matches allow, kubectl matches deny → deny wins
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo*", Action: configuration.CommandPolicyAllow},
			{Pattern: "kubectl delete*", Action: configuration.CommandPolicyDeny},
		},
	}

	action, _, matched := EvaluateCommandPolicy("echo hi && kubectl delete pod mypod", policies)
	if !matched {
		t.Fatal("should match")
	}
	if action != configuration.CommandPolicyDeny {
		t.Errorf("action = %s, want deny (deny > allow)", action)
	}
}

func TestEvaluateCommandPolicy_QuotedSeparatorsNotSplit(t *testing.T) {
	// The && inside quotes should NOT cause splitting.
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo \"a && b\"", Action: configuration.CommandPolicyAllow},
		},
	}

	action, _, matched := EvaluateCommandPolicy(`echo "a && b"`, policies)
	if !matched {
		t.Fatal("quoted && should not split, so the full command should match the pattern")
	}
	if action != configuration.CommandPolicyAllow {
		t.Errorf("action = %s, want allow", action)
	}
}

func TestEvaluateCommandPolicy_AllActions(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo*", Action: configuration.CommandPolicyAllow},
			{Pattern: "git push*", Action: configuration.CommandPolicyAsk},
			{Pattern: "kubectl delete*", Action: configuration.CommandPolicyDeny},
		},
	}

	// Allow
	action, _, matched := EvaluateCommandPolicy("echo hello", policies)
	if !matched || action != configuration.CommandPolicyAllow {
		t.Errorf("echo: matched=%v action=%s, want true/allow", matched, action)
	}

	// Ask
	action, _, matched = EvaluateCommandPolicy("git push origin main", policies)
	if !matched || action != configuration.CommandPolicyAsk {
		t.Errorf("git push: matched=%v action=%s, want true/ask", matched, action)
	}

	// Deny
	action, _, matched = EvaluateCommandPolicy("kubectl delete pod mypod", policies)
	if !matched || action != configuration.CommandPolicyDeny {
		t.Errorf("kubectl delete: matched=%v action=%s, want true/deny", matched, action)
	}
}

func TestEvaluateCommandPolicy_SeverityOrdering(t *testing.T) {
	// Verify the full severity ordering: deny > ask > allow
	// Each subcommand matches a different action; highest severity wins.
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo*", Action: configuration.CommandPolicyAllow},
			{Pattern: "git push*", Action: configuration.CommandPolicyAsk},
			{Pattern: "kubectl delete*", Action: configuration.CommandPolicyDeny},
		},
	}

	// Three subcommands, each matching a different action → deny wins
	action, _, matched := EvaluateCommandPolicy(
		"echo hi && git push && kubectl delete pod x", policies,
	)
	if !matched {
		t.Fatal("should match")
	}
	if action != configuration.CommandPolicyDeny {
		t.Errorf("action = %s, want deny (deny > ask > allow)", action)
	}

	// Two subcommands: allow + ask → ask wins
	action, _, matched = EvaluateCommandPolicy(
		"echo hi && git push", policies,
	)
	if !matched || action != configuration.CommandPolicyAsk {
		t.Errorf("action = %s, want ask (ask > allow)", action)
	}
}

func TestEvaluateCommandPolicy_GlobQuestionMark(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "git push ?", Action: configuration.CommandPolicyAsk},
		},
	}

	// Single char after "git push "
	action, _, matched := EvaluateCommandPolicy("git push x", policies)
	if !matched || action != configuration.CommandPolicyAsk {
		t.Errorf("single-char: matched=%v action=%s, want true/ask", matched, action)
	}

	// Two chars should NOT match ?
	_, _, matched = EvaluateCommandPolicy("git push ab", policies)
	if matched {
		t.Error("'?' should not match two characters")
	}
}

func TestEvaluateCommandPolicy_GlobCharacterClass(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "git pu[sd]h*", Action: configuration.CommandPolicyAsk},
		},
	}

	// "push" matches [sd]
	action, _, matched := EvaluateCommandPolicy("git push origin", policies)
	if !matched || action != configuration.CommandPolicyAsk {
		t.Errorf("push: matched=%v action=%s, want true/ask", matched, action)
	}

	// "pudh" matches [sd]
	action, _, matched = EvaluateCommandPolicy("git pudh origin", policies)
	if !matched || action != configuration.CommandPolicyAsk {
		t.Errorf("pudh: matched=%v action=%s, want true/ask", matched, action)
	}

	// "pull" does not match [sd]
	_, _, matched = EvaluateCommandPolicy("git pull origin", policies)
	if matched {
		t.Error("'pull' should not match 'pu[sd]h*'")
	}
}

func TestEvaluateCommandPolicy_InvalidGlobPattern(t *testing.T) {
	// Invalid glob patterns are skipped, not treated as errors.
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "[invalid", Action: configuration.CommandPolicyDeny}, // unclosed bracket
			{Pattern: "git push*", Action: configuration.CommandPolicyAsk},
		},
	}

	// The invalid pattern is skipped; the valid one matches.
	action, _, matched := EvaluateCommandPolicy("git push origin", policies)
	if !matched || action != configuration.CommandPolicyAsk {
		t.Errorf("action = %s, want ask (invalid pattern skipped)", action)
	}
}

func TestEvaluateCommandPolicy_MultipleSeparators(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "kubectl delete*", Action: configuration.CommandPolicyDeny},
		},
	}

	// Semicolon separator
	action, _, matched := EvaluateCommandPolicy("echo hi; kubectl delete pod mypod", policies)
	if !matched || action != configuration.CommandPolicyDeny {
		t.Errorf("semicolon: action = %s, want deny", action)
	}

	// Pipe separator
	action, _, matched = EvaluateCommandPolicy("echo hi | cat; kubectl delete pod mypod", policies)
	if !matched || action != configuration.CommandPolicyDeny {
		t.Errorf("pipe: action = %s, want deny", action)
	}

	// Double pipe separator
	action, _, matched = EvaluateCommandPolicy("echo hi || kubectl delete pod mypod", policies)
	if !matched || action != configuration.CommandPolicyDeny {
		t.Errorf("double pipe: action = %s, want deny", action)
	}
}

func TestEvaluateCommandPolicy_WhitespaceHandling(t *testing.T) {
	policies := &configuration.CommandPolicies{
		Rules: []configuration.CommandRule{
			{Pattern: "echo hi", Action: configuration.CommandPolicyAllow},
		},
	}

	// Extra whitespace in command should still match
	action, _, matched := EvaluateCommandPolicy("  echo hi  ", policies)
	if !matched || action != configuration.CommandPolicyAllow {
		t.Errorf("trimmed: action = %s, want allow", action)
	}
}
