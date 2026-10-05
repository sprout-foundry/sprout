package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/design"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// provenanceHeaderPrefix opens the textual provenance banner written into
// every derived PNG. SP-140 invariant 2 requires derived artifacts to be
// recognizable as derived "so tooling can regenerate them"; a PNG carries no
// metadata field for this tool to populate (no PNG encoder is available in
// this dependency-free path), so the artifact is documented both in-band —
// these bytes, appended after IEND, are ignored by every PNG decoder — and
// out-of-band, in the tool result's provenance fields. See
// buildCritiqueProvenance.
const provenanceHeaderPrefix = "sprout:derived-artifact"

// provenanceHeaderTerminator closes the provenance banner so a reader can
// extract it without guessing a length.
const provenanceHeaderTerminator = "sprout:end-provenance"

// critiqueVisionTierAvailable reports whether a vision pass can actually reach
// a tier for this environment: either the caller wired a VisionProcessor (the
// agent path always does — see pkg/agent's ToolEnv construction), or the
// package-level SP-137 vision capability resolves (registry-driven vision
// client, or the platform's native OCR shim).
//
// It is the availability half of the critiqueOutput.Visual decision, and it is
// deterministic and hermetic: with no wired processor it only consults the
// capability probe — never the network — so a unit-test env with no provider
// config, no custom vision providers and no native OCR reports false.
//
// Package-level state (provider configs, native-OCR shim discovery) is read at
// call time rather than cached, so a process that gains or loses a tier
// between runs is reported correctly.
func critiqueVisionTierAvailable(env ToolEnv) bool {
	// A wired processor is honoured as a truthy signal — the same shape the
	// other consumers of this seam in pkg/agent use (`workflow != nil`), and
	// `critiqued` still gates on real analysis text, so a processor that
	// cannot talk to a model never yields visual=true.
	if env.VisionProcessor != nil {
		return true
	}
	// With no wired processor the pass goes through the package-level
	// AnalyzeImage, which reports a missing capability as a structured
	// response rather than an error — so probe the capability directly
	// instead of inferring it from the response. The probe never touches the
	// network: native OCR presence, then provider-config vision models.
	return HasVisionCapability()
}

// renderCritiqueTarget rasterizes one target through the shared render helper,
// and — inside the one window where the browser's temp PNG provably exists —
// captures the SP-137 attachment, the raw bytes for the provenance-headed
// artifact, and (when critique is true) the vision tier's analysis of those
// pixels. It returns (rendered, attachment, artifact, analysis, analyzeErr,
// renderErr).
//
// §4e render cache: before rasterizing, it computes the target's cache key
// (source content hash + render material) and, when an artifact from an
// earlier run matches, reuses it — the browser is not asked to render again,
// and the cached provenance is preserved (writeCritiqueArtifact replaces it
// with a fresh one, which carries the same sourceHash and render material).
//
// analyzeErr is a degraded-result signal, not a tool failure: a missing or
// failing vision tier still leaves a rendered artifact and a rubric, which item
// 4.2 turns into the visual:false static result.
func renderCritiqueTarget(
	ctx context.Context,
	env ToolEnv,
	t critiqueTarget,
	viewOpts RenderInputOptions,
	instruction string,
	critique bool,
) (bool, ToolResult, critiqueArtifact, string, error, error) {
	renderSource := t.RenderSource

	// §4e cache lookup: read the source bytes once (for either the mermaid
	// page or the plain browser render) and derive the content hash from them,
	// so a repeat critique of unchanged content can skip the re-render.
	sourceBytes, hashErr := readCritiqueSourceBytes(ctx, t)
	if hashErr != nil {
		return false, ToolResult{}, critiqueArtifact{}, "", nil, hashErr
	}
	sourceHash := critiqueContentHash(sourceBytes)
	material := critiqueRenderMaterial(viewOpts, instruction)
	key := critiqueCacheKey(sourceHash, material)

	var cleanup func()
	if t.Kind == renderKindMermaid {
		htmlPath, mermaidCleanup, buildErr := writeMermaidHTMLFile(string(sourceBytes), "")
		if buildErr != nil {
			return false, ToolResult{}, critiqueArtifact{}, "", nil,
				fmt.Errorf("cannot prepare flow render page: %w", buildErr)
		}
		renderSource = htmlPath
		cleanup = mermaidCleanup
	}
	if cleanup != nil {
		defer cleanup()
	}

	artifactPath := critiqueArtifactPath(t)

	// A matching cache entry means the pixels for this exact content + render
	// material are already on disk: attach them, report the artifact, and skip
	// the browser entirely. The critique still runs against the cached PNG, so
	// the vision tier judges the same bytes it would have rendered.
	if hit, ok := loadCachedCritiqueArtifact(ctx, artifactPath, key); ok {
		hit.artifact.Target = t.Label
		analysis, analyzeErr := "", error(nil)
		if critique {
			// The cached PNG is the render; run the vision pass against it so a
			// cache hit produces the same findings a fresh render would.
			pngAbs, resolveErr := filesystem.SafeResolvePathWithBypass(ctx, artifactPath)
			if resolveErr == nil {
				analysis, analyzeErr = runCritiqueVisionPass(ctx, env, instruction, pngAbs)
			} else {
				analyzeErr = resolveErr
			}
		}
		return true, hit.attachment, hit.artifact, analysis, analyzeErr, nil
	}

	var attachment ToolResult
	var pngBytes []byte
	var analysis string
	var analyzeErr error
	_, renderedBool, renderErr := renderInputToString(ctx, env, "design_critique", renderSource, viewOpts,
		func(renderCtx context.Context, pngPath string) (string, error) {
			// The browser's temp PNG exists only here: capture the SP-137
			// attachment, the raw bytes for the provenance-headed artifact,
			// and the vision analysis in this one window.
			if att, ok := buildRenderAttachment(renderCtx, pngPath); ok {
				attachment = att
			}
			safePath, resolveErr := filesystem.SafeResolvePathWithBypass(renderCtx, pngPath)
			if resolveErr != nil {
				return "", resolveErr
			}
			data, readErr := os.ReadFile(safePath)
			if readErr != nil {
				return "", readErr
			}
			pngBytes = data
			if !critique {
				return "", nil
			}
			// A vision failure must not fail the render: return the analysis
			// text (possibly empty) and surface the error through analyzeErr.
			text, aErr := runCritiqueVisionPass(renderCtx, env, instruction, pngPath)
			analysis, analyzeErr = text, aErr
			return text, nil
		})
	if renderErr != nil {
		return false, ToolResult{}, critiqueArtifact{}, "", nil, renderErr
	}
	if !renderedBool {
		return false, ToolResult{}, critiqueArtifact{}, "", nil, nil
	}

	artifact, writeErr := writeCritiqueArtifact(ctx, t, pngBytes, instruction, key, sourceHash, material)
	if writeErr != nil {
		// The artifact is derived output: failing to cache it must not fail
		// the critique. The caller still gets the render count and the
		// attached pixels.
		return true, attachment, critiqueArtifact{Target: t.Label}, analysis, analyzeErr, nil
	}
	return true, attachment, artifact, analysis, analyzeErr, nil
}

// writeCritiqueArtifact persists a rendered PNG under
// design/.cache/renders/ and appends the SP-140 invariant 2 provenance header.
// It returns the artifact descriptor (workspace-relative path + provenance
// text). A failure is returned for the caller to downgrade to a notice.
//
// sourceHash (the §4e content hash of the render source bytes) and material
// (the canonical render material) are recorded in the provenance so a consumer
// can verify the artifact; cacheKey (their composed key) is written to the
// sidecar so a later run can prove a cache hit rather than assume it.
func writeCritiqueArtifact(ctx context.Context, t critiqueTarget, png []byte, instruction, cacheKey, sourceHash, material string) (critiqueArtifact, error) {
	if len(png) == 0 {
		return critiqueArtifact{}, errors.New("no rendered bytes to persist")
	}

	artifactPath := critiqueArtifactPath(t)

	abs, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, artifactPath)
	if err != nil {
		return critiqueArtifact{}, err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return critiqueArtifact{}, mkErr
	}

	provenance := buildCritiqueProvenance(t, instruction, sourceHash, material)
	if writeErr := os.WriteFile(abs, append(append([]byte{}, png...), []byte(provenanceBanner(provenance))...), 0o644); writeErr != nil {
		return critiqueArtifact{}, writeErr
	}
	// The sidecar is what makes the next run's cache lookup possible (§4e).
	// Best-effort: a sidecar write failure still leaves a valid PNG, so the
	// artifact is returned and the next run simply re-renders.
	_ = writeCritiqueCache(ctx, artifactPath, cacheKey, sourceHash, material)
	return critiqueArtifact{Target: t.Label, Path: artifactPath, Provenance: provenance}, nil
}

// critiqueArtifactPath is the derived-artifact path for a target:
// design/.cache/renders/<stem>.png, or design/.cache/renders/<base>~<cmp>.png
// for a comparison artifact, where <base> is the compared-against target's
// stem and <cmp> is this target's. `~` cannot appear in a design slug, so the
// composed name is unambiguous and a target never collides with its
// comparison.
func critiqueArtifactPath(t critiqueTarget) string {
	dir := path.Join(design.DirName, designArtifactDirName, designRenderArtifactDirName)
	stem := critiqueStem(t)
	if t.Stage != "compare" || t.CompareLabel == "" {
		return path.Join(dir, stem+".png")
	}
	base := critiqueStemForLabel(t.CompareLabel)
	return path.Join(dir, base+designCritiqueStemSeparator+stem+".png")
}

// critiqueStem is the slug-safe stem used for a target's artifact name,
// derived from its label (falling back to its source).
func critiqueStem(t critiqueTarget) string {
	stem := critiqueStemForLabel(t.Label)
	if stem == "target" {
		stem = critiqueStemForLabel(t.Source)
	}
	return stem
}

// critiqueStemForLabel derives a slug-safe stem from a workspace-relative
// label: "design/wireframes/login.svg" → "login". It returns "target" when the
// label yields nothing usable, so an artifact name is always produced.
func critiqueStemForLabel(label string) string {
	base := GetBaseName(label)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	if base == "" {
		return "target"
	}
	// slugifyImageStem drops characters it cannot map, including a leading
	// digit or hyphen; the sentinel keeps them and is trimmed back off.
	stem := strings.TrimPrefix(slugifyImageStem("x"+base, ""), "x")
	if stem == "" {
		return "target"
	}
	return stem
}

// provenanceBanner renders the textual provenance block appended to a derived
// PNG. It is plain ASCII with a fixed opener and terminator so a tool that
// reads the raw bytes can extract it without parsing the image.
func provenanceBanner(provenance string) string {
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(provenanceHeaderPrefix)
	sb.WriteString("\n")
	sb.WriteString(provenance)
	sb.WriteString("\n")
	sb.WriteString(provenanceHeaderTerminator)
	sb.WriteString("\n")
	return sb.String()
}

// buildCritiqueProvenance composes the provenance text for one artifact
// (SP-140 invariant 2: a derived artifact must be recognizable as derived and
// carry enough to regenerate it).
//
// sourceHash is the artifact's §4e content hash (the SHA-256 of the render
// source bytes) and material is the canonical render material (viewport +
// rubric) folded into the cache key. Both are recorded so a consumer can
// verify the artifact's provenance and so a later run can prove a cache hit
// rather than assume it.
func buildCritiqueProvenance(t critiqueTarget, instruction, sourceHash, material string) string {
	var sb strings.Builder
	sb.WriteString("tool: design_critique\n")
	fmt.Fprintf(&sb, "source: %s\n", t.Label)
	fmt.Fprintf(&sb, "kind: %s\n", renderKindName(t.Kind))
	if sourceHash != "" {
		// The source content hash, per §4e.
		fmt.Fprintf(&sb, "sourceHash: %s\n", sourceHash)
	}
	if material != "" {
		fmt.Fprintf(&sb, "renderMaterial: %s\n", material)
	}
	sb.WriteString("generated: " + time.Now().UTC().Format(time.RFC3339) + "\n")
	sb.WriteString("note: derived render cache — never edit; regenerate with design_render/design_critique\n")
	if instruction != "" {
		// The instruction is the render-time input most likely to explain a
		// surprising artifact, so it is recorded (single line, for a stable
		// banner shape).
		sb.WriteString("instruction: " + strings.Join(strings.Fields(instruction), " ") + "\n")
	}
	return sb.String()
}

// renderKindName names a render kind for the provenance banner.
func renderKindName(k designRenderKind) string {
	switch k {
	case renderKindBrowser:
		return "browser"
	case renderKindMermaid:
		return "mermaid"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Render cache + cost cap (SP-140-4 §4e)
// ---------------------------------------------------------------------------
