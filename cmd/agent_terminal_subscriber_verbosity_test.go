package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// TestTerminalSubscriber_VerbosityLiveRead is a regression test for the
// bug where output_verbosity was read once at startup and cached in the
// subscriber state as a plain string. Changing verbosity via /settings
// mid-session had no effect until the user restarted sprout.
//
// The fix: the subscriber stores the *configuration.Manager and reads
// cfg.OutputVerbosity live on every event via isCompact(). This test
// flips the config between "default" and "compact" and verifies the
// subscriber reflects the change without reconstruction.
func TestTerminalSubscriber_VerbosityLiveRead(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	// Start in default mode — not compact.
	state := newTerminalSubscriberState(mgr)
	if state.isCompact() {
		t.Fatal("isCompact() = true with default config; want false")
	}

	// Flip to compact mid-session (simulates /settings output_verbosity compact).
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityCompact
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	// The SAME subscriber state should now report compact — no restart,
	// no reconstruction needed.
	if !state.isCompact() {
		t.Fatal("isCompact() = false after setting compact; the subscriber did not pick up the live config change")
	}

	// Flip back to default.
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityDefault
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}
	if state.isCompact() {
		t.Fatal("isCompact() = true after reverting to default")
	}
}

// TestTerminalSubscriber_NilConfigManagerIsNonCompact verifies the nil
// fallback: non-agent callers (tests, nil chatAgent) pass a nil config
// manager. isCompact() must return false (non-compact) rather than
// panicking, so the subscriber renders normally.
func TestTerminalSubscriber_NilConfigManagerIsNonCompact(t *testing.T) {
	state := newTerminalSubscriberState(nil)
	if state.isCompact() {
		t.Fatal("isCompact() = true with nil config manager; want false")
	}
}

// TestTerminalSubscriber_VerboseLiveRead mirrors the compact live-read
// test for the new isVerbose() helper. Verifying that a mid-session
// /settings change to "verbose" is picked up without a restart, and
// that verboseMaxArgLen() returns the bumped width.
func TestTerminalSubscriber_VerboseLiveRead(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	state := newTerminalSubscriberState(mgr)
	if state.isVerbose() {
		t.Fatal("isVerbose() = true with default config; want false")
	}
	if w := state.verboseMaxArgLen(); w != 0 {
		t.Fatalf("verboseMaxArgLen() = %d in default mode; want 0", w)
	}

	// Flip to verbose mid-session.
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityVerbose
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	if !state.isVerbose() {
		t.Fatal("isVerbose() = false after setting verbose; the subscriber did not pick up the live config change")
	}
	if w := state.verboseMaxArgLen(); w != verbosePreviewWidth {
		t.Fatalf("verboseMaxArgLen() = %d in verbose mode; want %d", w, verbosePreviewWidth)
	}

	// Flip back to default.
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityDefault
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}
	if state.isVerbose() {
		t.Fatal("isVerbose() = true after reverting to default; want false")
	}
	if w := state.verboseMaxArgLen(); w != 0 {
		t.Fatalf("verboseMaxArgLen() = %d in default mode; want 0", w)
	}
}

// TestTerminalSubscriber_NilConfigManagerIsNonVerbose verifies the nil
// fallback for isVerbose: a nil config manager must return false rather
// than panicking, so the subscriber renders in default detail.
func TestTerminalSubscriber_NilConfigManagerIsNonVerbose(t *testing.T) {
	state := newTerminalSubscriberState(nil)
	if state.isVerbose() {
		t.Fatal("isVerbose() = true with nil config manager; want false")
	}
	if w := state.verboseMaxArgLen(); w != 0 {
		t.Fatalf("verboseMaxArgLen() = %d with nil manager; want 0", w)
	}
}

// TestFormatResultSize verifies the human-readable result-size formatter
// used by verbose mode to append a dim "· 1.2KB" / "· 450 chars" suffix
// to tool-end lines.
func TestFormatResultSize(t *testing.T) {
	cases := []struct {
		name string
		len  int
		want string
	}{
		{"zero", 0, ""},
		{"negative", -5, ""},
		{"small", 100, "100 chars"},
		{"boundary 999", 999, "999 chars"},
		{"boundary 1000", 1000, "1.0KB"},
		{"1500 chars", 1500, "1.5KB"},
		{"large", 12345, "12.1KB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatResultSize(c.len)
			if got != c.want {
				t.Errorf("formatResultSize(%d) = %q; want %q", c.len, got, c.want)
			}
		})
	}
}

// TestFormatCostSummary verifies the turn-summary cost formatter.
func TestFormatCostSummary(t *testing.T) {
	cases := []struct {
		cost float64
		want string
	}{
		{0.0421, "$0.0421"},
		{0.999, "$0.9990"},
		{1.0, "$1.00"},
		{12.34, "$12.34"},
		{0, "$0.0000"},
	}
	for _, c := range cases {
		got := formatCostSummary(c.cost)
		if got != c.want {
			t.Errorf("formatCostSummary(%.4f) = %q; want %q", c.cost, got, c.want)
		}
	}
}

// TestHandleQueryCompletedEvent verifies the CLI-UX-7 turn-end summary:
//   - In non-compact mode it writes a "turn complete" line to stderr.
//   - In compact mode it produces no output (early return).
//   - The summary includes duration and cost.
func TestHandleQueryCompletedEvent(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	state := newTerminalSubscriberState(mgr)

	// --- Non-compact (default) mode: should produce output ---
	var stderrBuf bytes.Buffer
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	indicator := console.NewActivityIndicator(&bytes.Buffer{})

	state.handleQueryCompletedEvent(map[string]interface{}{
		"duration_ms": int64(12300),
		"cost":        float64(0.0421),
	}, indicator)

	os.Stderr = oldStderr
	w.Close()
	io.Copy(&stderrBuf, r)

	output := stderrBuf.String()
	if !strings.Contains(output, "turn complete") {
		t.Errorf("non-compact mode: expected 'turn complete' in output, got: %q", output)
	}
	if !strings.Contains(output, "12.3s") {
		t.Errorf("non-compact mode: expected '12.3s' in output, got: %q", output)
	}
	if !strings.Contains(output, "$0.0421") {
		t.Errorf("non-compact mode: expected cost '$0.0421' in output, got: %q", output)
	}

	// --- Compact mode: should produce no output ---
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityCompact
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	var stderrBufCompact bytes.Buffer
	oldStderr2 := os.Stderr
	r2, w2, _ := os.Pipe()
	os.Stderr = w2

	state.handleQueryCompletedEvent(map[string]interface{}{
		"duration_ms": int64(12300),
		"cost":        float64(0.0421),
	}, indicator)

	os.Stderr = oldStderr2
	w2.Close()
	io.Copy(&stderrBufCompact, r2)

	compactOutput := stderrBufCompact.String()
	if compactOutput != "" {
		t.Errorf("compact mode: expected no output, got: %q", compactOutput)
	}
}

// TestHandleQueryStartedEvent verifies the CLI-UX-5 thinking indicator:
//   - In non-compact mode with an inactive indicator, it starts the
//     thinking spinner and sets thinkingActive.
//   - In compact mode it does nothing.
func TestHandleQueryStartedEvent(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	state := newTerminalSubscriberState(mgr)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})

	// Non-compact mode: indicator is inactive so thinking should start.
	state.handleQueryStartedEvent(indicator)
	if !state.thinkingActive {
		t.Fatal("non-compact mode: thinkingActive should be true after query_started with inactive indicator")
	}

	// Compact mode: should not start thinking.
	state.thinkingActive = false
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityCompact
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	state.handleQueryStartedEvent(indicator)
	if state.thinkingActive {
		t.Fatal("compact mode: thinkingActive should remain false")
	}
}

// TestHandleStreamChunkEvent_StopsThinking verifies that a StreamChunk
// with content_type clears the thinkingActive flag (CLI-UX-5). When
// thinkingActive is true, the chunk handler stops the indicator.
func TestHandleStreamChunkEvent_StopsThinking(t *testing.T) {
	state := newTerminalSubscriberState(nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	state.thinkingActive = true

	state.handleStreamChunkEvent(map[string]interface{}{
		"chunk":        "Hello",
		"content_type": "text",
	}, indicator)

	if state.thinkingActive {
		t.Fatal("thinkingActive should be false after StreamChunk with content_type")
	}
}

// TestHandleStreamChunkEvent_NoContentTypeDoesNotClear verifies that a
// StreamChunk WITHOUT content_type (e.g. a bare chunk) does not clear
// thinkingActive — only chunks that carry assistant text/reasoning
// should stop the thinking spinner.
func TestHandleStreamChunkEvent_NoContentTypeDoesNotClear(t *testing.T) {
	state := newTerminalSubscriberState(nil)
	indicator := console.NewActivityIndicator(&bytes.Buffer{})
	state.thinkingActive = true

	state.handleStreamChunkEvent(map[string]interface{}{
		"chunk": "bare chunk without content type",
	}, indicator)

	if !state.thinkingActive {
		t.Fatal("thinkingActive should remain true for StreamChunk without content_type")
	}
}

// CLI-UX-11: subagent task description in spawn line
func TestExtractSubagentTask(t *testing.T) {
	tests := []struct {
		name     string
		argsJSON string
		wantDesc string
		wantPsn  string
	}{
		{
			name:     "simple prompt",
			argsJSON: `{"persona":"coder","prompt":"Refactor the auth module"}`,
			wantDesc: "Refactor the auth module",
			wantPsn:  "coder",
		},
		{
			name:     "multi-line prompt takes first line",
			argsJSON: `{"persona":"tester","prompt":"Write tests for auth\nCover edge cases"}`,
			wantDesc: "Write tests for auth",
			wantPsn:  "tester",
		},
		{
			name:     "long prompt truncated",
			argsJSON: `{"persona":"coder","prompt":"This is a very long task description that exceeds the sixty character limit and should be truncated"}`,
			wantDesc: "This is a very long task description that exceeds the sixty…",
			wantPsn:  "coder",
		},
		{
			name:     "empty prompt",
			argsJSON: `{"persona":"coder","prompt":""}`,
			wantDesc: "",
			wantPsn:  "coder",
		},
		{
			name:     "invalid json",
			argsJSON: `{bad json}`,
			wantDesc: "",
			wantPsn:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desc, psn := extractSubagentTask(tt.argsJSON)
			if desc != tt.wantDesc {
				t.Errorf("desc = %q, want %q", desc, tt.wantDesc)
			}
			if psn != tt.wantPsn {
				t.Errorf("persona = %q, want %q", psn, tt.wantPsn)
			}
		})
	}
}

func TestFormatSpawnLine_WithTaskDescription(t *testing.T) {
	line := formatSpawnLine(nil, 1, "coder", 0, "Refactoring auth.go")
	if !strings.Contains(line, "coder") {
		t.Errorf("expected persona in line, got %q", line)
	}
	if !strings.Contains(line, "Refactoring auth.go") {
		t.Errorf("expected task description in line, got %q", line)
	}
}

func TestFormatSpawnLine_WithoutTaskDescription(t *testing.T) {
	line := formatSpawnLine(nil, 1, "coder", 0, "")
	if strings.Contains(line, ": ") {
		t.Errorf("should not have task suffix when empty, got %q", line)
	}
}

// CLI-UX-3: diffstat for file-editing tools
func TestComputeDiffStat_EditFile(t *testing.T) {
	args := `{"path":"foo.go","old_str":"old line\nsecond","new_str":"new line"}`
	got := computeDiffStat("edit_file", args)
	if !strings.Contains(got, "+1") {
		t.Errorf("expected +1 for 1-line new_str, got %q", got)
	}
	if !strings.Contains(got, "-2") {
		t.Errorf("expected -2 for 2-line old_str, got %q", got)
	}
}

func TestComputeDiffStat_WriteFile(t *testing.T) {
	args := `{"path":"new.go","content":"line1\nline2\nline3"}`
	got := computeDiffStat("write_file", args)
	if !strings.Contains(got, "+3") {
		t.Errorf("expected +3 for 3-line content, got %q", got)
	}
}

func TestComputeDiffStat_NonFileTool(t *testing.T) {
	got := computeDiffStat("shell_command", `{"command":"ls"}`)
	if got != "" {
		t.Errorf("expected empty for non-file tool, got %q", got)
	}
}

func TestComputeDiffStat_EmptyArgs(t *testing.T) {
	got := computeDiffStat("edit_file", "")
	if got != "" {
		t.Errorf("expected empty for empty args, got %q", got)
	}
}

func TestFormatCompactDiffLine(t *testing.T) {
	args := `{"path":"webui/src/components/Foo.tsx","old_str":"a","new_str":"b"}`
	got := formatCompactDiffLine("edit_file", args, "+1 -1")
	if !strings.Contains(got, "Foo.tsx") {
		t.Errorf("expected filename in compact diff line, got %q", got)
	}
	if !strings.Contains(got, "+1 -1") {
		t.Errorf("expected diffstat in compact diff line, got %q", got)
	}
}
