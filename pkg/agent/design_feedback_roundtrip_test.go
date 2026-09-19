//go:build !js

// design_feedback_roundtrip_test.go — the committed SP-140-4 §4d / item-4.9
// acceptance test for the human feedback round trip.
//
// The AC (SP-140-4 "Acceptance criteria", item 4.9) is:
//
//	Feedback round trip: annotation written via the webui write path →
//	design_assets reports pending feedback → skill-loop test (scripted agent)
//	reads feedback and addresses the annotated screen → annotation resolvable
//	in DesignView.
//
// This file covers that path end to end through a REAL scripted agent turn, so
// the dispatch layer the handler unit tests cannot reach is exercised too: the
// pending report must survive seed's tool registry into the tool message the
// model sees, the model must be able to read the very feedback file the report
// named, and the edit it makes must land on disk before the loop closes.
//
// It is deliberately a *committed* (tracked) test. A broader scripted-client
// e2e suite also exists in design_e2e_test.go, but that file is part of the
// uncommitted design-tier backlog; the round-trip AC must be auditable in the
// tracked tree, so this file carries its own hermetic fixtures and helpers
// rather than importing from the backlog file.

package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// fbrManifest is a design/README.md satisfying the SP-140-1 manifest
// conventions, so a whole-tree design_assets run has nothing else to report
// and the assertions read only the feedback section.
const fbrManifest = `# Design Workspace

frames:
  desktop: 1440x900
  mobile: 390x844

## Screens

- ` + "`login`" + ` — draft — sign-in entry point

## Flows

## Status markers

Screens and flows carry one of: draft, review, ready.

## Links

- [Login wireframe](wireframes/login.svg)
`

const (
	fbrGitAttributes = "* text=auto eol=lf\ndesign/**/*.svg diff=html\n"
	fbrGitIgnore     = "node_modules/\ndesign/.cache/\n"
)

// fbrLoginSVG is the annotated wireframe. It is the *original* sketch; the
// scripted turn replaces it with fbrLoginSVGRevised when it addresses the
// annotation, so the test can prove the annotated screen actually changed.
const fbrLoginSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">` +
	`<text x="24" y="64" font-size="20">Login</text>` +
	`<rect id="submit" x="24" y="200" width="342" height="52"/>` +
	`</svg>`

// fbrLoginSVGRevised is the post-annotation wireframe: the CTA is promoted
// (larger, primary) per the annotation's note.
const fbrLoginSVGRevised = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">` +
	`<text x="24" y="64" font-size="28">Login</text>` +
	`<rect id="primary-cta" x="24" y="200" width="342" height="56"/>` +
	`</svg>`

// fbrFeedbackJSONVolumeDown is exactly what the webui write path produces for
// a new annotation (webui/src/design/feedbackWrite.ts buildFeedbackFile /
// DesignFeedbackResolution): status changes-requested, one unresolved
// annotation keyed on the login wireframe. Seeding it here — rather than
// hand-rolling a shape — is what makes the test a *round trip*: the document
// the human affordance writes is the document the agent reads.
const fbrFeedbackJSONVolumeDown = `{
  "target": "design/wireframes/login.svg",
  "status": "changes-requested",
  "resolution": "",
  "annotations": [
    {"id": "a1", "at": {"x": 0.42, "y": 0.18}, "area": "hierarchy",
     "note": "Primary CTA reads as secondary; swap emphasis", "resolved": false,
     "created": "2026-09-15T10:36:47Z"}
  ]
}`

// fbrFeedbackJSONClosed is what the round trip must produce: the same document
// with the annotation marked resolved and the agent's resolution note written
// (the §4d closing fields). The scripted turn writes this with write_file, and
// the assertions then check the on-disk document rather than the scripted
// fixture, so a turn that only *claims* to close the loop fails.
const fbrFeedbackJSONClosed = `{
  "target": "design/wireframes/login.svg",
  "status": "resolved",
  "resolution": "Promoted the primary CTA; made the login title the largest element.",
  "annotations": [
    {"id": "a1", "at": {"x": 0.42, "y": 0.18}, "area": "hierarchy",
     "note": "Primary CTA reads as secondary; swap emphasis", "resolved": true,
     "created": "2026-09-15T10:36:47Z"}
  ]
}`

// ---------------------------------------------------------------------------
// Helpers (self-contained: the design_e2e_test.go backlog file owns its own)
// ---------------------------------------------------------------------------

// fbrWrite writes rel (slash-separated) under root, creating parents.
func fbrWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// fbrWorkspace builds the design/ tree with the §1h git contract and chdirs
// into it so both the agent's VFS resolution and the design handlers'
// fallback root agree on the workspace.
func fbrWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fbrWrite(t, root, design.GitContractFile, fbrGitAttributes)
	fbrWrite(t, root, design.GitIgnoreFile, fbrGitIgnore)
	fbrWrite(t, root, "design/README.md", fbrManifest)
	fbrWrite(t, root, "design/wireframes/login.svg", fbrLoginSVG)
	t.Chdir(root)
	return root
}

// fbrAgent wires a scripted client into the seed conversation loop with the
// full context profile (Low-Context Mode would prune the design tools, a
// different spec's concern).
func fbrAgent(t *testing.T, client *ScriptedClient, workspaceRoot string) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		// No interactive prompt may fire mid-turn (stdin is closed in tests):
		// SkipPrompt makes a "prompt" file-access verdict resolve to denied
		// deterministically.
		cfg.SkipPrompt = true
		return nil
	}); err != nil {
		t.Fatalf("configure test agent: %v", err)
	}

	ag, err := NewAgentWithClient(client, api.TestClientType, mgr)
	if err != nil {
		t.Fatalf("NewAgentWithClient: %v", err)
	}
	t.Cleanup(ag.Shutdown)
	ag.SetMaxIterations(10)
	ag.SetWorkspaceRoot(workspaceRoot)
	return ag
}

// fbrToolMessage returns the tool result message for callID.
func fbrToolMessage(t *testing.T, ag *Agent, callID string) (api.Message, string) {
	t.Helper()
	for _, m := range ag.GetMessages() {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m, m.Content
		}
	}
	t.Fatalf("no tool result message for call %q", callID)
	return api.Message{}, ""
}

// fbrHasTool reports whether the agent's roster advertises name, so a scripted
// turn cannot "pass" because the tool silently fell out of the allowlist.
func fbrHasTool(t *testing.T, ag *Agent, name string) bool {
	t.Helper()
	return NewSeedToolRegistry(ag).HasTool(name)
}

// fbrJSONQuote renders s as a JSON string literal (scripted tool arguments
// carrying multi-line documents must stay valid JSON). encoding/json owns the
// escaping so any character in the fixture — control chars, unicode, quotes —
// round-trips correctly rather than depending on a hand-maintained escaper.
func fbrJSONQuote(t *testing.T, s string) string {
	t.Helper()
	if s == "" {
		return `""`
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal scripted argument string: %v", err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// The §4d round trip
// ---------------------------------------------------------------------------

// TestDesignFeedbackRoundTripE2E_ScriptedTurnAddressesAnnotation is the item
// 4.9 AC: an annotation written through the webui write path drives a scripted
// agent turn all the way to a closed loop.
//
// The four AC steps, asserted in order:
//
//  1. design_assets reports the target as pending, with counts (the skill
//     loop's trigger);
//  2. the scripted agent reads design/feedback/login.json with read_file and
//     the annotation's note/area reach the model verbatim;
//  3. the agent edits the annotated screen (the wireframe on disk changes),
//     then closes the loop by writing the §4d resolution note and flipping
//     status off changes-requested in the same feedback file;
//  4. the resulting document is resolvable in DesignView: it is no longer
//     pending, its annotation is resolved, and the resolution note is present
//     (the exact state the 4.8 detail pane consumes).
func TestDesignFeedbackRoundTripE2E_ScriptedTurnAddressesAnnotation(t *testing.T) {
	root := fbrWorkspace(t)
	// Step 0: the human annotation, via the webui write path's document shape.
	fbrWrite(t, root, "design/feedback/login.json", fbrFeedbackJSONVolumeDown)

	client := NewScriptedClient(
		NewScriptedToolCallResponse("assets_1", "design_assets", `{}`,
			"Checking the tree for pending feedback."),
		NewScriptedToolCallResponse("read_1", "read_file",
			`{"path":"design/feedback/login.json"}`,
			"Reading the pending feedback file."),
		NewScriptedToolCallResponse("edit_screen_1", "write_file",
			`{"path":"design/wireframes/login.svg","content":`+fbrJSONQuote(t, fbrLoginSVGRevised)+`}`,
			"Addressing the hierarchy annotation on the login wireframe."),
		NewScriptedToolCallResponse("close_1", "write_file",
			`{"path":"design/feedback/login.json","content":`+fbrJSONQuote(t, fbrFeedbackJSONClosed)+`}`,
			"Closing the feedback loop with the resolution note."),
		NewScriptedTextResponse("Addressed the annotation on design/wireframes/login.svg and closed the feedback loop."),
	)
	ag := fbrAgent(t, client, root)

	for _, name := range []string{"design_assets", "read_file", "write_file"} {
		if !fbrHasTool(t, ag, name) {
			t.Skipf("%s is not registered in this build", name)
		}
	}

	if _, err := ag.ProcessQuery(
		"There is pending design feedback; find it, address it, and close the loop."); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	// --- Step 1: design_assets reports the pending target with counts. -----
	_, assetsOut := fbrToolMessage(t, ag, "assets_1")
	if strings.Contains(assetsOut, "unknown tool") {
		t.Fatalf("design_assets was not dispatched: %s", assetsOut)
	}
	for _, want := range []string{
		"Pending feedback: 1 target(s)",
		"design/wireframes/login.svg",
		"1 unresolved",
		"read of its feedback file",
	} {
		if !strings.Contains(assetsOut, want) {
			t.Errorf("design_assets must report pending feedback %q:\n%s", want, assetsOut)
		}
	}

	// --- Step 2: the model reads the feedback file the report named. -------
	_, readOut := fbrToolMessage(t, ag, "read_1")
	if strings.Contains(readOut, "unknown tool") || strings.Contains(readOut, "blocked") {
		t.Fatalf("read_file of the pending feedback file failed: %s", readOut)
	}
	for _, want := range []string{
		"changes-requested",
		"Primary CTA reads as secondary",
		"hierarchy",
	} {
		if !strings.Contains(readOut, want) {
			t.Errorf("the model must see the annotation %q:\n%s", want, readOut)
		}
	}

	// --- Step 3a: the annotated screen actually changed on disk. -----------
	written, err := os.ReadFile(filepath.Join(root, "design", "wireframes", "login.svg"))
	if err != nil {
		t.Fatalf("the scripted turn must have edited the annotated wireframe: %v", err)
	}
	if string(written) != fbrLoginSVGRevised {
		t.Errorf("annotated screen edit differs from the scripted content:\n got %s\nwant %s",
			written, fbrLoginSVGRevised)
	}

	// --- Step 3b + 4: the loop closed and the annotation is resolvable. ----
	// Observe the on-disk state through the same parser the 4.8 DesignView
	// feedback seam and the design-system skill's loop use, rather than
	// trusting the scripted fixture.
	states, scanErr := design.ScanFeedbackDir(root)
	if scanErr != nil {
		t.Fatalf("ScanFeedbackDir: %v", scanErr)
	}
	if len(states) != 1 {
		t.Fatalf("expected exactly one feedback file, got %#v", states)
	}
	state := states[0]
	if !state.Valid {
		t.Fatalf("the closed feedback document must still parse as §4d: %#v", state)
	}
	if state.Pending != 0 || state.Resolved != state.Annotations || state.Annotations != 1 {
		t.Errorf("the annotation must be resolved after the round trip: %#v", state)
	}
	if state.IsPending() {
		t.Errorf("a closed feedback file must not report pending: %#v", state)
	}
	if state.Status != "resolved" {
		t.Errorf("status must move off changes-requested, got %q", state.Status)
	}
	if !strings.Contains(state.Resolution, "Promoted the primary CTA") {
		t.Errorf("the resolution note must be recorded, got %q", state.Resolution)
	}
	if got := design.PendingFeedbackCount(states); got != 0 {
		t.Errorf("PendingFeedbackCount after the round trip = %d, want 0", got)
	}

	// The closed document is also validator-clean (no §4d schema finding), so
	// the DesignView pane has a resolvable, well-formed file to render.
	findings, valErr := design.ValidateFeedbackDir(root)
	if valErr != nil {
		t.Fatalf("ValidateFeedbackDir: %v", valErr)
	}
	if len(findings) != 0 {
		t.Errorf("the closed feedback document must validate cleanly, got %#v", findings)
	}

	// --- Step 4 (cont.): re-running design_assets must now report no pending
	// feedback, i.e. the trigger the loop started on is gone. ---------------
	states2, scanErr2 := design.ScanFeedbackDir(root)
	if scanErr2 != nil {
		t.Fatalf("ScanFeedbackDir (post): %v", scanErr2)
	}
	if design.PendingFeedbackCount(states2) != 0 {
		t.Errorf("post-close scan must report no pending feedback: %#v", states2)
	}

	// --- Turn shape: four tool calls, each threaded to its result. ---------
	assertToolThreadingForRoundTrip(t, ag)

	// --- The model saw the pending report before reading, and the feedback
	// contents before editing (conversation continuity). --------------------
	reqs := client.GetSentRequests()
	if len(reqs) < 5 {
		t.Fatalf("expected 5 provider requests (one per step), got %d", len(reqs))
	}
	final := reqs[len(reqs)-1]
	var sawAssets, sawFeedback bool
	for _, m := range final {
		if m.Role != "tool" {
			continue
		}
		switch m.ToolCallID {
		case "assets_1":
			sawAssets = true
			if !strings.Contains(m.Content, "Pending feedback") {
				t.Errorf("the model must see the pending-feedback report, got: %s", m.Content)
			}
		case "read_1":
			sawFeedback = true
			if !strings.Contains(m.Content, "Primary CTA reads as secondary") {
				t.Errorf("the model must see the annotation, got: %s", m.Content)
			}
		}
	}
	if !sawAssets || !sawFeedback {
		t.Errorf("conversation continuity broken: assetsSeen=%v feedbackSeen=%v", sawAssets, sawFeedback)
	}
}

// TestDesignFeedbackRoundTripE2E_PendingReportNamesCountsWhenPartiallyResolved
// pins the reporting half of the AC against the *partially* addressed state a
// real loop passes through: two annotations, one already resolved by the pane
// (4.8), leaves the file pending with a count of 1 — so the agent is told
// exactly how much work remains rather than a bare "pending".
func TestDesignFeedbackRoundTripE2E_PendingReportNamesCountsWhenPartiallyResolved(t *testing.T) {
	root := fbrWorkspace(t)
	fbrWrite(t, root, "design/feedback/login.json", `{
  "target": "design/wireframes/login.svg",
  "status": "changes-requested",
  "resolution": "",
  "annotations": [
    {"id": "a1", "at": {"x": 0.1, "y": 0.1}, "area": "hierarchy", "note": "first",
     "resolved": true, "created": "2026-09-15T10:36:47Z"},
    {"id": "a2", "at": {"x": 0.2, "y": 0.2}, "area": "contrast", "note": "second",
     "resolved": false, "created": "2026-09-15T10:37:00Z"}
  ]
}`)

	client := NewScriptedClient(
		NewScriptedToolCallResponse("assets_1", "design_assets", `{}`,
			"Checking pending feedback."),
		NewScriptedTextResponse("One annotation remains."),
	)
	ag := fbrAgent(t, client, root)
	if !fbrHasTool(t, ag, "design_assets") {
		t.Skip("design_assets is not registered in this build")
	}

	if _, err := ag.ProcessQuery("What design feedback is pending?"); err != nil {
		t.Fatalf("ProcessQuery: %v", err)
	}

	_, assetsOut := fbrToolMessage(t, ag, "assets_1")
	// The summary names the target with its remaining-unresolved count.
	if !strings.Contains(assetsOut, "Pending feedback: 1 target(s)") {
		t.Errorf("partially-resolved file must still be pending:\n%s", assetsOut)
	}
	if !strings.Contains(assetsOut, "1 unresolved") {
		t.Errorf("the report must name the remaining unresolved count:\n%s", assetsOut)
	}

	// Machine-readable view agrees: the pending row carries the split.
	states, err := design.ScanFeedbackDir(root)
	if err != nil {
		t.Fatalf("ScanFeedbackDir: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected one feedback state, got %#v", states)
	}
	st := states[0]
	if st.Annotations != 2 || st.Resolved != 1 || st.Pending != 1 {
		t.Errorf("annotation split = %d total / %d resolved / %d pending, want 2/1/1", st.Annotations, st.Resolved, st.Pending)
	}
	if !st.IsPending() {
		t.Errorf("one unresolved annotation keeps the file pending: %#v", st)
	}
}

// TestDesignFeedbackRoundTripE2E_ResolvedFileIsNotPendingOnceClosed covers the
// terminal state directly (the AC's "annotation resolvable in DesignView"
// endpoint): a file the pane fully resolved and the agent annotated with a
// resolution note reports neither pending nor a dangling-target finding.
func TestDesignFeedbackRoundTripE2E_ResolvedFileIsNotPendingOnceClosed(t *testing.T) {
	root := fbrWorkspace(t)
	fbrWrite(t, root, "design/feedback/login.json", fbrFeedbackJSONClosed)

	states, err := design.ScanFeedbackDir(root)
	if err != nil {
		t.Fatalf("ScanFeedbackDir: %v", err)
	}
	if len(states) != 1 || states[0].IsPending() {
		t.Fatalf("a fully-resolved feedback file must not be pending: %#v", states)
	}
	if got := design.PendingFeedbackCount(states); got != 0 {
		t.Errorf("PendingFeedbackCount = %d, want 0", got)
	}
	findings, err := design.ValidateFeedbackDir(root)
	if err != nil {
		t.Fatalf("ValidateFeedbackDir: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("the resolved document must validate cleanly, got %#v", findings)
	}
}

// ---------------------------------------------------------------------------
// Shared thread assertion
// ---------------------------------------------------------------------------

// assertToolThreadingForRoundTrip verifies every assistant tool call has a
// matching tool result and no two assistant messages are adjacent. Kept local
// (the shared helper has a pre-existing presence-check bug documented in the
// backlog e2e file, and this test must assert the correct form).
func assertToolThreadingForRoundTrip(t *testing.T, ag *Agent) {
	t.Helper()
	msgs := ag.GetMessages()
	missing := 0
	consecutive := 0
	for i, m := range msgs {
		if m.Role == "assistant" && i > 0 && msgs[i-1].Role == "assistant" {
			consecutive++
		}
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		declared := make(map[string]bool, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				declared[tc.ID] = false
			}
		}
		for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
			if _, ok := declared[msgs[j].ToolCallID]; ok {
				declared[msgs[j].ToolCallID] = true
			}
		}
		for _, found := range declared {
			if !found {
				missing++
			}
		}
	}
	if missing != 0 {
		t.Errorf("tool threading: %d call(s) without a result", missing)
	}
	if consecutive != 0 {
		t.Errorf("tool threading: %d consecutive assistant message(s)", consecutive)
	}
}
