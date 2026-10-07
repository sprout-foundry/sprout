//go:build !js

// requiretest_hook_test.go — the require-a-test-for-new-behavior acceptance
// tests, end to end through a full agent turn (scripted model, real workspace
// fixture, real shell execution of the manifest's build command). They pin
// the opt-in gate and its enforcement through the verification run:
//
//   - enabled + new non-test code + no test item and no test file →
//     verification fails and the reply carries the failure;
//   - enabled + a test acceptance item in the active plan → passes;
//   - enabled + a test file added in the turn → passes;
//   - disabled (the default) → unchanged behavior even with no test;
//   - a docs-only turn is not flagged (the changed-application-code gate
//     still applies).
//
// Fixtures mirror verification_hook_test.go (vhAgent, vhWriteToolCall,
// vhReportMessages); the manifest's build command always passes here so the
// require_test check is the only thing that can fail the run.

package agent

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/plancontract"
	"github.com/sprout-foundry/sprout/pkg/planstore"
)

// rtWriteToolCall is a scripted write_file tool call with explicit content, so
// a test can write a test-named file (or any path) whose content does not
// matter to the path-only classifier.
func rtWriteToolCall(t *testing.T, root, path, content string) *ScriptedResponse {
	t.Helper()
	args := fmt.Sprintf(`{"path":%q,"content":%q}`, filepath.Join(root, path), content)
	return NewScriptedToolCallResponse("rt_wf_1", "write_file", args, "Writing the file.")
}

// rtWritePlan writes an active plan with the given acceptance items, so a
// test can prove the plan-side satisfaction of the requirement (a test item).
func rtWritePlan(t *testing.T, root string, acceptance []plancontract.Acceptance) {
	t.Helper()
	plan := plancontract.New("Add login", time.Now())
	plan.Scope = []plancontract.ScopeItem{{ID: "s1", Title: "Auth"}}
	plan.Steps = []plancontract.Step{{Scope: "s1", Description: "Implement login"}}
	plan.Acceptance = acceptance
	_, err := planstore.New().Save(root, plan)
	require.NoError(t, err, "save the plan fixture")
}

// TestRequireTest_EnabledNewCodeWithoutTestFails proves rule (1): enabled with
// new non-test code and neither a test item nor a test file fails
// verification. The failing require_test check is fed back and the final reply
// carries the failure report.
func TestRequireTest_EnabledNewCodeWithoutTestFails(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	// The build always passes: only the require_test check can fail the run.
	vhWriteStarterManifest(t, root, "exit 0")

	const (
		turnAnswer   = "I added the feature."
		repairAnswer = "The requirement stands; I did not add a test."
	)
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
		NewScriptedTextResponse(repairAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:        true,
		RequireTest:    true,
		RepairAttempts: 1,
	})

	result, err := ag.ProcessQuery("Implement the feature.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if !strings.Contains(result, "Verification: FAILED") {
		t.Errorf("result = %q, want the verification failure attachment", result)
	}
	if !strings.Contains(result, "require_test") {
		t.Errorf("result = %q, want the require_test check named in the failure", result)
	}

	// The repair round was fed the failing check with the requirement reason.
	reports := vhReportMessages(ag)
	if len(reports) != 1 {
		t.Fatalf("verification-report messages = %d, want 1 (one repair round)", len(reports))
	}
	if !strings.Contains(reports[0], "require_test") {
		t.Errorf("report = %q, want the require_test check", reports[0])
	}
	if !strings.Contains(reports[0], "test") {
		t.Errorf("report = %q, want the requirement explanation", reports[0])
	}

	res := ag.LastVerificationResult()
	if res == nil || !res.Failed() {
		t.Fatalf("LastVerificationResult = %+v, want the stored failing run", res)
	}
	var sawRequireTest bool
	for _, c := range res.Checks {
		if string(c.Kind) == "require_test" {
			sawRequireTest = true
			if c.Passed {
				t.Error("require_test check Passed = true, want false")
			}
		}
	}
	if !sawRequireTest {
		t.Errorf("no require_test check in the stored result: %+v", res.Checks)
	}
}

// TestRequireTest_EnabledWithPlanTestItemPasses proves rule (2): a plan that
// declares an acceptance item of kind test satisfies the requirement on a
// code-changing turn.
func TestRequireTest_EnabledWithPlanTestItemPasses(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "exit 0")
	rtWritePlan(t, root, []plancontract.Acceptance{
		{ID: "a1", Scope: "s1", Kind: plancontract.KindBuild},
		{ID: "a2", Scope: "s1", Kind: plancontract.KindTest},
	})

	const turnAnswer = "I added the feature and its test item is in the plan."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:        true,
		RequireTest:    true,
		RepairAttempts: 1,
	})

	result, err := ag.ProcessQuery("Implement the feature.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if strings.Contains(result, "Verification: FAILED") {
		t.Errorf("result = %q, want no verification failure (the plan declares a test item)", result)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (the requirement is satisfied)", len(reports))
	}
	res := ag.LastVerificationResult()
	if res == nil || !res.Passed() {
		t.Fatalf("LastVerificationResult = %+v, want a passing run", res)
	}
}

// TestRequireTest_EnabledWithTestFileAddedPasses proves rule (3): a test file
// added in the turn satisfies the requirement even with no plan.
func TestRequireTest_EnabledWithTestFileAddedPasses(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "exit 0")

	const turnAnswer = "I added the feature and its test."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		rtWriteToolCall(t, root, "src/app_test.go", "package app\n"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:        true,
		RequireTest:    true,
		RepairAttempts: 1,
	})

	result, err := ag.ProcessQuery("Implement the feature with a test.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if strings.Contains(result, "Verification: FAILED") {
		t.Errorf("result = %q, want no verification failure (a test file was added)", result)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (the requirement is satisfied)", len(reports))
	}
	res := ag.LastVerificationResult()
	if res == nil || !res.Passed() {
		t.Fatalf("LastVerificationResult = %+v, want a passing run", res)
	}
}

// TestRequireTest_DisabledIsUnchangedBehavior proves rule (4): with the flag
// off (the default) a code-changing turn with no test item and no test file is
// not flagged — verification behaves exactly as it does without the flag.
func TestRequireTest_DisabledIsUnchangedBehavior(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "exit 0")

	const turnAnswer = "I added the feature without a test."
	client := NewScriptedClient(
		vhWriteToolCall(t, root, "src/app.js"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:        true,
		RepairAttempts: 1,
	})

	result, err := ag.ProcessQuery("Implement the feature.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if strings.Contains(result, "Verification: FAILED") {
		t.Errorf("result = %q, want no failure (the requirement is off by default)", result)
	}
	if reports := vhReportMessages(ag); len(reports) != 0 {
		t.Errorf("verification-report messages = %d, want 0 (the requirement is off)", len(reports))
	}
	res := ag.LastVerificationResult()
	if res == nil || !res.Passed() {
		t.Fatalf("LastVerificationResult = %+v, want a passing run", res)
	}
	for _, c := range res.Checks {
		if string(c.Kind) == "require_test" {
			t.Errorf("a disabled requirement must add no require_test check: %+v", res.Checks)
		}
	}
}

// TestRequireTest_DocsOnlyTurnNotFlagged proves rule (5): the
// changed-application-code gate still applies — a docs-only turn is not new
// behavior, so even with the flag on the requirement is not applied.
func TestRequireTest_DocsOnlyTurnNotFlagged(t *testing.T) {
	vhShAvailable(t)
	root := t.TempDir()
	vhWriteStarterManifest(t, root, "exit 0")

	const turnAnswer = "Docs updated."
	client := NewScriptedClient(
		vhacWriteToolCall(t, root, "docs/guide.md"),
		NewScriptedTextResponse(turnAnswer),
	)
	ag := vhAgent(t, client, root, &configuration.VerificationConfig{
		Enabled:        true,
		RequireTest:    true,
		RepairAttempts: 1,
	})

	result, err := ag.ProcessQuery("Update the docs guide.")
	if err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}
	if result != turnAnswer {
		t.Errorf("result = %q, want the untouched turn answer %q (a docs-only turn must not run verification)", result, turnAnswer)
	}
	if res := ag.LastVerificationResult(); res != nil {
		t.Errorf("LastVerificationResult = %+v, want nil (the hook never ran)", res)
	}
}
