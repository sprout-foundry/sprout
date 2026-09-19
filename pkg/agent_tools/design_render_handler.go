//go:build !js

package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// designRenderHandler implements ToolHandler for the design_render tool
// (SP-140-2 §2c). It rasterizes a design source — a wireframe/icon SVG, a
// screen HTML file, or a mermaid flow (.mmd) — to a PNG via the host browser,
// attaches the image through the SP-137 tool-result image path, and (when a
// vision tier is available) returns an analysis of it for critique.
//
// It is browser- and vision-dependent, so it is a //go:build !js file with a
// WASM stub in design_render_handler_js.go (mirroring all_vision.go). The
// registration is build-tagged and lives in neither the shared AllTools list
// nor the WASM roster (SP-140 invariant 7).
type designRenderHandler struct{}

func (h *designRenderHandler) Name() string { return "design_render" }

func (h *designRenderHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "design_render",
		Description: "Render a design source to an image for critique: a wireframe/icon SVG, a " +
			"self-contained screen HTML file, or a mermaid flow (.mmd). Renders it in a headless " +
			"browser and returns the image attached (vision-capable models see the pixels) plus an " +
			"analysis. Use this to see your own output before judging it or after a revision. " +
			"SVG and HTML render directly; a .mmd source is rendered by generating a standalone " +
			"HTML page with the pinned vendored mermaid script — the .mmd source is never modified. " +
			"Pass flow_layout to nudge a flow's orientation (top-down/bottom-up/left-right/right-left) " +
			"as a render-time concern.",
		Parameters: []ParameterDef{
			{
				Name:        "source",
				Type:        "string",
				Required:    true,
				Description: "Design source to render: a workspace-relative SVG/HTML path or a mermaid .mmd flow path (e.g. `design/wireframes/login.svg`, `design/screens/login.html`, `design/flows/sign-up.mmd`).",
			},
			{
				Name:        "analysis_prompt",
				Type:        "string",
				Required:    false,
				Description: "Optional custom critique prompt passed through to the vision tier (e.g. `does the primary action read clearly?`).",
			},
			{
				Name:        "viewport_width",
				Type:        "integer",
				Required:    false,
				Description: "Browser width in px for the render (default 1280).",
			},
			{
				Name:        "viewport_height",
				Type:        "integer",
				Required:    false,
				Description: "Browser height in px for the render (default 720).",
			},
			{
				Name:        "flow_layout",
				Type:        "string",
				Required:    false,
				Description: "Optional mermaid orientation hint applied at render time to a .mmd source: top-down (default), bottom-up, left-right, or right-left. The .mmd file itself is not modified.",
			},
		},
		Required: []string{"source"},
	}
}

func (h *designRenderHandler) Validate(args map[string]any) error {
	if _, err := extractString(args, "source"); err != nil {
		return err
	}
	if v, exists := lookupKey(args, "analysis_prompt"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'analysis_prompt' must be a string, got %T", v)
		}
	}
	if v, exists := lookupKey(args, "flow_layout"); exists && v != nil {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("parameter 'flow_layout' must be a string, got %T", v)
		}
	}
	return nil
}

// designRenderKind classifies a source path into the render strategy.
type designRenderKind int

const (
	renderKindUnknown designRenderKind = iota
	renderKindBrowser                  // SVG/HTML: render the source file directly
	renderKindMermaid                  // .mmd: render a generated standalone HTML page
)

// classifyDesignSource maps a source path to its render strategy.
func classifyDesignSource(source string) designRenderKind {
	switch GetFileExtension(source) {
	case ".svg", ".html", ".htm":
		return renderKindBrowser
	case ".mmd":
		return renderKindMermaid
	default:
		return renderKindUnknown
	}
}

func (h *designRenderHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	source, err := extractString(args, "source")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}
	source = strings.TrimSpace(source)

	kind := classifyDesignSource(source)
	if kind == renderKindUnknown {
		msg := fmt.Sprintf("design_render: unsupported source %q — expected a .svg, .html/.htm, or .mmd file", source)
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_render", msg, nil)
	}

	analysisPrompt := stringArg(args, "analysis_prompt")
	flowLayout := stringArg(args, "flow_layout")

	// Gate-1 precheck (SP-140 invariant 7): every workspace path is checked
	// before it is read, mirroring analyze_ui_screenshot. The source is a
	// local file — a URL is not a design source — so the classifier always
	// runs.
	resolvedPath, decision := PrecheckFileAccess(ctx, env.FileAccessClassifier, "design_render", source)
	if decision == "deny" {
		msg := fmt.Sprintf("design_render blocked: %s is not accessible from this session", source)
		return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_render blocked: %s is not accessible", source)
	}
	if decision == "prompt" && env.FileAccessPrompter != nil {
		if ctx2, approved := promptForOffWorkspacePath(ctx, env, "design_render", source, resolvedPath, "read"); approved {
			ctx = ctx2
		} else {
			msg := fmt.Sprintf("design_render blocked: off-workspace access to %s was not approved", source)
			return ToolResult{Output: msg, IsError: true}, fmt.Errorf("design_render blocked: off-workspace access to %s was not approved", source)
		}
	}

	viewOpts := RenderInputOptions{
		Mode:                     RenderModeBrowser,
		ViewportWidth:            viewportDim(args, "viewport_width", defaultViewportWidth),
		ViewportHeight:           viewportDim(args, "viewport_height", defaultViewportHeight),
		ReportBrowserUnavailable: false,
	}

	// A mermaid source is rendered from a generated standalone HTML page, so
	// prepare that page first and point the render helper at it. The .mmd
	// source is read here and never written back — the generated page is a
	// temp artifact whose lifetime the helper (and this cleanup) own.
	renderSource := source
	var mermaidCleanup func()
	if kind == renderKindMermaid {
		data, readErr := readDesignSource(ctx, source)
		if readErr != nil {
			msg := fmt.Sprintf("design_render: cannot read flow source %s: %v", source, readErr)
			return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_render", msg, readErr)
		}
		htmlPath, cleanup, buildErr := writeMermaidHTMLFile(string(data), normaliseFlowLayout(flowLayout))
		if buildErr != nil {
			msg := fmt.Sprintf("design_render: cannot prepare flow render page: %v", buildErr)
			return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_render", msg, buildErr)
		}
		renderSource = htmlPath
		mermaidCleanup = cleanup
	}
	if mermaidCleanup != nil {
		defer mermaidCleanup()
	}

	// rendered collects the SP-137 attachment captured inside the analyze
	// callback, while the temp PNG still exists.
	var attachment ToolResult
	result, rendered, err := renderInputToString(ctx, env, "design_render", renderSource, viewOpts,
		func(analyzeCtx context.Context, pngPath string) (string, error) {
			if att, ok := buildRenderAttachment(analyzeCtx, pngPath); ok {
				attachment = att
			}
			return AnalyzeImage(analyzeCtx, pngPath, analysisPrompt, visionModeFrontend)
		})
	if err != nil {
		return ToolResult{Output: result, IsError: true}, err
	}
	if !rendered {
		// RenderModeBrowser always renders; this is unreachable unless the
		// helper's decision logic changes. Report rather than fail silently.
		msg := fmt.Sprintf("design_render: %s could not be rendered", source)
		return ToolResult{Output: msg, IsError: true}, agenterrors.NewTool("design_render", msg, nil)
	}

	summary := buildDesignRenderSummary(source, kind, analysisPrompt, result)
	attachment.Output = summary
	return attachment, nil
}

// readDesignSource reads a design source through the workspace-safe resolver
// (SP-126 VFS boundary), returning its bytes.
func readDesignSource(ctx context.Context, source string) ([]byte, error) {
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, source)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(cleanPath)
}

// buildRenderAttachment reads the rendered PNG and returns a ToolResult
// carrying its inline data-URI, mirroring analyze_image_content's SP-137
// attachment. It must be called while the PNG exists (inside the analyze
// callback). A read failure degrades to no attachment — the analysis text is
// still valuable.
func buildRenderAttachment(ctx context.Context, pngPath string) (ToolResult, bool) {
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, pngPath)
	if err != nil {
		return ToolResult{}, false
	}
	info, err := os.Stat(cleanPath)
	if err != nil || info.IsDir() || info.Size() > maxInlineImageBytes {
		return ToolResult{}, false
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return ToolResult{}, false
	}
	return ToolResult{
		Images: []ImageData{{
			URI:      "data:image/png;base64," + base64.StdEncoding.EncodeToString(data),
			MIMEType: "image/png",
		}},
	}, true
}

// buildDesignRenderSummary composes the human-readable result: what was
// rendered, how, and — for a non-vision primary — the SP-137 fallback
// guidance, so the tool reports the artifact instead of failing.
func buildDesignRenderSummary(source string, kind designRenderKind, analysisPrompt, analysis string) string {
	var sb strings.Builder
	switch kind {
	case renderKindMermaid:
		fmt.Fprintf(&sb, "design_render: rendered flow %s (standalone mermaid page; source unchanged).", source)
	default:
		fmt.Fprintf(&sb, "design_render: rendered %s.", source)
	}
	if analysisPrompt != "" {
		sb.WriteString(" Critique prompt: " + analysisPrompt)
	}
	analysis = strings.TrimSpace(analysis)
	if analysis != "" {
		sb.WriteString("\n\n" + analysis)
	} else {
		sb.WriteString(" No vision tier was available to analyze the render; " +
			"the image is attached for a vision-capable primary, otherwise read it with " +
			"analyze_image_content (OCR/native fallback).")
	}
	return sb.String()
}

func (h *designRenderHandler) Aliases() []string      { return nil }
func (h *designRenderHandler) Timeout() time.Duration { return 0 }
func (h *designRenderHandler) MaxResultSize() int     { return 0 }
func (h *designRenderHandler) SafeForParallel() bool  { return false }
func (h *designRenderHandler) Interactive() bool      { return false }

// registerDesignRenderTools registers the design_render tool, which requires
// the host browser tier (and, for critique, the vision tier) that native
// (desktop/daemon) builds provide. Excluded from WASM builds via
// design_render_handler_js.go, which returns nil — mirroring
// registerVisionTools/all_vision.go (SP-140 invariant 7, SP-140-2 §2c).
func registerDesignRenderTools() []ToolHandler {
	return []ToolHandler{
		&designRenderHandler{},
	}
}
