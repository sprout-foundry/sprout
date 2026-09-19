//go:build !js

// design_loop_e2e_test.go — the SP-140-5 §5 "End-to-end loop" acceptance test
// (TODO item 5.9).
//
// The AC is the design↔code loop, driven end to end through REAL agent turns:
//
//	design turn (new token) → export → dev turn (component consumes + tweaks it)
//	  → design_sync → design tree reflects the tweak;
//	    next design turn builds on it, not against it.
//
// It is a TEST-ONLY item: no tool behaviour changes. The test wires a scripted
// client (the SP-137 scripted-client pattern) into seed's conversation loop with
// the CONFIGURED DESIGNER PERSONA active and drives three turns against one
// hermetic fixture workspace:
//
//  1. Design turn (designer persona): read the tree with design_assets, add a
//     new color token by rewriting design/tokens/color.tokens.json with
//     write_file, then run the REAL design_export_tokens handler. Assert the
//     token exists in the tree AND the generated theme carries it.
//  2. Dev turn: read the generated theme (it consumes the token), write the
//     implementation's theme file that revalues the token (the tweak), then run
//     the REAL design_sync handler — analyze (assert a literal revalue delta
//     naming the DTCG file+entry, and that analyze wrote nothing) followed by
//     apply (assert the design tree now reflects the code's tweaked value).
//  3. Next design turn (designer persona): read design/tokens back and observe
//     the dev tweak (builds on it), then re-export so the generated theme
//     carries the tweaked value — proving the design layer moved FORWARD from
//     the dev change rather than reverting it.
//
// Every assertion is falsifiable against on-disk state and the tool messages
// the model actually saw, so a dispatch/threading/heuristic regression fails the
// test rather than passing silently on the scripted fixtures.
//
// The analyze delta shape (the exact DTCG file/entry and design-file work set)
// is asserted through the SAME pure core (design.AnalyzeTouchedFiles /
// design.PlanSyncApply) the handler calls, run against a mirror of the loop's
// dev state: seed's dispatch path only forwards a handler's human-readable
// Output to the model, so the structured report is not reachable from the
// transcript. The mirror probe pins the semantics the scripted turn's real
// handler consumed.
//
// Hermetic: the scripted client never touches a provider and both real handlers
// are pure Go (no browser/vision). The designer persona is applied out of the
// real embedded catalog; the design handlers resolve against the workspace root
// the agent carries, so no global state leaks between tests.

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
	"github.com/sprout-foundry/sprout/pkg/personas"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// dloopManifest is a design/README.md satisfying the SP-140-1 manifest
// conventions, so the design tree the loop reads is a well-formed workspace.
const dloopManifest = `# Design Workspace

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
	dloopGitAttributes = "* text=auto eol=lf\ndesign/**/*.svg diff=html\n"
	dloopGitIgnore     = "node_modules/\ndesign/.cache/\n"
)

// dloopLoginSVG is the fixture wireframe; it exists so the tree is complete
// (the loop's deltas are token-shaped, not structural, and this keeps it that
// way).
const dloopLoginSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">` +
	`<text x="24" y="64" font-size="28">Login</text>` +
	`<rect id="submit" x="24" y="200" width="342" height="52"/>` +
	`</svg>`

// dloopTokensBefore is the token source the design turn starts from: a single
// colour token. It is valid W3C DTCG, so the pre-export tree is a real,
// exportable token tier rather than an empty one.
const dloopTokensBefore = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" }
    }
  }
}
`

// dloopTokensAfterDesign is the token source the DESIGN turn writes: the
// existing primary token is untouched and a NEW accent colour is added. The
// loop's first half is "design leads".
const dloopTokensAfterDesign = `{
  "color": {
    "brand": {
      "primary": { "$type": "color", "$value": "#0055ff" },
      "accent": { "$type": "color", "$value": "#00aa88" }
    }
  }
}
`

// The token the design turn introduces. Its DTCG path maps to the CSS var
// --color-brand-accent (the export's naming convention), which is what the dev
// turn consumes and tweaks.
const (
	dloopNewTokenPath  = "color.brand.accent"   // DTCG dotted path
	dloopNewTokenVar   = "--color-brand-accent" // generated CSS custom property
	dloopNewTokenValue = "#00aa88"              // the design turn's value
	dloopDevTokenValue = "#ff5500"              // the dev turn's tweak
	dloopTokenFile     = "design/tokens/color.tokens.json"
	dloopDevCodeFile   = "src/theme.css"
)

// dloopDevThemeCSS is the DEV turn's implementation change: the theme file
// consumes the generated var and revalues it (the tweak). The declared var maps
// 1:1 to the DTCG entry, so design_sync's literal pass reports it as a revalue
// and the apply half rewrites the DTCG entry.
const dloopDevThemeCSS = ":root {\n  " + dloopNewTokenVar + ": " + dloopDevTokenValue + ";\n}\n\n" +
	".login-badge {\n  background: var(" + dloopNewTokenVar + ");\n}\n"

// ---------------------------------------------------------------------------
// Helpers (self-contained: this file owns its fixtures)
// ---------------------------------------------------------------------------

// dloopWrite writes rel (slash-separated) under root, creating parents.
func dloopWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// dloopRead reads a workspace file, failing the test when it is missing.
func dloopRead(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// dloopWriteDesignTree writes the design/ tree the loop starts from (token tier,
// manifest, wireframe, git contract).
func dloopWriteDesignTree(t *testing.T, root string) {
	t.Helper()
	dloopWrite(t, root, design.GitContractFile, dloopGitAttributes)
	dloopWrite(t, root, design.GitIgnoreFile, dloopGitIgnore)
	dloopWrite(t, root, "design/README.md", dloopManifest)
	dloopWrite(t, root, "design/wireframes/login.svg", dloopLoginSVG)
	dloopWrite(t, root, dloopTokenFile, dloopTokensBefore)
}

// dloopWorkspace builds the fixture design/ tree and chdirs into it, so both the
// agent's VFS resolution and the design handlers' fallback root agree on the
// workspace. The implementation tree (src/) starts empty; the dev turn creates
// its theme file.
//
// t.Chdir is process-global, so this file deliberately declares no t.Parallel and
// only one test consumes dloopWorkspace — the chdir and the agent's lifetime are
// strictly nested, and t.Chdir restores the previous cwd at test end.
func dloopWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dloopWriteDesignTree(t, root)
	t.Chdir(root)
	return root
}

// dloopAgent wires a scripted client straight into the seed conversation loop
// with the full context profile and the DESIGNER PERSONA active from the real
// embedded catalog. The loop's design turns run as the designer; the dev turn is
// the same agent continuing (the loop's dev side in this fixture is the same
// session — the point is the files and the tools, not a second roster).
func dloopAgent(t *testing.T, client *ScriptedClient, workspaceRoot string) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		// No interactive prompt may fire mid-turn (stdin is closed in tests):
		// SkipPrompt makes an open-ended file-access verdict resolve to denied
		// deterministically rather than hanging.
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

	// The designer persona is the loop's design-side identity. Applying it also
	// loads the designer system prompt, so the turn runs under the persona the
	// §5 loop is written for (not a bare default agent).
	if err := ag.ApplyPersona(personas.IDDesigner); err != nil {
		t.Fatalf("ApplyPersona(%s): %v", personas.IDDesigner, err)
	}
	if got := ag.GetActivePersona(); got != personas.IDDesigner {
		t.Fatalf("active persona = %q, want %q", got, personas.IDDesigner)
	}
	return ag
}

// dloopToolMessage returns the tool result message for callID plus its
// agent-visible text.
func dloopToolMessage(t *testing.T, ag *Agent, callID string) (api.Message, string) {
	t.Helper()
	for _, m := range ag.GetMessages() {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m, m.Content
		}
	}
	t.Fatalf("no tool result message for call %q; messages: %s", callID, dloopDumpMessages(ag))
	return api.Message{}, ""
}

// dloopDumpMessages renders the transcript roles for a failure message.
func dloopDumpMessages(ag *Agent) string {
	var sb strings.Builder
	for i, m := range ag.GetMessages() {
		names := make([]string, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			names = append(names, tc.Function.Name)
		}
		sb.WriteString("\n  ")
		sb.WriteString(itoa(i))
		sb.WriteString(" role=" + m.Role)
		if len(names) > 0 {
			sb.WriteString(" calls=" + strings.Join(names, ","))
		}
		if m.ToolCallID != "" {
			sb.WriteString(" tcid=" + m.ToolCallID)
		}
	}
	return sb.String()
}

// dloopRequireRegistered skips when a tool the loop depends on is not in this
// build's registry. This is the registry-level check (the persona allowlist only
// governs the advertised schema, not dispatch), so it is the honest gate for
// "the loop can run here".
func dloopRequireRegistered(t *testing.T, ag *Agent, names ...string) {
	t.Helper()
	registry := NewSeedToolRegistry(ag)
	for _, name := range names {
		if !registry.HasTool(name) {
			t.Skipf("%s is not registered in this build", name)
		}
	}
}

// dloopJSONQuote renders s as a JSON string literal so scripted tool arguments
// carrying multi-line documents stay valid JSON.
func dloopJSONQuote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal scripted argument string: %v", err)
	}
	return string(b)
}

// dloopTokenLeafExists reports whether the export projection resolves path to a
// leaf carrying value.
func dloopTokenLeafExists(tokens *design.TokenExport, path, value string) bool {
	if tokens == nil {
		return false
	}
	for _, leaf := range tokens.Leaves {
		if leaf.Name == path && dloopLeafValueString(leaf) == value {
			return true
		}
	}
	return false
}

// dloopLeafValueString renders a leaf's resolved (else raw) value as a string,
// which for the string-valued colour tokens in this fixture is the literal hex.
func dloopLeafValueString(leaf design.ExportedToken) string {
	v := leaf.Resolved
	if !leaf.ResolvedOK {
		v = leaf.Value
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// dloopLeafDigest renders the projection for failure messages.
func dloopLeafDigest(tokens *design.TokenExport) string {
	if tokens == nil {
		return "<nil>"
	}
	parts := make([]string, 0, len(tokens.Leaves))
	for _, leaf := range tokens.Leaves {
		parts = append(parts, leaf.Name+"="+dloopLeafValueString(leaf))
	}
	return strings.Join(parts, ", ")
}

// dloopAnalyzeProbe runs the pure analyze core (the same one the design_sync
// handler calls) over a mirror of the loop's DEV state — the fixture tree plus
// the implementation's theme file — and returns the report. Seed's dispatch path
// forwards only a handler's human-readable Output to the model, so the
// structured report the scripted dev turn consumed is asserted through the core
// it shares rather than scraped from the transcript.
func dloopAnalyzeProbe(t *testing.T) (*design.SyncReport, *design.SyncApplyPlan) {
	t.Helper()
	root := t.TempDir()
	dloopWriteDesignTree(t, root)
	dloopWrite(t, root, dloopTokenFile, dloopTokensAfterDesign)
	dloopWrite(t, root, dloopDevCodeFile, dloopDevThemeCSS)

	report, err := design.AnalyzeTouchedFiles(design.SyncInput{
		Root:    root,
		Touched: []design.SyncFileInput{{Path: dloopDevCodeFile, Content: []byte(dloopDevThemeCSS)}},
	})
	if err != nil {
		t.Fatalf("AnalyzeTouchedFiles probe: %v", err)
	}
	plan := design.PlanSyncApply(report, func(rel string) ([]byte, bool) {
		data, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if readErr != nil {
			return nil, false
		}
		return data, true
	})
	return report, plan
}

// dloopJSON renders a value for failure messages.
func dloopJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "<unrenderable>"
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// The §5 end-to-end loop
// ---------------------------------------------------------------------------

// TestDesignLoopE2E_DesignToDevToSyncAndBack is the item 5.9 acceptance test:
// the full design↔code loop, in one agent, over one hermetic workspace, with
// each arrow asserted on state (not on the scripted fixture).
//
// The five AC steps, in order:
//
//  1. design turn adds a new token (design/tokens/color.tokens.json gains
//     color.brand.accent);
//  2. design_export_tokens reflects it in design/generated/ (the token and its
//     CSS var appear in the generated theme);
//  3. the dev turn consumes the token and tweaks it (src/theme.css revalues
//     --color-brand-accent);
//  4. design_sync analyze reports a literal revalue delta naming the DTCG file
//     and entry, and apply makes the design tree reflect the tweak;
//  5. the next design turn reads that updated tree (it sees the tweak, does not
//     revert it) and builds forward by re-exporting, so the generated theme
//     carries the dev value with no trace of the design value.
func TestDesignLoopE2E_DesignToDevToSyncAndBack(t *testing.T) {
	root := dloopWorkspace(t)

	// All three turns share one scripted client, so responses are consumed in
	// turn order.
	client := NewScriptedClient(
		// --- Turn 1: DESIGN (designer persona) ------------------------------
		NewScriptedToolCallResponse("d_assets_1", "design_assets", `{}`,
			"Reading the design tree before adding a token."),
		NewScriptedToolCallResponse("d_write_1", "write_file",
			`{"path":"`+dloopTokenFile+`","content":`+
				dloopJSONQuote(t, dloopTokensAfterDesign)+`}`,
			"Adding the new accent color token to the color tier."),
		NewScriptedToolCallResponse("d_export_1", "design_export_tokens", `{}`,
			"Exporting the updated tokens so code can consume the new one."),
		NewScriptedTextResponse("Added color.brand.accent and exported the theme."),

		// --- Turn 2: DEV ----------------------------------------------------
		NewScriptedToolCallResponse("v_read_gen_1", "read_file",
			`{"path":"design/generated/tokens.css"}`,
			"Reading the generated theme to consume the new token."),
		NewScriptedToolCallResponse("v_write_1", "write_file",
			`{"path":"`+dloopDevCodeFile+`","content":`+dloopJSONQuote(t, dloopDevThemeCSS)+`}`,
			"Consuming the token in the theme and adjusting its value."),
		NewScriptedToolCallResponse("v_sync_analyze_1", "design_sync",
			`{"files":"`+dloopDevCodeFile+`","mode":"analyze"}`,
			"Analysing the dev change for semantic deltas."),
		NewScriptedTextResponse("Consumed --color-brand-accent; design_sync analyze found a revalue."),

		// --- Turn 2b: DEV, the design_sync apply half (same session) --------
		// Split from 2a so the analyze half's read-only guarantee can be proven
		// by reading the tree BETWEEN the two turns (seed runs a whole turn
		// before ProcessQuery returns, so the analyze of a single combined turn
		// could not be observed before its apply had already written).
		NewScriptedToolCallResponse("v_sync_apply_1", "design_sync",
			`{"files":"`+dloopDevCodeFile+`","mode":"apply"}`,
			"Adopting the revalue into the design tree."),
		NewScriptedTextResponse("design_sync adopted the tweak into design/."),

		// --- Turn 3: next DESIGN turn (designer persona) --------------------
		NewScriptedToolCallResponse("d2_read_tokens_1", "read_file",
			`{"path":"`+dloopTokenFile+`"}`,
			"Reading the current token state before building forward."),
		NewScriptedToolCallResponse("d2_export_1", "design_export_tokens", `{}`,
			"Regenerating the theme from the current (tweaked) tokens."),
		// A final analyze with NO `files` argument exercises §5b's default
		// touched set (the turn's ChangeTracker set via list_changes) inside the
		// loop, without coupling the loop's delta-count assertions to whatever
		// else the session has touched.
		NewScriptedToolCallResponse("d2_sync_default_1", "design_sync", `{}`,
			"Confirming the sync default touched-set seam on the current session."),
		NewScriptedTextResponse("The tree carries the dev tweak; regenerated the theme from it."),
	)
	ag := dloopAgent(t, client, root)
	dloopRequireRegistered(t, ag,
		"design_assets", "design_export_tokens", "design_sync", "read_file", "write_file")

	// ------------------------------------------------------------------
	// Step 1 — design turn: a new token lands in the semantic layer.
	// ------------------------------------------------------------------
	if _, err := ag.ProcessQuery(
		"Add a new accent color token to the design system and export the theme."); err != nil {
		t.Fatalf("design turn ProcessQuery: %v%s", err, dloopDumpMessages(ag))
	}

	_, assetsOut := dloopToolMessage(t, ag, "d_assets_1")
	if strings.Contains(assetsOut, "unknown tool") {
		t.Fatalf("design_assets was not dispatched: %s", assetsOut)
	}

	_, writeOut := dloopToolMessage(t, ag, "d_write_1")
	if strings.Contains(writeOut, "unknown tool") || strings.Contains(writeOut, "blocked") {
		t.Fatalf("the design turn's token write did not run: %s", writeOut)
	}
	if !strings.Contains(writeOut, "color.tokens.json") {
		t.Errorf("write_file must confirm the token file it wrote: %s", writeOut)
	}
	if got := dloopRead(t, root, dloopTokenFile); got != dloopTokensAfterDesign {
		t.Fatalf("the design turn's token file differs from the scripted content:\n got %s\nwant %s",
			got, dloopTokensAfterDesign)
	}

	// The token is on disk in the semantic layer: the tree's own parser resolves
	// the new leaf. Reading through the design package (not the fixture) is what
	// makes this a tree assertion rather than a string compare.
	tokens, err := design.ResolveExportTokens(root)
	if err != nil {
		t.Fatalf("ResolveExportTokens after the design turn: %v", err)
	}
	if !dloopTokenLeafExists(tokens, dloopNewTokenPath, dloopNewTokenValue) {
		t.Fatalf("the design turn must have added %s=%s; leaves: %v",
			dloopNewTokenPath, dloopNewTokenValue, dloopLeafDigest(tokens))
	}

	_, exportOut := dloopToolMessage(t, ag, "d_export_1")
	if strings.Contains(exportOut, "unknown tool") || strings.Contains(exportOut, "blocked") {
		t.Fatalf("design_export_tokens was not dispatched: %s", exportOut)
	}
	if strings.Contains(exportOut, "refused") || strings.Contains(exportOut, "no tokens found") {
		t.Fatalf("the export must accept the new token tree: %s", exportOut)
	}

	// Step 2 — export: design/generated/ reflects the new token. Each generated
	// target must carry the design turn's value, so the export is complete
	// rather than "a file exists".
	generatedCSS := dloopRead(t, root, "design/generated/tokens.css")
	if !strings.Contains(generatedCSS, dloopNewTokenVar+": "+dloopNewTokenValue) {
		t.Errorf("the generated theme must carry the new token %s: %s\n%s",
			dloopNewTokenVar, dloopNewTokenValue, generatedCSS)
	}
	// Provenance: the export carries the §5f source hash, so the export step is
	// verifiable offline (not just "a file exists").
	if !strings.Contains(generatedCSS, "source-hash:") {
		t.Errorf("the generated theme must carry its provenance header:\n%s", generatedCSS)
	}
	for _, artifact := range []string{
		"design/generated/tokens.ts",
		"design/generated/tailwind.theme.css",
		"design/generated/tokens.swift",
		"design/generated/tokens.kt",
	} {
		got := dloopRead(t, root, artifact)
		if !strings.Contains(got, dloopNewTokenValue) {
			t.Errorf("%s must be generated for the new token set (value %s missing):\n%s",
				artifact, dloopNewTokenValue, got)
		}
	}

	// ------------------------------------------------------------------
	// Step 3 + 4a — dev turn: consumes the token, tweaks it, then design_sync
	// ANALYZE (read-only) over the change.
	// ------------------------------------------------------------------
	if _, err := ag.ProcessQuery(
		"Use the exported token in the app theme, adjust its value, and report the semantic deltas."); err != nil {
		t.Fatalf("dev turn ProcessQuery: %v%s", err, dloopDumpMessages(ag))
	}

	_, readGenOut := dloopToolMessage(t, ag, "v_read_gen_1")
	if strings.Contains(readGenOut, "unknown tool") || strings.Contains(readGenOut, "blocked") {
		t.Fatalf("the dev turn could not read the generated theme: %s", readGenOut)
	}
	if !strings.Contains(readGenOut, dloopNewTokenVar) {
		t.Errorf("the generated theme the dev turn reads must name the token var %s:\n%s",
			dloopNewTokenVar, readGenOut)
	}

	_, devWriteOut := dloopToolMessage(t, ag, "v_write_1")
	if strings.Contains(devWriteOut, "unknown tool") || strings.Contains(devWriteOut, "blocked") {
		t.Fatalf("the dev turn's theme write did not run: %s", devWriteOut)
	}
	if got := dloopRead(t, root, dloopDevCodeFile); got != dloopDevThemeCSS {
		t.Fatalf("the dev theme on disk differs from the scripted content:\n got %s\nwant %s",
			got, dloopDevThemeCSS)
	}

	// The analyze report the scripted dev turn (really) consumed: the same pure
	// core, over the same state, must name the literal token delta with its DTCG
	// file+entry, and its plan must write exactly that DTCG file (design/-only).
	report, plan := dloopAnalyzeProbe(t)
	if report.DeltaCount != 1 {
		t.Errorf("the dev change introduces exactly 1 semantic delta, got %d: %s",
			report.DeltaCount, dloopJSON(report.Deltas))
	}
	if report.ByBasis[string(design.DeltaBasisLiteral)] != 1 {
		t.Errorf("the dev revalue must be a literal delta, byBasis=%v: %s",
			report.ByBasis, dloopJSON(report.Deltas))
	}
	literal := dloopLiteralTokenDelta(report)
	if literal == nil {
		t.Fatalf("the report must carry a literal token delta: %s", dloopJSON(report.Deltas))
	}
	if literal.Token != dloopNewTokenPath {
		t.Errorf("the literal delta's token = %q, want %q", literal.Token, dloopNewTokenPath)
	}
	if literal.TokenFile != dloopTokenFile {
		t.Errorf("the literal delta's DTCG file = %q, want %q", literal.TokenFile, dloopTokenFile)
	}
	if literal.TokenEntry != dloopNewTokenPath {
		t.Errorf("the literal delta's DTCG entry = %q, want %q", literal.TokenEntry, dloopNewTokenPath)
	}
	if !literal.SafeToApply {
		t.Errorf("a literal revalue must be safe to apply: %+v", literal)
	}
	if !strings.Contains(literal.Delta, "revalued") {
		t.Errorf("the delta must describe the revalue: %q", literal.Delta)
	}
	if !dloopStringsContain(literal.DesignFiles, dloopTokenFile) {
		t.Errorf("the delta's design files must name %s: %v", dloopTokenFile, literal.DesignFiles)
	}
	if !plan.IsConfinedToDesign() {
		t.Errorf("§5e: the apply plan must be design/-confined: %s", dloopJSON(plan.WritePaths))
	}
	if !dloopStringsContain(plan.WritePaths, dloopTokenFile) {
		t.Errorf("the apply plan must write %s (the dev revalue's DTCG entry), got %v",
			dloopTokenFile, plan.WritePaths)
	}
	for _, w := range plan.Writes {
		if w.Path == dloopDevCodeFile {
			t.Errorf("§5e: the plan must never write the implementation: %+v", w)
		}
	}

	// ANALYZE reported the delta in its summary and wrote NOTHING. The token
	// source is compared across the analyze call (turn 2a) to prove read-only
	// behaviour — the apply has not run yet, so this is a real before/after.
	tokenSourceBeforeAnalyze := dloopRead(t, root, dloopTokenFile)
	if tokenSourceBeforeAnalyze != dloopTokensAfterDesign {
		t.Fatalf("analyze must have written nothing; token source:\n got %s\nwant %s",
			tokenSourceBeforeAnalyze, dloopTokensAfterDesign)
	}
	_, analyzeOut := dloopToolMessage(t, ag, "v_sync_analyze_1")
	if strings.Contains(analyzeOut, "unknown tool") || strings.Contains(analyzeOut, "blocked") {
		t.Fatalf("design_sync analyze was not dispatched: %s", analyzeOut)
	}
	for _, want := range []string{"design_sync (analyze)", "1 touched file(s)", "1 semantic delta(s)", "1 literal", "safe to apply"} {
		if !strings.Contains(analyzeOut, want) {
			t.Errorf("the analyze summary must carry %q:\n%s", want, analyzeOut)
		}
	}

	// ------------------------------------------------------------------
	// Step 4b — design_sync APPLY: the tree adopts the tweak.
	// ------------------------------------------------------------------
	if _, err := ag.ProcessQuery("Apply the sync report so design/ adopts the tweak."); err != nil {
		t.Fatalf("apply turn ProcessQuery: %v%s", err, dloopDumpMessages(ag))
	}

	_, applyOut := dloopToolMessage(t, ag, "v_sync_apply_1")
	if strings.Contains(applyOut, "unknown tool") || strings.Contains(applyOut, "blocked") {
		t.Fatalf("design_sync apply was not dispatched: %s", applyOut)
	}
	for _, want := range []string{"design_sync (apply)", dloopTokenFile, "never rewritten"} {
		if !strings.Contains(applyOut, want) {
			t.Errorf("the apply summary must carry %q:\n%s", want, applyOut)
		}
	}
	// §5e, at the turn level: the dev's own code file is untouched by apply.
	if got := dloopRead(t, root, dloopDevCodeFile); got != dloopDevThemeCSS {
		t.Errorf("apply must never rewrite the implementation; %s changed:\n%s", dloopDevCodeFile, got)
	}

	// The design tree reflects the tweak: the DTCG entry now carries the dev
	// value, and the pre-tweak design value is gone from the token source.
	tokenSourceAfterApply := dloopRead(t, root, dloopTokenFile)
	if !strings.Contains(tokenSourceAfterApply, dloopDevTokenValue) {
		t.Errorf("the design tree must reflect the dev tweak %s:\n%s",
			dloopDevTokenValue, tokenSourceAfterApply)
	}
	if strings.Contains(tokenSourceAfterApply, dloopNewTokenValue) {
		t.Errorf("the design token must no longer carry the pre-tweak value %s:\n%s",
			dloopNewTokenValue, tokenSourceAfterApply)
	}
	tokensAfterApply, err := design.ResolveExportTokens(root)
	if err != nil {
		t.Fatalf("ResolveExportTokens after apply: %v", err)
	}
	if !dloopTokenLeafExists(tokensAfterApply, dloopNewTokenPath, dloopDevTokenValue) {
		t.Fatalf("the tree's own parser must resolve %s=%s after apply; leaves: %v",
			dloopNewTokenPath, dloopDevTokenValue, dloopLeafDigest(tokensAfterApply))
	}
	// The pre-existing token is untouched: the loop moved one value, not the tree.
	if !dloopTokenLeafExists(tokensAfterApply, "color.brand.primary", "#0055ff") {
		t.Errorf("apply must leave color.brand.primary intact; leaves: %v", dloopLeafDigest(tokensAfterApply))
	}
	// The loop is a real state change, not a fixture echo: apply moved the tree
	// off the value the analyze half (and the design turn) saw.
	if tokenSourceBeforeAnalyze == tokenSourceAfterApply {
		t.Fatalf("the design tree did not change across design_sync apply; the assertions above would pass vacuously")
	}

	// ------------------------------------------------------------------
	// Step 5 — next design turn builds on the updated tree.
	// ------------------------------------------------------------------
	if _, err := ag.ProcessQuery(
		"Continue building the design system from the current token state."); err != nil {
		t.Fatalf("next design turn ProcessQuery: %v%s", err, dloopDumpMessages(ag))
	}

	_, readTokensOut := dloopToolMessage(t, ag, "d2_read_tokens_1")
	if strings.Contains(readTokensOut, "unknown tool") || strings.Contains(readTokensOut, "blocked") {
		t.Fatalf("the next design turn could not read the token source: %s", readTokensOut)
	}
	// The next design turn SEES the dev tweak in its context: it starts from the
	// current truth, not the pre-dev past.
	if !strings.Contains(readTokensOut, dloopDevTokenValue) {
		t.Errorf("the next design turn must read the dev's tweaked value %s (build on it, not against it):\n%s",
			dloopDevTokenValue, readTokensOut)
	}
	if strings.Contains(readTokensOut, dloopNewTokenValue) {
		t.Errorf("the next design turn must not be shown the superseded value %s:\n%s",
			dloopNewTokenValue, readTokensOut)
	}

	// The next design turn builds FORWARD: regenerating the theme from the
	// current tokens produces the dev value, so the loop is consistent end to
	// end and the generated layer never argues with the code.
	_, export2Out := dloopToolMessage(t, ag, "d2_export_1")
	if strings.Contains(export2Out, "unknown tool") || strings.Contains(export2Out, "blocked") ||
		strings.Contains(export2Out, "refused") {
		t.Fatalf("the next design turn's export failed: %s", export2Out)
	}
	finalCSS := dloopRead(t, root, "design/generated/tokens.css")
	if !strings.Contains(finalCSS, dloopNewTokenVar+": "+dloopDevTokenValue) {
		t.Errorf("the regenerated theme must carry the dev value %s:\n%s",
			dloopDevTokenValue, finalCSS)
	}
	if strings.Contains(finalCSS, dloopNewTokenValue) {
		t.Errorf("the regenerated theme must not revert to the pre-tweak design value %s:\n%s",
			dloopNewTokenValue, finalCSS)
	}

	// §5b's default touched-set seam: with no `files` argument, design_sync must
	// read the turn's ChangeTracker set (via list_changes) rather than analysing
	// nothing — the loop's dev turn needs no bookkeeping to end with sync.
	_, defaultSyncOut := dloopToolMessage(t, ag, "d2_sync_default_1")
	if strings.Contains(defaultSyncOut, "unknown tool") || strings.Contains(defaultSyncOut, "blocked") {
		t.Fatalf("design_sync (default touched set) was not dispatched: %s", defaultSyncOut)
	}
	if !strings.Contains(defaultSyncOut, "Touched set: the turn's changed files") {
		t.Errorf("an omitted `files` argument must resolve to the turn's ChangeTracker set:\n%s", defaultSyncOut)
	}
	// …and it must be a real, non-empty set: the session changed files (the
	// design turn's token/generated writes and the dev theme file), so a
	// "(0)"/"No touched code files" result means the seam silently degraded.
	if strings.Contains(defaultSyncOut, "changed files (0)") ||
		strings.Contains(defaultSyncOut, "No touched code files to analyse") {
		t.Errorf("the turn's ChangeTracker set must be non-empty in this session:\n%s", defaultSyncOut)
	}

	// ------------------------------------------------------------------
	// Turn shape + continuity across the turns
	// ------------------------------------------------------------------
	dloopAssertToolThreading(t, ag)

	// The turns are one continuous conversation: the final provider
	// request must carry the dev tweak the next design turn read, i.e. its
	// context is the post-loop tree, not a fresh empty session.
	reqs := client.GetSentRequests()
	if len(reqs) < 13 {
		t.Fatalf("expected at least 13 provider requests (one per scripted step + turns), got %d", len(reqs))
	}
	finalReq := reqs[len(reqs)-1]
	var sawTweakedTokens bool
	for _, m := range finalReq {
		if m.Role == "tool" && m.ToolCallID == "d2_read_tokens_1" {
			sawTweakedTokens = true
			if !strings.Contains(m.Content, dloopDevTokenValue) {
				t.Errorf("the final request must carry the tweaked token source, got: %s", m.Content)
			}
		}
	}
	if !sawTweakedTokens {
		t.Errorf("conversation continuity broken: the final turn did not see the tweaked token source")
	}
}

// ---------------------------------------------------------------------------
// Small assertion helpers
// ---------------------------------------------------------------------------

// dloopLiteralTokenDelta returns the report's single literal token delta, or nil
// when the report carries none (or more than one, which would make the fixture's
// "one revalue" premise false).
func dloopLiteralTokenDelta(report *design.SyncReport) *design.SyncDelta {
	if report == nil {
		return nil
	}
	var found *design.SyncDelta
	for i := range report.Deltas {
		d := report.Deltas[i]
		if d.Basis != design.DeltaBasisLiteral || d.Kind != design.DeltaKindToken {
			continue
		}
		if found != nil {
			return nil
		}
		found = &d
	}
	return found
}

// dloopStringsContain reports whether the slice contains want.
func dloopStringsContain(haystack []string, want string) bool {
	for _, s := range haystack {
		if s == want {
			return true
		}
	}
	return false
}

// dloopAssertToolThreading verifies the session's raw state threads every tool
// call to its result: every assistant message carrying tool calls must be
// immediately followed by a tool result for each call ID, and no two assistant
// messages may be adjacent. A turn whose calls went unanswered would still have
// produced the on-disk artifacts the loop asserts on, so this is the check that
// the messages the MODEL saw were well-formed too.
//
// Kept local (a dloop-prefixed copy of the same fix as the round-trip file's
// helper) so this loop test carries its own assertions rather than depending on
// another test file's helper surviving in the same package.
func dloopAssertToolThreading(t *testing.T, ag *Agent) {
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
		t.Errorf("tool threading: %d call(s) without a result; messages: %s", missing, dloopDumpMessages(ag))
	}
	if consecutive != 0 {
		t.Errorf("tool threading: %d consecutive assistant message(s)", consecutive)
	}
}
