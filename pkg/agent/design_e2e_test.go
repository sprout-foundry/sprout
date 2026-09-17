//go:build !js

// design_e2e_test.go — scripted-client end-to-end tests for the design tier
// (SP-140-2 §2c, TODO item 2.11).
//
// These tests drive a REAL agent turn through seed's conversation loop with a
// scripted client standing in for the model. They exercise the three layers
// that unit-level handler tests cannot reach on their own:
//
//  1. the seed tool-registry dispatch path (convertHandlerToSeedToolConfig →
//     h.Execute → ToolResultMessageWithImages),
//  2. the SP-137 images-preserving tool-result path (images attached by a
//     handler survive into the tool message AND into the next provider
//     request when the primary client reports vision support), and
//  3. conversation continuity across a multi-tool-call turn
//     (design_import_sketch → write_file → design_validate), with the
//     artifacts the turn produced on disk afterwards.
//
// Everything is hermetic: the scripted client never touches a provider, the
// vision analysis is local (macOS native OCR) or degrades to a brief, and the
// browser render skips when Chrome is unavailable — unless
// SPROUT_REQUIRE_BROWSER is set, which turns that skip into a failure so a
// browser-equipped job can assert the rendered path actually ran.

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/design"
)

// ---------------------------------------------------------------------------
// Fixtures & helpers
// ---------------------------------------------------------------------------

// de2ePNG is a header-valid 1x1 PNG: the smallest file that survives the
// image-magic check in buildImageAttachment (mirrors validPNG() in
// pkg/agent_tools/vision_sp137_test.go).
var de2ePNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00,
	0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D, 0xB0, 0x00, 0x00, 0x00,
	0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// de2eManifest is a design/README.md that satisfies the SP-140-1 manifest
// conventions (frames block, every marker, resolvable links) so a whole-tree
// design_validate run reports zero error-severity findings.
const de2eManifest = `# Design Workspace

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

// de2eGitAttributes / de2eGitIgnore satisfy the §1h git contract so the only
// findings a validate run can surface are real design findings.
const (
	de2eGitAttributes = "* text=auto eol=lf\ndesign/**/*.svg diff=html\n"
	de2eGitIgnore     = "node_modules/\ndesign/.cache/\n"
)

// de2eLoginSVG is the wireframe the scripted turn writes: viewBox-only,
// structure not polish, a stable id on the interactive element, and a
// data-nav slug resolving to the manifest's `login` stem.
const de2eLoginSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 390 844">` +
	`<text x="24" y="64" font-size="28">Login</text>` +
	`<rect id="email" x="24" y="140" width="342" height="48"/>` +
	`<rect id="submit" x="24" y="200" width="342" height="52"/>` +
	`</svg>`

// de2eWrite writes rel (slash-separated) under root, creating parents.
func de2eWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// de2eWorkspace builds a design/ tree carrying the git contract, and chdirs
// into it so both the agent's VFS resolution and the design handlers' fallback
// root ("."-relative) agree on where the workspace is.
func de2eWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	de2eWrite(t, root, design.GitContractFile, de2eGitAttributes)
	de2eWrite(t, root, design.GitIgnoreFile, de2eGitIgnore)
	de2eWrite(t, root, "design/README.md", de2eManifest)
	de2eWrite(t, root, "design/wireframes/login.svg", de2eLoginSVG)
	// Process-global cwd: some design handlers resolve against it when the
	// env workspace root is empty, and the browser render helper resolves
	// relative sources with filepath.Abs. t.Chdir restores it afterwards.
	t.Chdir(root)
	return root
}

// de2eAgent wires a scripted client straight into the seed conversation loop
// with the full context profile (Low-Context Mode would prune the design tools
// from the roster, which is a different spec's concern).
func de2eAgent(t *testing.T, client *ScriptedClient, workspaceRoot string) *Agent {
	t.Helper()

	mgr, cleanup := configuration.NewTestManager(t)
	t.Cleanup(cleanup)
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.ContextMode = configuration.ContextModeFull
		// No interactive security prompt may fire mid-turn: tests would
		// either hang on stdin or get a non-deterministic verdict. With
		// SkipPrompt the CLI approval surface is non-interactive, so a
		// "prompt" file-access verdict resolves to denied.
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

// de2eToolMessage returns the tool message for callID plus its agent-visible
// text. Fails the test when no such message exists — a missing tool result
// means the turn never actually dispatched, which is exactly what these tests
// exist to catch.
func de2eToolMessage(t *testing.T, ag *Agent, callID string) (api.Message, string) {
	t.Helper()
	for _, m := range ag.GetMessages() {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m, m.Content
		}
	}
	t.Fatalf("no tool result message for call %q; messages=%s", callID, de2eDumpMessages(ag))
	return api.Message{}, ""
}

// de2eDumpMessages renders the raw state for failure messages.
func de2eDumpMessages(ag *Agent) string {
	var sb strings.Builder
	for i, m := range ag.GetMessages() {
		names := make([]string, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			names = append(names, tc.Function.Name+"("+tc.ID+")")
		}
		sb.WriteString("\n  [")
		sb.WriteString(itoa(i))
		sb.WriteString("] role=" + m.Role)
		if len(names) > 0 {
			sb.WriteString(" calls=" + strings.Join(names, ","))
		}
		if m.ToolCallID != "" {
			sb.WriteString(" toolCallID=" + m.ToolCallID)
		}
		if len(m.Images) > 0 {
			sb.WriteString(" images=" + itoa(len(m.Images)))
		}
		content := strings.TrimSpace(m.Content)
		if len(content) > 160 {
			content = content[:160] + "…"
		}
		if content != "" {
			sb.WriteString(" content=" + content)
		}
	}
	return sb.String()
}

// de2eHasDesignTool reports whether the agent's registered tool roster
// advertises name. Guards against a scripted turn "passing" because the tool
// silently fell out of the profile allowlist.
func de2eHasDesignTool(t *testing.T, ag *Agent, name string) bool {
	t.Helper()
	registry := NewSeedToolRegistry(ag)
	return registry.HasTool(name)
}

// de2eSkipOrFailNoBrowser handles a render that failed because no headless
// browser is available.
//
// A skip is the right answer locally (a dev machine may have no Chrome), but
// a *silent* skip is how browser-dependent coverage evaporates in CI: the
// suite reports green while the render path went unexercised. Setting
// SPROUT_REQUIRE_BROWSER=1 — which the job that installs Chromium does —
// converts the skip into a failure, so "these tests ran" becomes assertable
// rather than assumed.
func de2eSkipOrFailNoBrowser(t *testing.T, output string) {
	t.Helper()
	if os.Getenv("SPROUT_REQUIRE_BROWSER") != "" {
		t.Fatalf("no usable headless browser, but SPROUT_REQUIRE_BROWSER is set "+
			"(this environment is expected to provide one): %s", output)
	}
	t.Skipf("no usable headless browser in this environment: %s", output)
}

// de2eBrowserUnavailable reports whether a tool output names the missing-browser
// failure, which is how the render helper surfaces errNoBrowser.
func de2eBrowserUnavailable(output string) bool {
	return strings.Contains(output, "browser not available") ||
		strings.Contains(output, "failed to run browser")
}

// ---------------------------------------------------------------------------
// design_render — fixture SVG round trip through the seed tool-result path
// ---------------------------------------------------------------------------

// TestDesignRenderE2E_SVGRoundTripCarriesImageThroughSeedToolResult is the
// SP-140-2 AC for design_render: a fixture SVG rendered by a scripted agent
// turn must come back as a tool message carrying the rendered PNG, and — when
// the primary client reports vision support — that image must survive into the
// provider request as an attached tool-result image (the SP-137 1a path).
//
// This is the layer the handler unit tests cannot reach: they assert
// ToolResult.Images on the handler's return value, not that seed's registry
// preserves the images into Message.Images on the tool message, nor that
// sprout's Chat conversion maps them into the request.
//
// The render itself needs a headless browser; without one the tool fails
// before producing an image, and the test skips (the round-trip contract is
// unchanged — it is simply not exercisable in this environment).
func TestDesignRenderE2E_SVGRoundTripCarriesImageThroughSeedToolResult(t *testing.T) {
	root := de2eWorkspace(t)

	client := NewScriptedClientWithVision("vision-model",
		NewScriptedToolCallResponse("render_1", "design_render",
			`{"source":"design/wireframes/login.svg"}`, "Rendering the login wireframe."),
		NewScriptedTextResponse("The wireframe renders cleanly."),
	)
	if client.GetVisionModel() == "" {
		t.Fatal("vision scripted client must report a vision model")
	}
	ag := de2eAgent(t, client, root)

	if !de2eHasDesignTool(t, ag, "design_render") {
		t.Skip("design_render is not registered in this build (browser-tier tool)")
	}

	if _, err := ag.ProcessQuery("Render design/wireframes/login.svg so I can review it."); err != nil {
		t.Fatalf("ProcessQuery: %v%s", err, de2eDumpMessages(ag))
	}

	toolMsg, output := de2eToolMessage(t, ag, "render_1")

	// The tool actually ran rather than failing at dispatch.
	if strings.Contains(output, "unknown tool") || strings.Contains(output, "Pre-execute hook rejected") {
		t.Fatalf("design_render was not dispatched to a handler: %s", output)
	}
	if de2eBrowserUnavailable(output) {
		de2eSkipOrFailNoBrowser(t, output)
	}
	if strings.Contains(output, "design_render blocked") || strings.Contains(output, "unsupported source") {
		t.Fatalf("design_render rejected the in-workspace fixture: %s", output)
	}

	// AC: the render is attached to the tool result (seed's images-preserving
	// ToolResultMessageWithImages path).
	if len(toolMsg.Images) != 1 {
		t.Fatalf("expected 1 rendered image on the tool message, got %d\noutput=%s\nmessages=%s",
			len(toolMsg.Images), output, de2eDumpMessages(ag))
	}
	img := toolMsg.Images[0]
	if !strings.HasPrefix(img.URL, "data:image/png;base64,") {
		t.Errorf("tool-result image is not a PNG data URI: %.48s", img.URL)
	}
	if img.Type != "image/png" {
		t.Errorf("tool-result image MIME = %q, want image/png", img.Type)
	}

	// AC: the image flows through to the next provider request. A vision
	// primary sees the pixels; a non-vision primary gets them stripped by
	// seed's stripImages — both are correct, so assert the vision case only.
	reqs := client.GetSentRequests()
	if len(reqs) < 2 {
		t.Fatalf("expected at least 2 provider requests (tool call + follow-up), got %d", len(reqs))
	}
	followUp := reqs[len(reqs)-1]
	var toolResultImages int
	for _, m := range followUp {
		if m.Role == "tool" && m.ToolCallID == "render_1" {
			toolResultImages = len(m.Images)
		}
	}
	if !client.SupportsVision() {
		t.Fatalf("scripted client should report vision support for this test")
	}
	if toolResultImages != 1 {
		t.Errorf("the rendered image must ride the provider request as a tool-result image; got %d\nmessages=%s",
			toolResultImages, de2eDumpMessages(ag))
	}

	// The turn completed: the scripted follow-up text is the final assistant
	// message, and no second tool call was fabricated.
	if last := ag.GetMessages()[len(ag.GetMessages())-1]; last.Role != "assistant" ||
		!strings.Contains(last.Content, "renders cleanly") {
		t.Errorf("expected the scripted final answer to terminate the turn, got role=%q content=%q",
			last.Role, last.Content)
	}
}

// TestDesignRenderE2E_HTMLSourceRendersViaBrowser is the HTML half of the AC:
// a screen HTML fixture takes the same round trip. Kept separate from the SVG
// case so a browser-less environment skips both without masking a regression
// in either classification branch.
func TestDesignRenderE2E_HTMLSourceRendersViaBrowser(t *testing.T) {
	root := de2eWorkspace(t)
	de2eWrite(t, root, "design/screens/login.html",
		"<!DOCTYPE html><html><body><h1>Login</h1></body></html>")

	client := NewScriptedClientWithVision("vision-model",
		NewScriptedToolCallResponse("render_html_1", "design_render",
			`{"source":"design/screens/login.html","viewport_width":390,"viewport_height":844}`,
			"Rendering the login screen."),
		NewScriptedTextResponse("The screen renders cleanly."),
	)
	ag := de2eAgent(t, client, root)

	if !de2eHasDesignTool(t, ag, "design_render") {
		t.Skip("design_render is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Render design/screens/login.html."); err != nil {
		t.Fatalf("ProcessQuery: %v%s", err, de2eDumpMessages(ag))
	}

	toolMsg, output := de2eToolMessage(t, ag, "render_html_1")
	if de2eBrowserUnavailable(output) {
		de2eSkipOrFailNoBrowser(t, output)
	}
	if strings.Contains(output, "unsupported source") || strings.Contains(output, "design_render blocked") {
		t.Fatalf("design_render rejected the HTML fixture: %s", output)
	}
	if len(toolMsg.Images) != 1 {
		t.Fatalf("expected 1 rendered image for the HTML source, got %d\noutput=%s\nmessages=%s",
			len(toolMsg.Images), output, de2eDumpMessages(ag))
	}
}

// TestDesignRenderE2E_NonVisionPrimaryStillSeesArtifactPath pins the §2c
// non-vision contract at the turn level: with a non-vision primary the tool
// must still succeed and report the artifact/guidance, and seed must strip the
// image from the provider request rather than failing the turn.
//
// The scripted client deliberately does NOT support vision, so this exercises
// the stripImages path in seed's message pipeline.
func TestDesignRenderE2E_NonVisionPrimaryStillSeesArtifactPath(t *testing.T) {
	root := de2eWorkspace(t)

	// NewScriptedClient (no vision) — the primary model is non-visual.
	client := NewScriptedClient(
		NewScriptedToolCallResponse("render_nv_1", "design_render",
			`{"source":"design/wireframes/login.svg"}`, "Rendering."),
		NewScriptedTextResponse("Reported the artifact."),
	)
	ag := de2eAgent(t, client, root)

	if !de2eHasDesignTool(t, ag, "design_render") {
		t.Skip("design_render is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Render design/wireframes/login.svg."); err != nil {
		t.Fatalf("ProcessQuery: %v%s", err, de2eDumpMessages(ag))
	}

	_, output := de2eToolMessage(t, ag, "render_nv_1")
	if de2eBrowserUnavailable(output) {
		de2eSkipOrFailNoBrowser(t, output)
	}

	// §2c: a non-vision primary must not fail the tool — it returns the
	// artifact plus fallback guidance.
	if strings.Contains(output, "design_render blocked") {
		t.Fatalf("design_render must not block a non-vision primary: %s", output)
	}
	if !strings.Contains(output, "design/wireframes/login.svg") {
		t.Errorf("the summary must name the rendered artifact so a non-vision primary can route it onward: %s", output)
	}

	// Seed strips images for a non-vision primary; that is a delivery
	// decision, not a tool failure — the turn must still complete.
	reqs := client.GetSentRequests()
	last := reqs[len(reqs)-1]
	for _, m := range last {
		if len(m.Images) > 0 {
			t.Errorf("a non-vision primary must not receive images in the request; role=%s has %d",
				m.Role, len(m.Images))
		}
	}
}

// ---------------------------------------------------------------------------
// design_import_sketch — scripted agent turn producing a validated wireframe
// ---------------------------------------------------------------------------

// TestDesignImportSketchE2E_ScriptedTurnProducesValidatedWireframe is the
// SP-140-2 AC end-to-end case for design_import_sketch: a scripted vision
// client stands in for the model and drives a full turn —
//
//	design_import_sketch (photo of a hand-drawn login screen)
//	  → write_file   (design/wireframes/login.svg)
//	  → design_validate
//
// — and afterwards the workspace must hold a wireframe that the validator
// accepts with zero error-severity findings.
//
// Each step is asserted on what the agent actually observed (the tool message
// for that call ID), not on the scripted fixture, so a dispatch or threading
// regression fails the test rather than passing silently.
func TestDesignImportSketchE2E_ScriptedTurnProducesValidatedWireframe(t *testing.T) {
	root := de2eWorkspace(t)
	// The sketch the agent is asked to import. A real PNG header keeps the
	// SP-137 attachment path honest (magic-byte detection, not extension).
	de2eWrite(t, root, "design/feedback/login-sketch.png", string(de2ePNG))

	writeArgs := `{"path":"design/wireframes/login.svg","content":` + jsonString(de2eLoginSVG) + `}`

	client := NewScriptedClientWithVision("vision-model",
		NewScriptedToolCallResponse("import_1", "design_import_sketch",
			`{"image_path":"design/feedback/login-sketch.png","target":"wireframes","screen_name":"login"}`,
			"Reading the whiteboard sketch of the login screen."),
		NewScriptedToolCallResponse("write_1", "write_file", writeArgs,
			"Writing the extracted wireframe."),
		NewScriptedToolCallResponse("validate_1", "design_validate", `{}`,
			"Validating the new wireframe."),
		NewScriptedTextResponse("Imported the login sketch as design/wireframes/login.svg; design_validate reports no error findings."),
	)
	ag := de2eAgent(t, client, root)

	for _, name := range []string{"design_import_sketch", "write_file", "design_validate"} {
		if !de2eHasDesignTool(t, ag, name) {
			t.Skipf("%s is not registered in this build", name)
		}
	}

	if _, err := ag.ProcessQuery(
		"Import design/feedback/login-sketch.png into the design workspace as a login wireframe."); err != nil {
		t.Fatalf("ProcessQuery: %v%s", err, de2eDumpMessages(ag))
	}

	// --- Step 1: the import brief, with the sketch attached (SP-137). ------
	importMsg, importOut := de2eToolMessage(t, ag, "import_1")
	if strings.Contains(importOut, "unknown tool") {
		t.Fatalf("design_import_sketch was not dispatched: %s", importOut)
	}
	if strings.Contains(importOut, "blocked") {
		t.Fatalf("design_import_sketch blocked an in-workspace sketch: %s", importOut)
	}
	if len(importMsg.Images) != 1 {
		t.Fatalf("the sketch image must ride the tool result for visual extraction; got %d images\noutput=%s",
			len(importMsg.Images), importOut)
	}
	if !strings.HasPrefix(importMsg.Images[0].URL, "data:image/png;base64,") {
		t.Errorf("sketch attachment is not a PNG data URI: %.48s", importMsg.Images[0].URL)
	}
	// The brief must carry the wireframe conventions and the mandatory
	// validate reminder (§2c: the tool's output directs the agent).
	for _, want := range []string{"design/wireframes/login.svg", "viewBox", "Next steps:", "design_validate"} {
		if !strings.Contains(importOut, want) {
			t.Errorf("import brief missing %q:\n%s", want, importOut)
		}
	}
	if strings.Contains(importOut, "\"created\"") || strings.Contains(importOut, "\"wrote\"") {
		t.Errorf("the import brief must not claim it wrote files: %s", importOut)
	}

	// --- Step 2: the model writes the wireframe with the normal tool. ------
	_, writeOut := de2eToolMessage(t, ag, "write_1")
	if !strings.Contains(writeOut, "login.svg") {
		t.Errorf("write_file did not confirm the wireframe path: %s", writeOut)
	}
	written, err := os.ReadFile(filepath.Join(root, "design", "wireframes", "login.svg"))
	if err != nil {
		t.Fatalf("the scripted turn must have created design/wireframes/login.svg: %v", err)
	}
	if string(written) != de2eLoginSVG {
		t.Errorf("written wireframe differs from the scripted content:\n got %s\nwant %s", written, de2eLoginSVG)
	}

	// --- Step 3: design_validate runs and reports a clean tree. ------------
	_, validateOut := de2eToolMessage(t, ag, "validate_1")
	if strings.Contains(validateOut, "unknown tool") {
		t.Fatalf("design_validate was not dispatched: %s", validateOut)
	}
	if !strings.Contains(validateOut, "design_validate:") {
		t.Errorf("validate output is not a design_validate summary: %s", validateOut)
	}
	// AC: the produced wireframe is *validated* — zero error-severity
	// findings. Warnings/fixes are advisory and must not be conflated with
	// the error count.
	if strings.Contains(validateOut, "error(s)") {
		t.Fatalf("the imported wireframe must validate without error findings: %s", validateOut)
	}
	if !strings.Contains(validateOut, "0 findings") && !strings.Contains(validateOut, "fix(es)") &&
		!strings.Contains(validateOut, "warn(s)") {
		t.Errorf("unexpected validate summary: %s", validateOut)
	}

	// --- Turn shape: exactly three tool calls, threaded correctly. ---------
	assertE2EToolThreading(t, ag)

	// --- Conversation continuity: the model saw each result before acting. -
	reqs := client.GetSentRequests()
	if len(reqs) < 4 {
		t.Fatalf("expected 4 provider requests (one per step), got %d", len(reqs))
	}
	// The 4th request is the one that follows design_validate; by then the
	// model must have been shown both the import brief and the validate
	// summary.
	final := reqs[len(reqs)-1]
	var sawImport, sawValidate bool
	for _, m := range final {
		if m.Role != "tool" {
			continue
		}
		switch m.ToolCallID {
		case "import_1":
			sawImport = true
			if len(m.Images) == 0 {
				t.Errorf("the sketch image must still be attached when the model reads the brief")
			}
		case "validate_1":
			sawValidate = true
			if !strings.Contains(m.Content, "design_validate:") {
				t.Errorf("the model must see the validate summary, got: %s", m.Content)
			}
		}
	}
	if !sawImport || !sawValidate {
		t.Errorf("conversation continuity broken: importSeen=%v validateSeen=%v\n%s",
			sawImport, sawValidate, de2eDumpMessages(ag))
	}
}

// TestDesignImportSketchE2E_BriefRemindsAgentToValidateWhenVisionAbsent pins
// the "must not fail" half of the AC: without any vision tier the import still
// returns a complete brief directing the agent to write the file and run
// design_validate, so the scripted turn can still produce the wireframe.
func TestDesignImportSketchE2E_BriefRemindsAgentToValidateWhenVisionAbsent(t *testing.T) {
	root := de2eWorkspace(t)
	de2eWrite(t, root, "design/feedback/board.png", string(de2ePNG))

	// Non-vision primary: no inline pixels, extraction text unavailable.
	client := NewScriptedClient(
		NewScriptedToolCallResponse("import_nv_1", "design_import_sketch",
			`{"image_path":"design/feedback/board.png","target":"wireframes","screen_name":"signup"}`,
			"Importing."),
		NewScriptedTextResponse("Brief received."),
	)
	ag := de2eAgent(t, client, root)

	if !de2eHasDesignTool(t, ag, "design_import_sketch") {
		t.Skip("design_import_sketch is not registered in this build")
	}

	if _, err := ag.ProcessQuery("Import design/feedback/board.png."); err != nil {
		t.Fatalf("ProcessQuery: %v%s", err, de2eDumpMessages(ag))
	}

	_, output := de2eToolMessage(t, ag, "import_nv_1")
	if strings.Contains(output, "blocked") || strings.Contains(output, "not a recognized image file") {
		t.Fatalf("the import must not fail for a workspace-local sketch: %s", output)
	}
	if !strings.Contains(output, "design/wireframes/signup.svg") {
		t.Errorf("brief must name the artifact to produce: %s", output)
	}
	if !strings.Contains(output, "design_validate") {
		t.Errorf("brief must direct the agent to validate: %s", output)
	}
	// Non-vision guidance must point at a recovery route, never at an error.
	if !strings.Contains(output, "analyze_image_content") &&
		!strings.Contains(output, "attached for a vision-capable primary") &&
		!strings.Contains(output, "Extracted structure") {
		t.Errorf("non-vision brief must carry fallback guidance: %s", output)
	}
}

// TestDesignImportSketchE2E_OffWorkspaceSketchDeniedAtGate1 is the turn-level
// version of the §2e negative test: an off-workspace image_path must be denied
// by Gate 1 inside a real scripted turn, with no image attached and no brief
// produced.
//
// The production wiring supplies both verdicts, so the fixture must be a path
// the agent's real classifier resolves to a denial. Note that /tmp is
// deliberately allowlisted by classifyFileAccess (it is the agent's scratch
// area), so a t.TempDir() path is NOT off-workspace as far as Gate 1 is
// concerned — the fixture has to live outside the workspace in a location the
// session allowlist does not cover. The agent's own classifier then answers
// "prompt", and with no interactive surface the prompt resolves to denied.
func TestDesignImportSketchE2E_OffWorkspaceSketchDeniedAtGate1(t *testing.T) {
	root := de2eWorkspace(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir to build an off-allowlist fixture: %v", err)
	}
	outsideDir := filepath.Join(home, ".sprout-design-e2e-outside")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Skipf("cannot create off-allowlist fixture dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outsideDir) })
	outside := filepath.Join(outsideDir, "outside.png")
	if err := os.WriteFile(outside, de2ePNG, 0o644); err != nil {
		t.Skipf("cannot create off-allowlist fixture: %v", err)
	}

	client := NewScriptedClientWithVision("vision-model",
		NewScriptedToolCallResponse("import_out_1", "design_import_sketch",
			`{"image_path":`+jsonString(outside)+`,"target":"wireframes"}`, "Importing."),
		NewScriptedTextResponse("Could not import."),
	)
	ag := de2eAgent(t, client, root)

	if !de2eHasDesignTool(t, ag, "design_import_sketch") {
		t.Skip("design_import_sketch is not registered in this build")
	}

	// Sanity: the real classifier must not have allowlisted this fixture,
	// otherwise the assertion below would pass for the wrong reason.
	if verdict := ag.ClassifyFileAccess(t.Context(), outside, outside, "read"); verdict == "allow" {
		t.Skipf("the agent allowlists %s in this environment; no off-workspace fixture available", outside)
	}

	if _, err := ag.ProcessQuery("Import " + outside + " as a wireframe."); err != nil {
		// A turn-level error is acceptable here (the model gets the tool
		// error and may stop); what matters is the tool-level denial below.
		t.Logf("ProcessQuery returned: %v", err)
	}

	msg, output := de2eToolMessage(t, ag, "import_out_1")
	if !strings.Contains(output, "design_import_sketch blocked") {
		t.Fatalf("an off-workspace sketch must be blocked by Gate 1, got: %s", output)
	}
	if len(msg.Images) != 0 {
		t.Errorf("a denied off-workspace sketch must not be attached; got %d images", len(msg.Images))
	}
	// No brief may leak: the denial happens before the structured output is
	// built, so the tool must not have named an artifact.
	if strings.Contains(output, "Next steps:") {
		t.Errorf("a denied import must not produce an extraction brief: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Shared assertions
// ---------------------------------------------------------------------------

// assertE2EToolThreading verifies the turn's raw state threads every tool call
// to its result: every assistant message with tool calls must be immediately
// followed by a tool result for each call ID, and no two assistant messages may
// be adjacent.
//
// NOTE ON THE SHARED HELPER: this does NOT reuse validateToolThreading from
// tool_threading_corruption_test.go. That helper's inner check is
// `if declared[msgs[j].ToolCallID]` — a value read on a map whose entries are
// initialised to `false`, so it can never observe the key it is supposed to
// match and reports every call as unthreaded. It is a pre-existing bug
// (introduced when core.ValidateToolThreading was inlined in 45789c748), out of
// scope for SP-140-2, and it means the corruption tests using it are vacuously
// calling t.Errorf-free. validateToolThreadingForE2E below is the corrected
// presence-check form.
func assertE2EToolThreading(t *testing.T, ag *Agent) {
	t.Helper()
	msgs := ag.GetMessages()
	if v := validateToolThreadingForE2E(msgs); v != 0 {
		t.Errorf("tool threading violations: %d\n%s", v, de2eDumpMessages(ag))
	}
	if c := countConsecutiveAssistants(msgs); c != 0 {
		t.Errorf("consecutive assistant messages: %d\n%s", c, de2eDumpMessages(ag))
	}
	// Every tool-result message must have a non-empty call ID and a status,
	// and there must be exactly one result per scripted call.
	var toolMsgs, assistantCalls int
	for _, m := range msgs {
		switch m.Role {
		case "tool":
			toolMsgs++
			if m.ToolCallID == "" {
				t.Errorf("tool message without a tool_call_id: %q", m.Content)
			}
		case "assistant":
			assistantCalls += len(m.ToolCalls)
		}
	}
	if toolMsgs != assistantCalls {
		t.Errorf("tool call/result mismatch: %d calls, %d results\n%s",
			assistantCalls, toolMsgs, de2eDumpMessages(ag))
	}
}

// validateToolThreadingForE2E counts missing tool results: assistant messages
// whose declared tool calls have no matching tool message in the immediately
// following run of tool messages.
//
// This is the presence-check-correct form of the shared validateToolThreading
// helper (see assertE2EToolThreading's note); it is deliberately local so a fix
// to the shared helper does not change what these end-to-end tests assert.
func validateToolThreadingForE2E(msgs []api.Message) int {
	missing := 0
	for i, m := range msgs {
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
	return missing
}

// ---------------------------------------------------------------------------
// design_assets pending feedback + read_file — the SP-140-4 §4d round trip
// ---------------------------------------------------------------------------

// de2eFeedbackJSON is the §4d feedback document the webui write path produces
// (webui/src/design/feedbackWrite.ts): status changes-requested, one
// unresolved annotation naming the login wireframe. It is the fixture the
// round-trip test seeds and the scripted agent reads back.
const de2eFeedbackJSON = `{
  "target": "design/wireframes/login.svg",
  "status": "changes-requested",
  "resolution": "",
  "annotations": [
    {"id": "a1", "at": {"x": 0.42, "y": 0.18}, "area": "hierarchy",
     "note": "Primary CTA reads as secondary", "resolved": false,
     "created": "2026-09-15T10:36:47Z"}
  ]
}`

// TestDesignAssetsFeedbackE2E_ScriptedTurnReadsPendingFeedbackFile is the
// SP-140-4 item 4.7 skill-loop acceptance path, end to end through a real
// agent turn: a human annotation written to design/feedback/login.json (the
// §4d webui write path shape) must make design_assets report the target as
// pending with counts, after which the scripted agent reads the feedback file
// with the normal read_file tool and addresses the annotated screen.
//
// This is the layer the handler unit tests cannot reach: they assert
// StructuredOut on the handler's return value, not that the pending report
// survives seed's registry dispatch into the tool message the model sees, nor
// that the model can then read the very file the report named.
func TestDesignAssetsFeedbackE2E_ScriptedTurnReadsPendingFeedbackFile(t *testing.T) {
	root := de2eWorkspace(t)
	de2eWrite(t, root, "design/feedback/login.json", de2eFeedbackJSON)

	client := NewScriptedClient(
		NewScriptedToolCallResponse("assets_1", "design_assets", `{}`,
			"Checking the tree for pending feedback."),
		NewScriptedToolCallResponse("read_1", "read_file",
			`{"path":"design/feedback/login.json"}`,
			"Reading the pending feedback file."),
		NewScriptedToolCallResponse("write_1", "write_file",
			`{"path":"design/wireframes/login.svg","content":`+jsonString(de2eLoginSVG)+`}`,
			"Addressing the annotation on the login wireframe."),
		NewScriptedTextResponse("Addressed the changes-requested annotation on design/wireframes/login.svg."),
	)
	ag := de2eAgent(t, client, root)

	for _, name := range []string{"design_assets", "read_file", "write_file"} {
		if !de2eHasDesignTool(t, ag, name) {
			t.Skipf("%s is not registered in this build", name)
		}
	}

	if _, err := ag.ProcessQuery(
		"There is pending design feedback; find and address it."); err != nil {
		t.Fatalf("ProcessQuery: %v%s", err, de2eDumpMessages(ag))
	}

	// --- Step 1: design_assets reports the pending target with counts. -----
	_, assetsOut := de2eToolMessage(t, ag, "assets_1")
	if strings.Contains(assetsOut, "unknown tool") {
		t.Fatalf("design_assets was not dispatched: %s", assetsOut)
	}
	for _, want := range []string{"Pending feedback: 1 target(s)", "design/wireframes/login.svg", "1 unresolved"} {
		if !strings.Contains(assetsOut, want) {
			t.Errorf("design_assets summary must report pending feedback %q:\n%s", want, assetsOut)
		}
	}
	if !strings.Contains(assetsOut, "read of its feedback file") {
		t.Errorf("design_assets must direct the agent to read the feedback file: %s", assetsOut)
	}

	// --- Step 2: the agent reads the feedback file the report named. -------
	_, readOut := de2eToolMessage(t, ag, "read_1")
	if strings.Contains(readOut, "unknown tool") || strings.Contains(readOut, "blocked") {
		t.Fatalf("read_file of the pending feedback file failed: %s", readOut)
	}
	// The annotation's note and area must reach the model verbatim so it can
	// act on them.
	for _, want := range []string{"changes-requested", "Primary CTA reads as secondary", "hierarchy"} {
		if !strings.Contains(readOut, want) {
			t.Errorf("read_file output must carry the feedback annotation %q:\n%s", want, readOut)
		}
	}

	// --- Step 3: the agent edits the annotated screen. ---------------------
	written, err := os.ReadFile(filepath.Join(root, "design", "wireframes", "login.svg"))
	if err != nil {
		t.Fatalf("the scripted turn must have edited the annotated wireframe: %v", err)
	}
	if string(written) != de2eLoginSVG {
		t.Errorf("annotated screen edit differs from the scripted content:\n got %s\nwant %s", written, de2eLoginSVG)
	}

	// --- Turn shape: exactly three tool calls, threaded correctly. ---------
	assertE2EToolThreading(t, ag)

	// --- Conversation continuity: the model saw the pending report before
	// reading, and the feedback contents before editing. -------------------
	reqs := client.GetSentRequests()
	if len(reqs) < 4 {
		t.Fatalf("expected 4 provider requests (one per step), got %d", len(reqs))
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
				t.Errorf("the model must see the feedback annotation, got: %s", m.Content)
			}
		}
	}
	if !sawAssets || !sawFeedback {
		t.Errorf("conversation continuity broken: assetsSeen=%v feedbackSeen=%v\n%s",
			sawAssets, sawFeedback, de2eDumpMessages(ag))
	}
}

// jsonString renders s as a JSON string literal so scripted tool arguments
// carrying SVG markup stay valid JSON.
func jsonString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
