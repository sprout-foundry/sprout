//go:build !js

package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/service"
)

// =============================================================================
// agent_modes.go — isServiceMode
// =============================================================================

func TestIsServiceMode(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv() — runs sequentially.
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset", "", false},
		{"set to 1", "1", true},
		{"set to 0", "0", false},
		{"set to true", "true", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GetEnvSimple checks SPROUT_<name> then LEDIT_<name>
			t.Setenv("SPROUT_SERVICE", tt.value)
			t.Setenv("LEDIT_SERVICE", "")
			if got := isServiceMode(); got != tt.want {
				t.Errorf("isServiceMode(SPROUT_SERVICE=%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// =============================================================================
// agent_modes.go — agentFooterSource methods
// =============================================================================

func TestAgentFooterSource_NilReceiver(t *testing.T) {
	t.Parallel()
	var s *agentFooterSource

	if got := s.Model(); got != "" {
		t.Errorf("nil.Model() = %q, want empty", got)
	}
	used, limit := s.ContextTokens()
	if used != 0 || limit != 0 {
		t.Errorf("nil.ContextTokens() = %d, %d, want 0, 0", used, limit)
	}
	if got := s.TotalCost(); got != 0 {
		t.Errorf("nil.TotalCost() = %f, want 0", got)
	}
	// Note: QueuedMessages() has no nil-receiver guard — it panics.
	// (Existing code bug noted in agent_modes_test.go)
	// ActiveSubagents calls the package-level function; verify no panic.
	_ = s.ActiveSubagents()
	// WorkingDir calls os.Getwd() and doesn't use s — may return empty in some envs.
	_ = s.WorkingDir()
}

func TestAgentFooterSource_NilAgent(t *testing.T) {
	t.Parallel()
	s := &agentFooterSource{agent: nil}

	if got := s.Model(); got != "" {
		t.Errorf("nil agent Model() = %q, want empty", got)
	}
	used, limit := s.ContextTokens()
	if used != 0 || limit != 0 {
		t.Errorf("nil agent ContextTokens() = %d, %d, want 0, 0", used, limit)
	}
	if got := s.TotalCost(); got != 0 {
		t.Errorf("nil agent TotalCost() = %f, want 0", got)
	}
	if got := s.QueuedMessages(); got != 0 {
		t.Errorf("nil agent QueuedMessages() = %d, want 0", got)
	}
}

func TestAgentFooterSource_WithAgent(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	s := &agentFooterSource{agent: a}

	// Model() should return the agent's model (may be empty for test provider)
	model := s.Model()
	if model == "" {
		t.Log("Model() returned empty (expected with test provider)")
	}

	// ContextTokens() should return non-negative values
	used, limit := s.ContextTokens()
	if used < 0 || limit < 0 {
		t.Errorf("ContextTokens() returned negative values: used=%d, limit=%d", used, limit)
	}

	// TotalCost() should return non-negative value
	cost := s.TotalCost()
	if cost < 0 {
		t.Errorf("TotalCost() returned negative value: %f", cost)
	}

	// WorkingDir() should return the current directory
	wd := s.WorkingDir()
	if wd == "" {
		t.Error("WorkingDir() returned empty string")
	}

	// ActiveSubagents() should return a non-negative number
	sub := s.ActiveSubagents()
	if sub < 0 {
		t.Errorf("ActiveSubagents() returned negative value: %d", sub)
	}

	// QueuedMessages() should return a non-negative number
	qm := s.QueuedMessages()
	if qm < 0 {
		t.Errorf("QueuedMessages() returned negative value: %d", qm)
	}
}

// =============================================================================
// agent_modes.go — sanitizeArgForPreview
// =============================================================================

func TestSanitizeArgForPreview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello", "hello"},
		{"newlines collapsed", "hello\nworld", "hello world"},
		{"carriage return", "hello\rworld", "hello world"},
		{"tabs collapsed", "hello\tworld", "hello world"},
		{"multiple spaces preserved", "hello  world", "hello  world"},
		{"leading trailing whitespace", "  hello world  ", "hello world"},
		{"mixed whitespace", "\n  hello \t world \r ", "hello  world"},
		{"control chars stripped", "hello\x00\x01world", "helloworld"},
		{"tab then space", "a\t b", "a  b"},
		{"only control chars", "\n\t\r\x00", ""},
		{"preserves non-ascii", "日本語 🚀", "日本語 🚀"},
		{"consecutive tabs", "a\t\t\tb", "a b"},
		{"newline space", "a\n b", "a  b"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cliui.SanitizeArgForPreview(tt.in); got != tt.want {
				t.Errorf("cliui.SanitizeArgForPreview(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// =============================================================================
// service_env.go — service.MatchesAPIKeyPattern
// =============================================================================

func TestMatchesAPIKeyPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
		want bool
	}{
		// Suffix patterns
		{"_API_KEY suffix", "MY_API_KEY", true},
		{"_TOKEN suffix", "GITHUB_TOKEN", true},
		{"_SECRET suffix", "APP_SECRET", true},
		{"_ACCESS_KEY suffix", "AWS_ACCESS_KEY", true},
		{"_SECRET_KEY suffix", "AWS_SECRET_KEY", true},
		// Prefix patterns
		{"SPROUT_PROVIDER prefix", "SPROUT_PROVIDER", true},
		{"SPROUT_SUBAGENT_PROVIDER prefix", "SPROUT_SUBAGENT_PROVIDER", true},
		{"SPROUT_SUBAGENT_MODEL prefix", "SPROUT_SUBAGENT_MODEL", true},
		// Case insensitive
		{"lowercase api_key", "my_api_key", true},
		{"lowercase sprout_provider", "sprout_provider", true},
		// Non-matching
		{"simple var", "HOME", false},
		{"path var", "PATH", false},
		{"no match", "FOO_BAR", false},
		{"empty", "", false},
		{"_API_KEY exact", "_API_KEY", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := service.MatchesAPIKeyPattern(tt.key); got != tt.want {
				t.Errorf("service.MatchesAPIKeyPattern(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// =============================================================================
// service_env.go — service.ServiceEnvPath
// =============================================================================

func TestServiceEnvPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		homeDir string
		want    string
	}{
		{"simple", "/home/user", "/home/user/.sprout/service.env"},
		{"nested", "/Users/alanp/dev", "/Users/alanp/dev/.sprout/service.env"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := service.ServiceEnvPath(tt.homeDir); got != tt.want {
				t.Errorf("service.ServiceEnvPath(%q) = %q, want %q", tt.homeDir, got, tt.want)
			}
		})
	}
}

// =============================================================================
// service_env.go — service.LoadServiceEnvFile
// =============================================================================

func TestLoadServiceEnvFile_Missing(t *testing.T) {
	t.Parallel()
	m, err := service.LoadServiceEnvFile("/tmp/nonexistent_sprout_dir_" + t.Name())
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map for missing file, got %d entries", len(m))
	}
}

func TestLoadServiceEnvFile_WithContent(t *testing.T) {
	dir := t.TempDir()
	content := "# comment line\nMY_API_KEY=secret123\nSPROUT_PROVIDER=openai\n\nBADLINE_WITHOUT_EQUALS\n"
	path := dir + "/.sprout"
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"/service.env", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := service.LoadServiceEnvFile(dir)
	if err != nil {
		t.Fatalf("service.LoadServiceEnvFile() error: %v", err)
	}
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(m), m)
	}
	if m["MY_API_KEY"] != "secret123" {
		t.Errorf("MY_API_KEY = %q, want %q", m["MY_API_KEY"], "secret123")
	}
	if m["SPROUT_PROVIDER"] != "openai" {
		t.Errorf("SPROUT_PROVIDER = %q, want %q", m["SPROUT_PROVIDER"], "openai")
	}
}

// =============================================================================
// first_run_hint.go — firstRunStatePath
// =============================================================================

func TestFirstRunStatePath(t *testing.T) {
	t.Parallel()
	path, err := firstRunStatePath()
	if err != nil {
		t.Fatalf("firstRunStatePath() error: %v", err)
	}
	if !strings.HasSuffix(path, "state.json") {
		t.Errorf("firstRunStatePath() = %q, expected to end with state.json", path)
	}
}

// =============================================================================
// lockingWriter — concurrent writes
// =============================================================================

func TestLockingWriter_Concurrent(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	buf := new(bytes.Buffer)
	w := lockingWriter{buf: buf, mu: &mu}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w.Write([]byte(fmt.Sprintf("msg-%d", n)))
		}(i)
	}
	wg.Wait()

	got := buf.String()
	for i := 0; i < 10; i++ {
		if !strings.Contains(got, fmt.Sprintf("msg-%d", i)) {
			t.Errorf("missing msg-%d in output: %q", i, got)
		}
	}
}

// =============================================================================
// first_run_hint.go — saveFirstRunState and loadFirstRunState
// =============================================================================

func TestFirstRunStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"

	state := &sproutState{
		SeenFirstRunHint: []string{"/home/user/project"},
	}
	if err := saveFirstRunState(path, state); err != nil {
		t.Fatalf("saveFirstRunState() error: %v", err)
	}

	loaded, err := loadFirstRunState(path)
	if err != nil {
		t.Fatalf("loadFirstRunState() error: %v", err)
	}
	if len(loaded.SeenFirstRunHint) != 1 {
		t.Fatalf("expected 1 seen hint, got %d", len(loaded.SeenFirstRunHint))
	}
	if loaded.SeenFirstRunHint[0] != "/home/user/project" {
		t.Errorf("expected /home/user/project, got %q", loaded.SeenFirstRunHint[0])
	}
}

func TestLoadFirstRunState_Missing(t *testing.T) {
	_, err := loadFirstRunState("/tmp/nonexistent_file_" + t.Name())
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// =============================================================================
// agent_modes.go — shouldShowTurnStats (already tested but add coverage)
// =============================================================================

func TestNoteFirstStreamChunk(t *testing.T) {
	t.Parallel()
	// These functions use package-level atomic state.
	// Verify they don't panic.
	cliui.NoteFirstStreamChunk()
	cliui.ResetTurnFirstToken()
}

// =============================================================================
// agent_modes.go — formatTurnStatsLine via existing tests,
// but verify compactCost $0.0000 for zero value
// =============================================================================

func TestCompactCost_ZeroValue(t *testing.T) {
	t.Parallel()
	got := cliui.CompactCost(0)
	// CompactCost uses "$0.0000" for values < 0.01 (including 0)
	if got != "$0.0000" {
		t.Errorf("cliui.CompactCost(0) = %q, want %q", got, "$0.0000")
	}
}
