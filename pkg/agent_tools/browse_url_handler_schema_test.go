package tools

import (
	"errors"
	"testing"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// TestBrowseURLHandler_Definition_StepsDescriptionNamesEveryVerb guards the
// description path: api.ToolParameters has no `enum` field, so the `steps`
// description is where the model actually reads the verb list. A verb missing
// from it is a verb the model will not use.
func TestBrowseURLHandler_Definition_StepsDescriptionNamesEveryVerb(t *testing.T) {
	t.Parallel()

	h := &browseURLHandler{}
	def := h.Definition()

	var stepsParam *ParameterDef
	for i := range def.Parameters {
		if def.Parameters[i].Name == "steps" {
			stepsParam = &def.Parameters[i]
			break
		}
	}
	if stepsParam == nil {
		t.Fatal("browse_url Definition has no 'steps' parameter")
	}

	for _, action := range webcontent.BrowseStepActions() {
		if !contains(stepsParam.Description, action) {
			t.Errorf("'steps' description must name the verb %q, got: %s", action, stepsParam.Description)
		}
	}
	for _, want := range []string{"NO wait", "NO screenshot", `"expect"`} {
		if !contains(stepsParam.Description, want) {
			t.Errorf("'steps' description should mention %q, got: %s", want, stepsParam.Description)
		}
	}
}

// TestBrowseURLHandler_Definition_StepActionEnum pins the step-action
// vocabulary in the tool schema. The schema is the model's only view of the
// step language; an unconstrained string (or a stale list) is what let the
// model invent verbs like "wait" and "screenshot" that the executor rejected
// only after a full Chromium launch.
func TestBrowseURLHandler_Definition_StepActionEnum(t *testing.T) {
	t.Parallel()

	h := &browseURLHandler{}
	def := h.Definition()

	var stepsParam *ParameterDef
	for i := range def.Parameters {
		if def.Parameters[i].Name == "steps" {
			stepsParam = &def.Parameters[i]
			break
		}
	}
	if stepsParam == nil {
		t.Fatal("browse_url Definition has no 'steps' parameter")
	}
	if stepsParam.Items == nil {
		t.Fatal("'steps' must declare Items")
	}

	props, ok := stepsParam.Items["properties"].(map[string]any)
	if !ok {
		t.Fatalf("'steps' Items has no properties map, got %T", stepsParam.Items["properties"])
	}
	actionProp, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatalf("step 'action' property is not a schema map, got %T", props["action"])
	}

	rawEnum, ok := actionProp["enum"]
	if !ok {
		t.Fatal("step 'action' property must carry an enum")
	}
	enum, ok := rawEnum.([]string)
	if !ok {
		t.Fatalf("step 'action' enum is %T, want []string", rawEnum)
	}
	if len(enum) == 0 {
		t.Fatal("step 'action' enum must not be empty")
	}

	// The schema enum must be exactly the canonical vocabulary — no more, no
	// less. Drift either way misleads the model.
	if got, want := enum, webcontent.BrowseStepActions(); !equalStrings(got, want) {
		t.Errorf("step 'action' enum = %v, want %v", got, want)
	}

	// The description must call out the two verbs that are not step actions,
	// since those are the ones the model reaches for.
	desc, _ := actionProp["description"].(string)
	for _, want := range []string{"wait_for", "screenshot_selector", "NO wait", "NO screenshot"} {
		if !contains(desc, want) {
			t.Errorf("step 'action' description should mention %q, got: %s", want, desc)
		}
	}
}

// TestBrowseURLHandler_Definition_ActionEnum pins the top-level action enum.
func TestBrowseURLHandler_Definition_ActionEnum(t *testing.T) {
	t.Parallel()

	h := &browseURLHandler{}
	def := h.Definition()

	for _, p := range def.Parameters {
		if p.Name != "action" {
			continue
		}
		if got, want := p.Enum, webcontent.BrowseActions(); !equalStrings(got, want) {
			t.Errorf("top-level 'action' enum = %v, want %v", got, want)
		}
		return
	}
	t.Fatal("browse_url Definition has no 'action' parameter")
}

// TestBuildBrowseOptions_InvalidStepAction verifies a bogus verb is rejected
// as invalid input by the adapter path — before any browser work — and that
// the error names the valid actions.
func TestBuildBrowseOptions_InvalidStepAction(t *testing.T) {
	t.Parallel()

	opts := map[string]any{
		"steps": []interface{}{
			map[string]interface{}{"action": "bogus", "selector": "#btn"},
		},
	}

	_, err := buildBrowseOptions(opts)
	if err == nil {
		t.Fatal("expected an error for an unknown step action")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("error should be InvalidInput (not retryable), got %T: %v", err, err)
	}
	if !contains(err.Error(), "unknown browse step action") {
		t.Errorf("error should name the problem, got: %v", err)
	}
	if !contains(err.Error(), "click") {
		t.Errorf("error should list the valid actions, got: %v", err)
	}
}

// TestBuildBrowseOptions_StepAliasesNormalized verifies the adapter path
// resolves aliases to canonical verbs, so the executor never sees "wait".
func TestBuildBrowseOptions_StepAliasesNormalized(t *testing.T) {
	t.Parallel()

	opts := map[string]any{
		"steps": []interface{}{
			map[string]interface{}{"action": "wait", "selector": "#result"},
			map[string]interface{}{"action": "wait", "millis": float64(100)},
			map[string]interface{}{"action": "goto", "value": "https://example.com"},
		},
	}

	got, err := buildBrowseOptions(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("Steps length = %d, want 3", len(got.Steps))
	}
	if got.Steps[0].Action != "wait_for" {
		t.Errorf("Steps[0].Action = %q, want wait_for", got.Steps[0].Action)
	}
	if got.Steps[1].Action != "sleep" {
		t.Errorf("Steps[1].Action = %q, want sleep", got.Steps[1].Action)
	}
	if got.Steps[2].Action != "navigate" {
		t.Errorf("Steps[2].Action = %q, want navigate", got.Steps[2].Action)
	}
}

// TestBrowseURLHandler_Execute_UnknownActionFailsPreFlight verifies the
// handler rejects an unknown top-level action as invalid input without
// touching the browser.
func TestBrowseURLHandler_Execute_UnknownActionFailsPreFlight(t *testing.T) {
	t.Parallel()

	mock := &mockWebBrowserForBrowseURL{returnResult: "should not be reached"}
	h := &browseURLHandler{}

	result, err := h.Execute(t.Context(), ToolEnv{WebBrowser: mock}, map[string]any{
		"url":    "https://example.com",
		"action": "bogus",
	})

	if err == nil {
		t.Fatal("expected an error for an unknown action")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("error should be InvalidInput, got %T: %v", err, err)
	}
	if !result.IsError {
		t.Error("IsError should be true")
	}
	if mock.lastURL != "" {
		t.Errorf("browser must not be called for an invalid action, got URL %q", mock.lastURL)
	}
	if !contains(result.Output, "unknown action") {
		t.Errorf("Output should name the problem, got: %s", result.Output)
	}
}

// TestBrowseURLHandler_Execute_DefaultActionAllowed verifies the empty action
// (the documented default) still passes pre-flight validation.
func TestBrowseURLHandler_Execute_DefaultActionAllowed(t *testing.T) {
	t.Parallel()

	mock := &mockWebBrowserForBrowseURL{returnResult: "page text"}
	h := &browseURLHandler{}

	result, err := h.Execute(t.Context(), ToolEnv{WebBrowser: mock}, map[string]any{
		"url": "https://example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Errorf("IsError should be false, got output: %s", result.Output)
	}
	if mock.lastURL != "https://example.com" {
		t.Errorf("browser URL = %q, want https://example.com", mock.lastURL)
	}
}

// TestBrowseURLHandler_Execute_ScreenshotStillRequiresPath keeps the
// action-specific validation behavior for the canonical screenshot action.
func TestBrowseURLHandler_Execute_ScreenshotStillRequiresPath(t *testing.T) {
	t.Parallel()

	mock := &mockWebBrowserForBrowseURL{returnResult: "unused"}
	h := &browseURLHandler{}

	result, err := h.Execute(t.Context(), ToolEnv{WebBrowser: mock}, map[string]any{
		"url":    "https://example.com",
		"action": "screenshot",
	})
	// A missing screenshot_path is invalid input, returned typed so it is not
	// classified as retryable.
	if err == nil {
		t.Fatal("expected an error without screenshot_path")
	}
	if !agenterrors.IsInvalidInput(err) {
		t.Errorf("error should be InvalidInput, got %T: %v", err, err)
	}
	if !result.IsError {
		t.Error("IsError should be true without screenshot_path")
	}
	if !contains(result.Output, "screenshot_path") {
		t.Errorf("Output should mention screenshot_path, got: %s", result.Output)
	}
}

// TestBrowseURLHandler_Execute_TransientNavigationErrorStaysRetryable pins
// the classification of browse failures: a momentary Chromium net error must
// reach the executor as retryable (the page usually loads a moment later),
// while the handler still surfaces the underlying cause in the message.
func TestBrowseURLHandler_Execute_TransientNavigationErrorStaysRetryable(t *testing.T) {
	t.Parallel()

	mock := &mockWebBrowserForBrowseURL{returnError: errors.New("net::ERR_CONNECTION_RESET")}
	h := &browseURLHandler{}

	_, err := h.Execute(t.Context(), ToolEnv{WebBrowser: mock}, map[string]any{
		"url": "https://flaky.example.com",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !agenterrors.IsRetryable(err) {
		t.Errorf("a transient navigation fault must stay retryable, got %T: %v", err, err)
	}
	if !contains(err.Error(), "ERR_CONNECTION_RESET") {
		t.Errorf("error should carry the underlying cause, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Local helpers.
// ---------------------------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
