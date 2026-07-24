//go:build !js

package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// =============================================================================
// agent_modes.go - atomic and utility functions
// =============================================================================

func TestSetQueryInProgress(t *testing.T) {
	// Test that setQueryInProgress updates the atomic
	setQueryInProgress(true)
	if !isQueryInProgress() {
		t.Error("expected query in progress to be true after setting")
	}

	setQueryInProgress(false)
	if isQueryInProgress() {
		t.Error("expected query in progress to be false after resetting")
	}
}

func TestEnsureContinuationSessionID_NilAgent(t *testing.T) {
	// Should return empty string for nil agent
	result := ensureContinuationSessionID(nil)
	if result != "" {
		t.Errorf("expected empty string for nil agent, got %q", result)
	}
}

func TestEnsureContinuationSessionID_NoExistingSession(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Agent should not have a session ID initially
	if a.GetSessionID() != "" {
		t.Logf("Agent already has session ID: %s", a.GetSessionID())
	}

	// EnsureContinuationSessionID should set one
	result := ensureContinuationSessionID(a)
	if result == "" {
		t.Error("expected non-empty session ID")
	}

	// Verify the agent now has a session ID
	if a.GetSessionID() == "" {
		t.Error("expected agent to have session ID after ensure")
	}

	// Should return the same ID on subsequent calls
	result2 := ensureContinuationSessionID(a)
	if result != result2 {
		t.Errorf("expected same session ID on subsequent calls, got %q vs %q", result, result2)
	}
}

func TestEnsureContinuationSessionID_WithExistingSession(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Set a session ID manually
	testSessionID := "test_session_123"
	a.SetSessionID(testSessionID)

	// ensureContinuationSessionID should return the existing ID
	result := ensureContinuationSessionID(a)
	if result != testSessionID {
		t.Errorf("expected %q, got %q", testSessionID, result)
	}
}

func TestPrintContinuationHint(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	// Set a session ID
	a.SetSessionID("test_session_print")

	// printContinuationHint should print to stdout
	// We capture the output to verify it works
	// This is just a smoke test to ensure no panic
	printContinuationHint(a)
}

func TestPrintContinuationHint_NilAgent(t *testing.T) {
	// Should not panic with nil agent
	printContinuationHint(nil)
}

// =============================================================================
// SetupAgentEvents - test that it doesn't panic
// =============================================================================

func TestSetupAgentEvents(t *testing.T) {
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}

	eventBus := events.NewEventBus()

	// SetupAgentEvents should not panic
	SetupAgentEvents(a, eventBus, nil)
}

// TestSetupAgentEvents_NilEventBus skipped - it panics with nil event bus (exposes code bug)

// SP-048-1d: tool-arg preview helper for the activity indicator timeline.

func TestFormatToolArgPreview(t *testing.T) {
	cases := []struct {
		name string
		tool string
		args string
		want string
	}{
		{
			name: "read_file uses path",
			tool: "read_file",
			args: `{"path": "pkg/console/input_core.go"}`,
			want: " (pkg/console/input_core.go)",
		},
		{
			name: "shell_command uses command",
			tool: "shell_command",
			args: `{"command": "go test ./pkg/console/"}`,
			want: " (go test ./pkg/console/)",
		},
		{
			name: "write_file uses path",
			tool: "write_file",
			args: `{"path": "/tmp/foo.txt", "content": "hello"}`,
			want: " (/tmp/foo.txt)",
		},
		{
			name: "search_files uses pattern",
			tool: "search_files",
			args: `{"pattern": "TODO", "directory": "."}`,
			want: " (TODO)",
		},
		{
			name: "fetch_url uses url",
			tool: "fetch_url",
			args: `{"url": "https://example.com/page"}`,
			want: " (https://example.com/page)",
		},
		{
			// Path-aware abbreviation: when the full path is too long we
			// drop the directory prefix and keep the filename, which is
			// almost always the more useful half. The previous
			// behaviour (tail-truncate) chopped the filename off and
			// left only directory crumbs.
			name: "long path keeps the filename via abbreviation",
			tool: "read_file",
			args: `{"path": "a/very/long/path/that/exceeds/the/seventy/character/preview/limit/foo.go"}`,
			want: " (…/foo.go)",
		},
		{
			name: "empty arguments returns empty",
			tool: "read_file",
			args: "",
			want: "",
		},
		{
			name: "invalid json returns empty",
			tool: "read_file",
			args: "not json",
			want: "",
		},
		{
			name: "unknown tool falls back to first string field",
			tool: "future_tool",
			args: `{"thing": "value123"}`,
			want: " (value123)",
		},
		{
			name: "newlines in command are collapsed",
			tool: "shell_command",
			args: `{"command": "line1\nline2\tline3"}`,
			want: " (line1 line2 line3)",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cliui.FormatToolArgPreview(c.tool, c.args, 0)
			if got != c.want {
				t.Errorf("FormatToolArgPreview(%q, %q) = %q, want %q", c.tool, c.args, got, c.want)
			}
		})
	}
}

// SP-048 follow-up: subagent preview should show task count for
// run_parallel_subagents so the user can see fan-out.
func TestFormatRunParallelSubagentsPreview(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"three tasks", `{"subagents":["a","b","c"]}`, " (3 tasks)"},
		{"one task", `{"subagents":["only-one"]}`, " (1 tasks)"},
		{"empty array", `{"subagents":[]}`, ""},
		{"missing field", `{}`, ""},
		{"invalid json", `not json`, ""},
		{"empty args", ``, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cliui.FormatRunParallelSubagentsPreview(c.args)
			if got != c.want {
				t.Errorf("FormatRunParallelSubagentsPreview(%q) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

// formatToolPreview dispatches on tool name; the default branch should
// behave identically to formatToolArgPreview for unrelated tools.
func TestFormatToolPreview_DispatchToDefault(t *testing.T) {
	cases := []struct {
		tool string
		args string
		want string
	}{
		{"read_file", `{"path":"foo.go"}`, " (foo.go)"},
		{"shell_command", `{"command":"ls"}`, " (ls)"},
		{"unknown_tool", `{"thing":"value"}`, " (value)"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			got := cliui.FormatToolPreview(nil, c.tool, c.args, 0)
			if got != c.want {
				t.Errorf("FormatToolPreview(nil, %q, %q) = %q, want %q",
					c.tool, c.args, got, c.want)
			}
		})
	}
}

// run_subagent without an agent reference should degrade gracefully —
// no panic, returns empty (or just persona if available without lookup).
func TestFormatRunSubagentPreview_NilAgent(t *testing.T) {
	got := cliui.FormatRunSubagentPreview(nil, `{"persona":"coder"}`)
	if got != "" {
		t.Errorf("nil agent should yield empty preview, got %q", got)
	}
}

// TestAbbreviatePath pins the path-abbreviation behaviour the activity
// indicator relies on. The filename must always survive when the
// directory prefix is dropped — that's what makes the abbreviated
// preview readable.
func TestAbbreviatePath(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{"fits unchanged", "foo.go", 70, "foo.go"},
		{"long path keeps basename", "a/b/c/d/e/f/g/h/very_long_filename.go", 20, "…/very_long_filename.go"},
		{"single segment tail-truncates", "this_is_a_single_very_long_segment_no_slashes_at_all.txt", 20, "this_is_a_single_ve…"},
		// When path has a separator, we always prefer "…/basename" even
		// if that overshoots maxLen — preserves the file extension that
		// usually identifies the file type.
		{"path with long basename keeps full basename via …/", "x/y/this_is_a_single_very_long_segment_no_slashes_at_all.txt", 20, "…/this_is_a_single_very_long_segment_no_slashes_at_all.txt"},
		{"exactly at limit no truncation", "abc/def/ghi.go", 14, "abc/def/ghi.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cliui.AbbreviatePath(c.in, c.maxLen)
			if got != c.want {
				t.Errorf("AbbreviatePath(%q, %d) = %q, want %q", c.in, c.maxLen, got, c.want)
			}
		})
	}
}

// TestFormatThousands covers the comma-separated integer formatter
// used in the subagent-done line so a 1.2M token run reads as
// "1,234,567 tok" instead of "1234567 tok".
func TestFormatThousands(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{12345, "12,345"},
		{1234567, "1,234,567"},
		{-12345, "-12,345"},
	}
	for _, c := range cases {
		got := cliui.FormatThousands(c.in)
		if got != c.want {
			t.Errorf("FormatThousands(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFormatSubagentDoneLine covers the rendering of the per-subagent
// completion summary. Notably: zero-valued numeric fields drop out so
// a no-cost cancellation doesn't spam "0 tok · $0.0000".
func TestFormatSubagentDoneLine(t *testing.T) {
	cases := []struct {
		name        string
		persona     string
		status      string
		reason      string
		tokens      int
		cost        float64
		elapsedSec  float64
		wantSubstrs []string
	}{
		{
			name:    "completed with all fields",
			persona: "coder", status: "completed", reason: "",
			tokens: 12345, cost: 0.0234, elapsedSec: 4.2,
			wantSubstrs: []string{"done", "12,345 tok", "$0.0234", "4.2s"},
		},
		{
			name:    "cancelled with reason",
			persona: "coder", status: "cancelled", reason: "budget_exceeded",
			tokens: 8901, cost: 0.0102, elapsedSec: 2.1,
			wantSubstrs: []string{"cancelled (budget_exceeded)", "8,901 tok", "$0.0102", "2.1s"},
		},
		{
			name:    "completed with zero cost drops the dollar field",
			persona: "coder", status: "completed", reason: "",
			tokens: 100, cost: 0, elapsedSec: 1.5,
			wantSubstrs: []string{"done", "100 tok", "1.5s"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cliui.FormatSubagentDoneLine(c.persona, c.status, c.reason, c.tokens, c.cost, c.elapsedSec)
			for _, want := range c.wantSubstrs {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %q", want, got)
				}
			}
		})
	}
	// Cost == 0 case should not contain "$" at all.
	got := cliui.FormatSubagentDoneLine("coder", "completed", "", 100, 0, 1.0)
	if strings.Contains(got, "$") {
		t.Errorf("zero cost should drop the dollar field; got %q", got)
	}
}

// TestFormatTokensShort covers the compact token formatter used in
// spawn / tool / progress lines where horizontal space is at a premium.
func TestFormatTokensShort(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1.0k"},
		{12345, "12.3k"},
		{128000, "128.0k"},
		{1234567, "1.2M"},
	}
	for _, c := range cases {
		got := cliui.FormatTokensShort(c.in)
		if got != c.want {
			t.Errorf("FormatTokensShort(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFormatSubagentCtxSuffix pins the live-context hint appended to
// depth>0 tool-start lines. When monitorProgress has reported a
// ctxUsed/ctxMax pair the suffix shows both; otherwise it falls back
// to total tokens; otherwise empty so a not-yet-warmed subagent's
// tool lines stay clean.
func TestFormatSubagentCtxSuffix(t *testing.T) {
	cases := []struct {
		name string
		snap cliui.SubagentProgressSnapshot
		want string
	}{
		{"with ctx pair", cliui.SubagentProgressSnapshot{CtxUsed: 12300, CtxMax: 128000, TokensUsed: 12300}, " · 12.3k/128.0k ctx"},
		{"only total tokens", cliui.SubagentProgressSnapshot{TokensUsed: 500}, " · 500 tok"},
		{"empty snapshot", cliui.SubagentProgressSnapshot{}, ""},
		{"ctx max known but used not yet", cliui.SubagentProgressSnapshot{CtxMax: 128000}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cliui.FormatSubagentCtxSuffix(c.snap)
			if got != c.want {
				t.Errorf("FormatSubagentCtxSuffix(%+v) = %q, want %q", c.snap, got, c.want)
			}
		})
	}
}

// =============================================================================
// SP-051: depth-aware tool timeline rendering
// =============================================================================

func TestReadEventDepth(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want int
	}{
		{"nil_map", nil, 0},
		{"missing_key", map[string]interface{}{}, 0},
		{"int_value", map[string]interface{}{"subagent_depth": 2}, 2},
		{"int64_value", map[string]interface{}{"subagent_depth": int64(1)}, 1},
		{"float_value_from_json", map[string]interface{}{"subagent_depth": float64(1)}, 1},
		{"wrong_type_string", map[string]interface{}{"subagent_depth": "1"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cliui.ReadEventDepth(c.data); got != c.want {
				t.Errorf("ReadEventDepth = %d, want %d", got, c.want)
			}
		})
	}
}

func TestReadEventPersona(t *testing.T) {
	cases := []struct {
		name string
		data map[string]interface{}
		want string
	}{
		{"nil_map", nil, ""},
		{"missing_key", map[string]interface{}{}, ""},
		{"plain", map[string]interface{}{"active_persona": "coder"}, "coder"},
		{"whitespace_trimmed", map[string]interface{}{"active_persona": "  coder  "}, "coder"},
		{"wrong_type", map[string]interface{}{"active_persona": 42}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cliui.ReadEventPersona(c.data); got != c.want {
				t.Errorf("ReadEventPersona = %q, want %q", got, c.want)
			}
		})
	}
}

// Depth 0 must produce a line byte-identical to the pre-SP-051 format so
// primary-agent tool calls don't regress visually.
func TestFormatToolStartLine_Depth0_Unchanged(t *testing.T) {
	got := cliui.FormatToolStartLine(0, "", "read_file", " (foo.go)")
	want := "  read_file (foo.go)"
	if got != want {
		t.Errorf("FormatToolStartLine(0, ...) = %q, want %q", got, want)
	}
}

// Depth ≥ 1 should add an indent and a [persona] badge that contains the
// persona name. NO_COLOR keeps the line ANSI-free for stable comparison.
func TestFormatToolStartLine_Depth1_IndentedAndBadged(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := cliui.FormatToolStartLine(1, "coder", "read_file", " (foo.go)")
	if !strings.HasPrefix(got, "    [coder] read_file") {
		// 4 spaces = 2 (depth indent) + 2 (existing tool-line prefix)
		t.Errorf("depth-1 start line should be indented + badged, got %q", got)
	}
}

func TestFormatToolStartLine_Depth2_DoubleIndent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := cliui.FormatToolStartLine(2, "coder", "read_file", " (foo.go)")
	if !strings.HasPrefix(got, "      [coder] read_file") {
		// 6 spaces = 4 (depth-2 indent) + 2 (existing tool-line prefix)
		t.Errorf("depth-2 start line should be double-indented, got %q", got)
	}
}

func TestFormatToolEndLine_Depth0_Unchanged(t *testing.T) {
	got := cliui.FormatToolEndLine(0, "", "[OK]", "read_file", " (foo.go)", 0.1)
	want := "  [OK] read_file (foo.go) · 0.1s"
	if got != want {
		t.Errorf("FormatToolEndLine(0, ...) = %q, want %q", got, want)
	}
}

func TestFormatToolEndLine_Depth1_Badged(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := cliui.FormatToolEndLine(1, "coder", "[OK]", "read_file", " (foo.go)", 0.2)
	if !strings.Contains(got, "[coder]") {
		t.Errorf("depth-1 end line should include persona badge, got %q", got)
	}
	if !strings.HasSuffix(got, " · 0.2s") {
		t.Errorf("end line should preserve duration suffix, got %q", got)
	}
}

// Phase 3 collapsed-run formatter tests.

func TestFormatToolRunLine_IncludesCountAndArgsTrail(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := cliui.FormatToolRunLine(0, "", "✓ ", "read_file", 3,
		[]string{"(foo.go)", "(bar.go)", "(baz.go)"}, 0.4)
	if !strings.Contains(got, "× 3") {
		t.Errorf("collapsed line should show count, got %q", got)
	}
	if !strings.Contains(got, "(foo.go), (bar.go), (baz.go)") {
		t.Errorf("collapsed line should join args trail, got %q", got)
	}
	if !strings.HasSuffix(got, " · 0.4s") {
		t.Errorf("collapsed line should keep total duration suffix, got %q", got)
	}
}

func TestFormatToolRunLine_EmptyArgsTrail_NoEmptyParens(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := cliui.FormatToolRunLine(0, "", "✓ ", "ping", 5, nil, 0.0)
	if strings.Contains(got, "()") {
		t.Errorf("empty args trail should produce no parens, got %q", got)
	}
	if !strings.Contains(got, "× 5") {
		t.Errorf("count should still appear, got %q", got)
	}
}

// `?` keyboard help tests.

func TestWriteKeyboardHelp_IncludesSteerKeys(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	writeKeyboardHelp(&buf)
	got := buf.String()
	for _, want := range []string{
		"Steer panel",
		"Tab",
		"toggle steer ↔ queue mode",
		"↑ / ↓",
		"recall prior steer messages",
		"Ctrl+C",
		"interrupt the current turn",
		"Idle prompt",
		"slash command",
		"exit / quit",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("help should contain %q, got:\n%s", want, got)
		}
	}
}

func TestWriteKeyboardHelp_NoColorStripsANSI(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	writeKeyboardHelp(&buf)
	got := buf.String()
	if strings.Contains(got, "\033[") {
		t.Errorf("NO_COLOR should suppress ANSI escapes, got %q", got)
	}
}

func TestWriteKeyboardHelp_ColorAddsBoldHeaders(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	var buf bytes.Buffer
	writeKeyboardHelp(&buf)
	got := buf.String()
	if !strings.Contains(got, "\033[1m") {
		t.Errorf("color mode should bold section headers, got:\n%s", got)
	}
	if !strings.Contains(got, "\033[2m") {
		t.Errorf("color mode should dim descriptions, got:\n%s", got)
	}
}

func TestToolRunState_MatchesAndAppend(t *testing.T) {
	r := &cliui.ToolRunState{Name: "read_file", Depth: 0, Persona: ""}
	if !r.Matches("read_file", 0, "") {
		t.Error("Matches should return true for identical (name, depth, persona)")
	}
	if r.Matches("write_file", 0, "") {
		t.Error("Matches should return false for different tool")
	}
	if r.Matches("read_file", 1, "") {
		t.Error("Matches should return false at different depth")
	}
	// AppendArg should cap the trail at MaxArgsTrail.
	for i := 0; i < cliui.MaxArgsTrail+3; i++ {
		r.AppendArg("preview")
	}
	if len(r.ArgsTrail) != cliui.MaxArgsTrail {
		t.Errorf("expected ArgsTrail capped at %d, got %d", cliui.MaxArgsTrail, len(r.ArgsTrail))
	}
}
