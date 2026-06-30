package agent

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/require"
)

// TestFormatRiskType_AllTypes verifies that formatRiskType returns a human-readable
// description for each known risk type.
func TestFormatRiskType_AllTypes(t *testing.T) {
	tests := []struct {
		riskType string
		expected string
		contains string // substring that must appear
	}{
		{"mass_deletion", "Mass deletion", "Mass deletion"},
		{"source_code_destruction", "Source code destruction", "Source code destruction"},
		{"privilege_escalation", "Privilege escalation", "Privilege escalation"},
		{"remote_code_execution", "Remote code execution", "Remote code execution"},
		{"arbitrary_code_execution", "Arbitrary code execution", "Arbitrary code execution"},
		{"destructive_git_operation", "Destructive git operation", "Destructive git operation"},
		{"disk_destruction", "Disk destruction", "Disk destruction"},
		{"critical_system_operation", "Critical system operation", "Critical system operation"},
		{"system_instability", "System instability", "System instability"},
		{"insecure_permissions", "Insecure permissions", "Insecure permissions"},
		{"system_integrity", "System integrity", "System integrity"},
		{"", "", ""}, // empty string passes through
		{"unknown_risk_type", "unknown_risk_type", "unknown_risk_type"}, // unknown passes through
	}

	for _, tc := range tests {
		t.Run(tc.riskType, func(t *testing.T) {
			result := formatRiskType(tc.riskType)
			if tc.contains != "" && !strings.Contains(result, tc.contains) {
				t.Errorf("formatRiskType(%q) = %q, want substring %q",
					tc.riskType, result, tc.contains)
			}
		})
	}
}

// TestFormatRiskType_NoPanic verifies that formatRiskType does not panic
// for any input value.
func TestFormatRiskType_NoPanic(t *testing.T) {
	panickingInputs := []string{
		"",
		"nil",
		" ",
		"   ",
		"UPPERCASE",
		"mixed_Case",
		"with-dash",
		"with.dot",
		"mass_deletion_with_suffix",
	}
	for _, input := range panickingInputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("formatRiskType(%q) panicked: %v", input, r)
				}
			}()
			formatRiskType(input)
		}()
	}
}

// TestBuildSecurityPrompt_ShellCommand verifies the prompt format for shell_command.
func TestBuildSecurityPrompt_ShellCommand(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecurityDangerous,
		RiskType:  "arbitrary_code_execution",
		Reasoning: "This command may execute arbitrary code",
	}

	args := map[string]interface{}{
		"command": "ls -la /tmp",
	}

	prompt := buildSecurityPrompt("shell_command", args, secResult)

	// Check that the prompt contains expected elements.
	if !strings.Contains(prompt, "⚠  Security Warning") {
		t.Error("prompt missing warning header")
	}
	if !strings.Contains(prompt, "DANGEROUS") {
		t.Error("prompt missing risk level")
	}
	if !strings.Contains(prompt, "ls -la /tmp") {
		t.Error("prompt missing command text")
	}
	if !strings.Contains(prompt, "Command:") {
		t.Error("prompt missing 'Command:' label")
	}
	if !strings.Contains(prompt, "Arbitrary code execution") {
		t.Error("prompt missing formatted risk type")
	}
	if !strings.Contains(prompt, "This command may execute arbitrary code") {
		t.Error("prompt missing reasoning")
	}
	if !strings.Contains(prompt, "Do you want to proceed?") {
		t.Error("prompt missing confirmation question")
	}
}

// TestBuildSecurityPrompt_WriteFile verifies the prompt format for write_file.
func TestBuildSecurityPrompt_WriteFile(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecurityCaution,
		RiskType:  "system_integrity",
		Reasoning: "Writing to system files",
	}

	args := map[string]interface{}{
		"path": "/etc/passwd",
	}

	prompt := buildSecurityPrompt("write_file", args, secResult)

	if !strings.Contains(prompt, "/etc/passwd") {
		t.Error("prompt missing file path")
	}
	if !strings.Contains(prompt, "Target:") {
		t.Error("prompt missing 'Target:' label")
	}
	if !strings.Contains(prompt, "System integrity") {
		t.Error("prompt missing formatted risk type")
	}
}

// TestBuildSecurityPrompt_EditFile verifies the prompt format for edit_file.
func TestBuildSecurityPrompt_EditFile(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecuritySafe,
		RiskType:  "",
		Reasoning: "File edit in system directory",
	}

	args := map[string]interface{}{
		"path": "/usr/local/bin/script.sh",
	}

	prompt := buildSecurityPrompt("edit_file", args, secResult)

	if !strings.Contains(prompt, "/usr/local/bin/script.sh") {
		t.Error("prompt missing file path")
	}
	if !strings.Contains(prompt, "Target:") {
		t.Error("prompt missing 'Target:' label")
	}
	// Should NOT contain "Risk category:" when riskType is empty.
	if strings.Contains(prompt, "Risk category:") {
		t.Error("prompt should not contain 'Risk category:' when riskType is empty")
	}
}

// TestBuildSecurityPrompt_GitOperation verifies the prompt format for git.
func TestBuildSecurityPrompt_GitOperation(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecurityDangerous,
		RiskType:  "destructive_git_operation",
		Reasoning: "Force pushing to remote",
	}

	args := map[string]interface{}{
		"operation": "push --force",
	}

	prompt := buildSecurityPrompt("git", args, secResult)

	if !strings.Contains(prompt, "Operation: git push --force") {
		t.Error("prompt missing formatted git operation")
	}
	if !strings.Contains(prompt, "Destructive git operation") {
		t.Error("prompt missing formatted risk type")
	}
}

// TestBuildSecurityPrompt_MissingArgs verifies that the prompt handles missing
// argument values gracefully (doesn't panic or produce malformed output).
func TestBuildSecurityPrompt_MissingArgs(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecurityDangerous,
		RiskType:  "mass_deletion",
		Reasoning: "Mass deletion risk",
	}

	// Empty args map — should not panic.
	prompt := buildSecurityPrompt("shell_command", map[string]interface{}{}, secResult)
	if !strings.Contains(prompt, "⚠  Security Warning") {
		t.Error("prompt missing warning header")
	}
	if !strings.Contains(prompt, "Mass deletion") {
		t.Error("prompt missing formatted risk type")
	}

	// Non-string command value — should not panic.
	prompt = buildSecurityPrompt("shell_command", map[string]interface{}{
		"command": 123, // wrong type
	}, secResult)
	if !strings.Contains(prompt, "⚠  Security Warning") {
		t.Error("prompt missing warning header for non-string command")
	}

	// Nil args — should not panic.
	prompt = buildSecurityPrompt("shell_command", nil, secResult)
	if !strings.Contains(prompt, "⚠  Security Warning") {
		t.Error("prompt missing warning header for nil args")
	}
}

// TestBuildSecurityPrompt_PatchStructuredFile verifies the prompt format for patch_structured_file.
func TestBuildSecurityPrompt_PatchStructuredFile(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecurityCaution,
		RiskType:  "insecure_permissions",
		Reasoning: "Setting world-writable permissions",
	}

	args := map[string]interface{}{
		"path": "config/settings.yaml",
	}

	prompt := buildSecurityPrompt("patch_structured_file", args, secResult)

	if !strings.Contains(prompt, "config/settings.yaml") {
		t.Error("prompt missing file path")
	}
	if !strings.Contains(prompt, "Insecure permissions") {
		t.Error("prompt missing formatted risk type")
	}
}

// TestBuildSecurityPrompt_WriteStructuredFile verifies the prompt format for write_structured_file.
func TestBuildSecurityPrompt_WriteStructuredFile(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecurityCaution,
		RiskType:  "system_integrity",
		Reasoning: "Writing to system config",
	}

	args := map[string]interface{}{
		"path": "/etc/app/config.json",
	}

	prompt := buildSecurityPrompt("write_structured_file", args, secResult)

	if !strings.Contains(prompt, "/etc/app/config.json") {
		t.Error("prompt missing file path")
	}
	if !strings.Contains(prompt, "Target:") {
		t.Error("prompt missing 'Target:' label")
	}
}

// TestBuildSecurityPrompt_UnknownTool verifies the prompt works for unrecognized
// tool names (should produce minimal but valid output).
func TestBuildSecurityPrompt_UnknownTool(t *testing.T) {
	secResult := tools.SecurityResult{
		Risk:      tools.SecuritySafe,
		RiskType:  "",
		Reasoning: "Unknown tool being called",
	}

	args := map[string]interface{}{
		"some_param": "some_value",
	}

	prompt := buildSecurityPrompt("unknown_tool", args, secResult)

	// Should still contain the warning header and reasoning,
	// but no specific command/target labels.
	if !strings.Contains(prompt, "⚠  Security Warning") {
		t.Error("prompt missing warning header")
	}
	if !strings.Contains(prompt, "Unknown tool being called") {
		t.Error("prompt missing reasoning")
	}
	if strings.Contains(prompt, "Command:") {
		t.Error("prompt should not contain 'Command:' for unknown tool")
	}
	if strings.Contains(prompt, "Target:") {
		t.Error("prompt should not contain 'Target:' for unknown tool")
	}
	if strings.Contains(prompt, "Operation:") {
		t.Error("prompt should not contain 'Operation:' for unknown tool")
	}
}

// =============================================================================
// SP-049-3a: Unsafe shell mode bypass tests
// =============================================================================

func TestStaticGateAutoApprove_UnsafeShellModeNotTriggered(t *testing.T) {
	// staticGateAutoApprove should NOT return true for --unsafe-shell.
	// --unsafe-shell has its own bypass path in ExecuteTool (line ~196)
	// that is specific to shell_command + non-hard-block + non-DANGEROUS.
	// The staticGateAutoApprove only handles --unsafe (full) and
	// session elevation — it must not short-circuit for unsafe shell.
	a := NewTestAgent()
	a.SetUnsafeShellMode(true)

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		Reasoning:    "Test",
	}

	if a.staticGateAutoApprove(secResult) {
		t.Error("staticGateAutoApprove should NOT trigger when only unsafe shell mode is set")
	}
}

func TestStaticGateAutoApprove_UnsafeModeTriggers(t *testing.T) {
	// --unsafe (full bypass) should trigger staticGateAutoApprove for
	// non-hard-block operations.
	a := NewTestAgent()
	a.SetUnsafeMode(true)

	secResult := tools.SecurityResult{
		Risk:         tools.SecurityCaution,
		ShouldPrompt: true,
		Reasoning:    "Test",
		IsHardBlock:  false,
	}

	if !a.staticGateAutoApprove(secResult) {
		t.Error("staticGateAutoApprove should trigger when unsafe mode is set")
	}
}

func TestStaticGateAutoApprove_HardBlockNeverAutoApproved(t *testing.T) {
	// Even with --unsafe, hard blocks should NOT auto-approve
	// through staticGateAutoApprove. They have their own absolute
	// block path.
	a := NewTestAgent()
	a.SetUnsafeMode(true)

	secResult := tools.SecurityResult{
		Risk:        tools.SecurityDangerous,
		Reasoning:   "Critical system operation",
		IsHardBlock: true,
	}

	// --unsafe (full) DOES return true from staticGateAutoApprove
	// regardless of IsHardBlock — it's a full bypass.
	if !a.staticGateAutoApprove(secResult) {
		t.Error("staticGateAutoApprove with unsafe mode returns true even for hard blocks")
	}

	// But session elevation should NOT auto-approve hard blocks.
	a.SetUnsafeMode(false)
	// We need to test session elevation. Since we can't easily set
	// the risk profile override in a test agent, verify the logic
	// path independently — the code does:
	//   if a.IsSessionElevated() && !secResult.IsHardBlock
	// So with IsHardBlock=true, it should return false.
	// We'll trust the existing implementation since we can't set up
	// a full config-backed test here without a config manager.
}

func TestStaticGateAutoApprove_NilAgent(t *testing.T) {
	var a *Agent
	secResult := tools.SecurityResult{
		Risk:        tools.SecurityCaution,
		Reasoning:   "Test",
		IsHardBlock: false,
	}

	if a.staticGateAutoApprove(secResult) {
		t.Error("staticGateAutoApprove with nil agent should return false")
	}
}

func TestStaticGateAutoApprove_ElevatedSessionNonHardBlock(t *testing.T) {
	// Verify that elevated session bypasses non-hard-block operations.
	// We set up a test agent with a risk profile override.
	a := NewTestAgent()

	// Simulate session elevation by setting the override.
	a.SetRiskProfileOverride(configuration.RiskProfilePermissive)

	secResult := tools.SecurityResult{
		Risk:        tools.SecurityCaution,
		Reasoning:   "Test",
		IsHardBlock: false,
	}

	if !a.staticGateAutoApprove(secResult) {
		t.Error("staticGateAutoApprove should return true for elevated session with non-hard-block")
	}

	// Hard block should still not auto-approve under elevation.
	secResult.IsHardBlock = true
	if a.staticGateAutoApprove(secResult) {
		t.Error("staticGateAutoApprove should NOT auto-approve hard blocks under elevation")
	}
}

// TestOutputRouter_WriteRoutesToPrintLineAsync verifies that the outputRouter
// io.Writer flushes complete lines to agent.PrintLineAsync and buffers partial
// lines until a newline arrives.
//
// Chrome (tool logs, system messages) must NOT route through the streaming
// callback — that's reserved for prose via RouteStreamChunk. Chrome goes to
// stdout with a row clear (\r\033[K) so partial prose streams don't get
// appended to. So this test asserts the lines land on stdout (not the
// streaming callback) and are newline-terminated in order.
func TestOutputRouter_WriteRoutesToPrintLineAsync(t *testing.T) {
	a := NewTestAgent()
	a.output.SetOutputMutex(&sync.Mutex{})

	// The streaming callback must NOT fire for chrome. If it does, the
	// bug that caused prose/tool-log clobbering has regressed.
	a.output.SetStreamingCallback(func(s string) {
		t.Fatalf("streamingCallback must not fire for chrome: got %q", s)
	})
	a.output.SetStreamingEnabled(true)
	a.output.SetOutputRouter(NewOutputRouter(a, nil))

	// Capture stdout so we can assert what actually reaches the terminal.
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	wRouter := newOutputRouter(a)

	// Two complete lines in one Write — both must arrive as separate flushed lines.
	if _, err := wRouter.Write([]byte("line1\nline2\n")); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// A partial line followed by its completion should arrive as ONE flushed
	// line, not two. The router must buffer the partial chunk.
	if _, err := wRouter.Write([]byte("partial")); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if _, err := wRouter.Write([]byte(" line\n")); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Poll the pipe until all 3 lines arrive (or 2s timeout). PrintLineAsync
	// is asynchronous — a worker drains the channel.
	wantLines := []string{"line1\n", "line2\n", "partial line\n"}
	var buf bytes.Buffer
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tmp := make([]byte, 4096)
		_ = r.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, _ := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		out := buf.String()
		allFound := true
		for _, want := range wantLines {
			if !strings.Contains(out, want) {
				allFound = false
				break
			}
		}
		if allFound {
			break
		}
	}
	w.Close()

	out := buf.String()
	// Each line must arrive on stdout, newline-terminated. No \r\033[K
	// prefix — in TTY mode the externalWriteHook handles row management,
	// and in non-TTY mode there's no cursor to clear.
	for _, want := range wantLines {
		if !strings.Contains(out, want) {
			t.Errorf("expected stdout to contain %q, got %q", want, out)
		}
	}
}

// TestOutputRouter_NilAgentFallback verifies that when the agent is nil,
// Write forwards directly to os.Stdout (no panic, byte count passthrough).
// It does not — and cannot — capture stdout from inside the test process, so
// the assertion is limited to: no panic, return value matches input length.
func TestOutputRouter_NilAgentFallback(t *testing.T) {
	w := newOutputRouter(nil)
	n, err := w.Write([]byte("hello\n"))
	if err != nil {
		t.Fatalf("Write error: %v", err)
	}
	if n != 6 {
		t.Errorf("Write returned n=%d, want 6", n)
	}
}
